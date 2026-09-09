package worker_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestApplyNodeHappyPath(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})

	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if len(mock.Applied(f.node.ID)) != 1 {
		t.Fatal("driver not called")
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	if n.Dirty || n.LastApplyAt == nil {
		t.Fatalf("node not marked applied: %+v", n)
	}
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status != db.KeyStatusActive {
		t.Fatalf("key status %s", kk.Status)
	}
	profiles, _ := f.st.Q.ListNodeProfiles(ctx, f.node.ID)
	for _, p := range profiles {
		if p.SyncState != db.SyncStateSynced {
			t.Fatalf("profile %s sync %s", p.Name, p.SyncState)
		}
	}
	jobs, _ := f.st.Q.ListNodeApplyJobs(ctx, db.ListNodeApplyJobsParams{NodeID: f.node.ID, Limit: 10})
	if len(jobs) != 1 || jobs[0].Status != db.ApplyStatusOk {
		t.Fatalf("jobs %+v", jobs)
	}
}

func TestApplyNodeFailureKeepsDirtyAndPending(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.FailNextApply(f.node.ID, "relay -check rejected profiles")
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err == nil {
		t.Fatal("expected error")
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if !n.Dirty || kk.Status != db.KeyStatusPending {
		t.Fatalf("state after failure: dirty=%v key=%s", n.Dirty, kk.Status)
	}
	jobs, _ := f.st.Q.ListNodeApplyJobs(ctx, db.ListNodeApplyJobsParams{NodeID: f.node.ID, Limit: 10})
	if jobs[0].Status != db.ApplyStatusRolledBack || jobs[0].Error == "" {
		t.Fatalf("job %+v", jobs[0])
	}
}

func TestApplyFailureRecordsAlertAndNotifiesThenSuccessResolves(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.FailNextApply(f.node.ID, "relay -check rejected profiles\nfull stack trace here")
	_, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	if err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	alerts := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	a.SetAlerts(alerts)

	if err := a.ApplyNode(ctx, f.node.ID); err == nil {
		t.Fatal("expected error")
	}
	if sender.count() != 1 {
		t.Fatalf("expected 1 telegram send, got %d", sender.count())
	}
	text := sender.last().text
	if !strings.Contains(text, "relay -check rejected profiles") {
		t.Fatalf("text missing first line: %q", text)
	}
	if strings.Contains(text, "full stack trace here") {
		t.Fatalf("text leaked lines past the first: %q", text)
	}

	openAlerts, _ := f.st.Q.ListOpenAlerts(ctx)
	var found bool
	for _, al := range openAlerts {
		if al.Kind == "apply_failed" {
			found = true
			if al.Message != "relay -check rejected profiles" {
				t.Fatalf("alert message = %q, want the first line only", al.Message)
			}
		}
	}
	if !found {
		t.Fatalf("apply_failed alert not recorded: %+v", openAlerts)
	}

	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	openAlerts, _ = f.st.Q.ListOpenAlerts(ctx)
	for _, al := range openAlerts {
		if al.Kind == "apply_failed" {
			t.Fatalf("apply_failed alert not resolved after a successful apply: %+v", al)
		}
	}
	if sender.count() != 1 {
		t.Fatalf("resolving on success must not send a new telegram message, got %d sends", sender.count())
	}
}

func TestApplyOfflineNodeSkipped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err == nil {
		t.Fatal("expected offline error")
	}
	if len(mock.Applied(f.node.ID)) != 0 {
		t.Fatal("must not call driver")
	}
}

func TestApplyDoesNotActivateKeyWithoutProfiles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})

	nodeB, err := f.st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "b", Hostname: "b.test"})
	if err != nil {
		t.Fatal(err)
	}

	k, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeB.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.keys.Unbind(ctx, k.ID, nodeB.ID); err != nil {
		t.Fatal(err)
	}

	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status != db.KeyStatusPending {
		t.Fatalf("key with no profiles wrongly activated: status=%s", kk.Status)
	}
}

func TestTriggerRunsApply(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	_ = f.st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: f.node.ID, Dirty: true})
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	go a.Run(ctx)
	a.Trigger(f.node.ID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(mock.Applied(f.node.ID)) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("trigger did not apply")
}

type blockingDriver struct {
	*nodedriver.Mock
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingDriver(m *nodedriver.Mock) *blockingDriver {
	return &blockingDriver{Mock: m, entered: make(chan struct{}), release: make(chan struct{})}
}

func (d *blockingDriver) Apply(ctx context.Context, id uuid.UUID, req nodedriver.ApplyRequest) (nodedriver.ApplyResult, error) {
	d.once.Do(func() { close(d.entered) })
	select {
	case <-d.release:
	case <-ctx.Done():
		return nodedriver.ApplyResult{}, ctx.Err()
	}
	return d.Mock.Apply(ctx, id, req)
}

// TestApplyDoesNotSyncStateCreatedMidApply is the C1 regression test.
func TestApplyDoesNotSyncStateCreatedMidApply(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	drv := newBlockingDriver(mock)

	a := worker.NewApply(f.st, f.box, drv, time.Hour, slog.New(slog.DiscardHandler))
	done := make(chan error, 1)
	go func() { done <- a.ApplyNode(ctx, f.node.ID) }()

	select {
	case <-drv.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("apply never reached the driver")
	}

	// The snapshot is taken; now race a mutation against it.
	k, err := f.keys.Create(ctx, keys.CreateInput{Label: "mid", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	if err != nil {
		t.Fatal(err)
	}
	close(drv.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// The pushed request must not contain the new profile.
	applied := mock.Applied(f.node.ID)
	if len(applied) != 1 {
		t.Fatalf("expected exactly one apply, got %d", len(applied))
	}
	for _, p := range applied[0].Profiles {
		if p.Name == domain.ProfileName(k.ID) {
			t.Fatal("test is not exercising the race: the new profile was in the pushed snapshot")
		}
	}

	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status != db.KeyStatusPending {
		t.Fatalf("key created mid-apply was activated without ever being pushed: status=%s", kk.Status)
	}
	profiles, _ := f.st.Q.ListNodeProfiles(ctx, f.node.ID)
	for _, p := range profiles {
		if p.Name == domain.ProfileName(k.ID) && p.SyncState != db.SyncStatePending {
			t.Fatalf("profile created mid-apply marked %s", p.SyncState)
		}
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	if !n.Dirty {
		t.Fatal("node cleared dirty against a stale snapshot; the new profile would never be pushed")
	}

	// A second apply (nothing racing it this time) settles the state.
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	kk, _ = f.st.Q.GetKey(ctx, k.ID)
	n, _ = f.st.Q.GetNode(ctx, f.node.ID)
	if kk.Status != db.KeyStatusActive || n.Dirty {
		t.Fatalf("second apply did not settle: key=%s dirty=%v", kk.Status, n.Dirty)
	}
}

func TestSweepPicksUpDirtyNodeMarkedOffline(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOffline})
	_ = f.st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: f.node.ID, Dirty: true})

	a := worker.NewApply(f.st, f.box, mock, 30*time.Millisecond, slog.New(slog.DiscardHandler))
	go a.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(mock.Applied(f.node.ID)) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sweep never applied a dirty node the driver reports online")
}

func TestStopWaitsForInFlightApply(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	bd := newBlockingDriver(mock)

	a := worker.NewApply(f.st, f.box, bd, time.Hour, slog.New(slog.DiscardHandler))
	go a.Run(ctx)
	a.Trigger(f.node.ID)
	select {
	case <-bd.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("apply never started")
	}

	cancel() // the SIGTERM: Run returns, the apply is still pushing state

	// A Stop whose own budget expires first reports that rather than hanging.
	short, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShort()
	if err := a.Stop(short); err == nil {
		t.Fatal("Stop returned nil while an apply was still in flight")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- a.Stop(context.Background()) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned before the in-flight apply finished")
	case <-time.After(150 * time.Millisecond):
	}

	close(bd.release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return after the apply finished")
	}
	// The apply must have run to completion despite the cancelled Run context.
	if len(mock.Applied(f.node.ID)) != 1 {
		t.Fatalf("apply did not complete after shutdown: %d applies", len(mock.Applied(f.node.ID)))
	}
	n, _ := f.st.Q.GetNode(context.Background(), f.node.ID)
	if n.LastApplyAt == nil {
		t.Fatal("apply outcome was not recorded")
	}
}

func TestStopWithNoWorkReturnsImmediately(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := worker.Start(ctx, worker.NewApply(f.st, f.box, nodedriver.NewMock(), time.Hour, slog.New(slog.DiscardHandler)),
		worker.NewExpiry(f.st, f.keys, slog.New(slog.DiscardHandler)),
		worker.NewStats(f.st, nodedriver.NewMock(), time.Minute, slog.New(slog.DiscardHandler)), nil)
	cancel()
	start := time.Now()
	if err := stop(context.Background()); err != nil {
		t.Fatalf("Stop with no in-flight applies: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %v with nothing in flight", elapsed)
	}
}

func TestApplyFailureLeavesJobAndProfilesConsistent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.FailNextApply(f.node.ID, "relay -check rejected profiles")
	if _, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}}); err != nil {
		t.Fatal(err)
	}
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err == nil {
		t.Fatal("expected the apply to fail")
	}

	jobs, _ := f.st.Q.ListNodeApplyJobs(ctx, db.ListNodeApplyJobsParams{NodeID: f.node.ID, Limit: 10})
	if len(jobs) != 1 {
		t.Fatalf("expected exactly one job row, got %d", len(jobs))
	}
	if jobs[0].Status != db.ApplyStatusRolledBack || jobs[0].Error == "" || jobs[0].FinishedAt == nil {
		t.Fatalf("job not finished consistently: %+v", jobs[0])
	}
	profiles, _ := f.st.Q.ListNodeProfiles(ctx, f.node.ID)
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	for _, p := range profiles {
		if p.SyncState != db.SyncStateFailed {
			t.Fatalf("profile %s is %s while the job says %s", p.Name, p.SyncState, jobs[0].Status)
		}
	}
	var failedAlerts int
	openAlerts, _ := f.st.Q.ListOpenAlerts(ctx)
	for _, al := range openAlerts {
		if al.Kind == "apply_failed" {
			failedAlerts++
		}
	}
	if failedAlerts != 1 {
		t.Fatalf("expected exactly one open apply_failed alert, got %d", failedAlerts)
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	if !n.Dirty || n.LastApplyAt != nil {
		t.Fatalf("failed apply must leave the node dirty and unapplied: %+v", n)
	}
}

func assignSite(t *testing.T, f *fixture, body string) string {
	t.Helper()
	ctx := context.Background()
	b := sitekit.Bundle{Files: map[string][]byte{"index.html": []byte(body)}}
	if err := f.st.Q.UpsertNodeSite(ctx, db.UpsertNodeSiteParams{NodeID: f.node.ID, Bundle: b.JSON(), BundleHash: b.Hash()}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: f.node.ID, Dirty: true}); err != nil {
		t.Fatal(err)
	}
	return b.Hash()
}

func TestApplyDoesNotMarkSiteAssignedMidApplyAsDeployed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})

	hashA := assignSite(t, f, "<html><body>A</body></html>")

	drv := newBlockingDriver(mock)
	a := worker.NewApply(f.st, f.box, drv, time.Hour, slog.New(slog.DiscardHandler))
	done := make(chan error, 1)
	go func() { done <- a.ApplyNode(ctx, f.node.ID) }()

	select {
	case <-drv.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("apply never reached the driver")
	}

	// The snapshot (bundle A) is taken and in flight; assign template B against it.
	hashB := assignSite(t, f, "<html><body>B</body></html>")
	if hashA == hashB {
		t.Fatal("test setup broken: both bundles hash the same")
	}
	close(drv.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	applied := mock.Applied(f.node.ID)
	if len(applied) != 1 || applied[0].Site == nil {
		t.Fatalf("expected exactly one apply carrying a site, got %+v", applied)
	}
	if got := string(applied[0].Site.Files["index.html"]); !strings.Contains(got, ">A<") {
		t.Fatalf("test is not exercising the race: the pushed site was %q, want bundle A", got)
	}

	site, err := f.st.Q.GetNodeSite(ctx, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if site.BundleHash != hashB {
		t.Fatalf("node_sites.bundle_hash = %q, want the mid-apply bundle %q", site.BundleHash, hashB)
	}
	if site.DeployedHash != nil && *site.DeployedHash == hashB {
		t.Fatal("bundle assigned mid-apply was marked deployed although it was never pushed")
	}
	if site.DeployedHash != nil && *site.DeployedHash != hashA {
		t.Fatalf("deployed_hash = %q, want NULL or the pushed bundle's hash %q", *site.DeployedHash, hashA)
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	if !n.Dirty {
		t.Fatal("node cleared dirty against a stale snapshot; the new site would never be pushed")
	}

	// A second sweep, with nothing racing it, deploys bundle B for real and settles the row.
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	applied = mock.Applied(f.node.ID)
	if len(applied) != 2 || applied[1].Site == nil {
		t.Fatalf("second sweep did not push a site: %+v", applied)
	}
	if got := string(applied[1].Site.Files["index.html"]); !strings.Contains(got, ">B<") {
		t.Fatalf("second sweep pushed %q, want bundle B", got)
	}
	site, _ = f.st.Q.GetNodeSite(ctx, f.node.ID)
	if site.DeployedHash == nil || *site.DeployedHash != hashB {
		t.Fatalf("deployed_hash after the settling apply = %v, want %q", site.DeployedHash, hashB)
	}
	n, _ = f.st.Q.GetNode(ctx, f.node.ID)
	if n.Dirty {
		t.Fatal("node still dirty after the settling apply")
	}
}

func TestApplyRecordsDeployedHashOfPushedBundle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	hash := assignSite(t, f, "<html><body>only</body></html>")

	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	site, _ := f.st.Q.GetNodeSite(ctx, f.node.ID)
	if site.DeployedHash == nil || *site.DeployedHash != hash {
		t.Fatalf("deployed_hash = %v, want %q", site.DeployedHash, hash)
	}
	// And a repeat apply must not push the same site again.
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	applied := mock.Applied(f.node.ID)
	if len(applied) != 2 || applied[1].Site != nil {
		t.Fatalf("an already-deployed site was pushed again: %+v", applied[1])
	}
}

func TestStopWaitsForRunToReturn(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)

	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	go a.Run(ctx)
	// An apply that completed proves Run is up, so Stop cannot mistake it for never started.
	a.Trigger(f.node.ID)
	deadline := time.Now().Add(5 * time.Second)
	for len(mock.Applied(f.node.ID)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("apply loop never picked up the trigger")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Run's context is still live, so Stop must not claim everything is finished.
	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	if err := a.Stop(short); err == nil {
		t.Fatal("Stop returned nil while the apply loop was still running and could still spawn")
	}

	cancel()
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after cancelling Run: %v", err)
	}
}

func TestApplyNodeRaisesAnAlertForDeferredConfig(t *testing.T) {
	f := newTelemtFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	mock.DeferNextApply(f.node.ID, "web.carrier_learning")

	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	openAlerts, _ := f.st.Q.ListOpenAlerts(ctx)
	var found bool
	for _, al := range openAlerts {
		if al.Kind == "config_deferred" {
			found = true
			if !strings.Contains(al.Message, "web.carrier_learning") {
				t.Fatalf("alert message = %q", al.Message)
			}
		}
	}
	if !found {
		t.Fatalf("a deferred patch must reach the operator: %+v", openAlerts)
	}

	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	openAlerts, _ = f.st.Q.ListOpenAlerts(ctx)
	for _, al := range openAlerts {
		if al.Kind == "config_deferred" {
			t.Fatalf("an apply with nothing deferred must resolve the alert: %+v", al)
		}
	}
}
