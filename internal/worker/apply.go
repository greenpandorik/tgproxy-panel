package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type Apply struct {
	st           *store.Store
	box          *crypto.Box
	driver       nodedriver.Driver
	interval     time.Duration
	intervalFunc func(context.Context) time.Duration
	alerts       *Alerts
	log          *slog.Logger
	trigger      chan uuid.UUID
	mu           sync.Mutex
	inFlight     map[uuid.UUID]bool
	wg           sync.WaitGroup

	// runStarted/runDone let Stop wait for Run to return before it waits on wg.
	// Run is the only caller of spawn (and so of wg.Add), so a Trigger landing at
	// the same instant as Stop would otherwise Add concurrently with Wait, which
	// sync.WaitGroup explicitly forbids.
	runStarted atomic.Bool
	runDone    chan struct{}
}

// applyTimeout bounds a single detached apply. The gateway driver already caps
// its own call at 90s; this is the backstop that keeps a spawned goroutine from
// outliving the process's shutdown budget.
const applyTimeout = 2 * time.Minute

// stopTimeout is how long Stop waits for in-flight applies before giving up.
const stopTimeout = 30 * time.Second

func NewApply(st *store.Store, box *crypto.Box, driver nodedriver.Driver, interval time.Duration, log *slog.Logger) *Apply {
	return &Apply{
		st: st, box: box, driver: driver, interval: interval, log: log,
		trigger: make(chan uuid.UUID, 64), inFlight: map[uuid.UUID]bool{},
		runDone: make(chan struct{}),
	}
}

// SetIntervalFunc installs an override consulted after every tick of Run to
// decide the sweep interval; a nil or non-positive result keeps the current
// interval. Nil (the default) keeps the interval fixed at the constructor's
// value.
func (a *Apply) SetIntervalFunc(f func(context.Context) time.Duration) {
	a.intervalFunc = f
}

// SetAlerts installs the Telegram alert notifier; nil (the default) means no
// notifications are sent.
func (a *Apply) SetAlerts(alerts *Alerts) { a.alerts = alerts }

// Trigger requests an apply pass for nodeID. Non-blocking: if the buffer is
// full the request is dropped, since the periodic sweep over dirty nodes
// will pick it up anyway.
func (a *Apply) Trigger(nodeID uuid.UUID) {
	select {
	case a.trigger <- nodeID:
	default:
	}
}

// Run drives the apply loop until ctx is cancelled: it reacts to Trigger
// requests immediately and otherwise sweeps every dirty node on each tick.
//
// The sweep uses ListDirtyNodesAny (no status filter) and lets ApplyNode's
// driver.Online check decide reachability. Filtering on status in SQL used to
// hide a node whose gRPC stream was alive but whose row the stats worker had
// marked offline on a stale heartbeat: dirty, reachable, and never swept.
func (a *Apply) Run(ctx context.Context) {
	a.runStarted.Store(true)
	defer close(a.runDone)
	// Applies are spawned on a context detached from ctx so a SIGTERM does not
	// cut one mid-restart; Stop waits for them instead.
	applyCtx := context.WithoutCancel(ctx)
	interval := a.interval
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-a.trigger:
			a.spawn(applyCtx, id)
		case <-t.C:
			nodes, err := a.st.Q.ListDirtyNodesAny(ctx)
			if err != nil {
				a.log.Error("list dirty nodes", "err", err)
			} else {
				for _, n := range nodes {
					a.spawn(applyCtx, n.ID)
				}
			}
			if a.intervalFunc != nil {
				if next := a.intervalFunc(ctx); next > 0 && next != interval {
					interval = next
					t.Reset(interval)
				}
			}
		}
	}
}

// spawn runs one apply in a tracked goroutine so Stop can wait for it.
func (a *Apply) spawn(ctx context.Context, id uuid.UUID) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(ctx, applyTimeout)
		defer cancel()
		a.applyLogged(ctx, id)
	}()
}

// Stop waits for the applies Run has spawned, so the process does not exit
// while a node is mid-restart. It gives up after stopTimeout (or when ctx is
// done, whichever is sooner) and reports that as an error; the agent's own
// rollback is what covers the node from there.
//
// Call it only after the context passed to Run has been cancelled - that is the
// shutdown order cmd/panel uses. Stop waits for Run to return first: Run is the
// only goroutine that calls wg.Add, so letting a Trigger land while wg.Wait is
// already running would be exactly the concurrent Add/Wait sync.WaitGroup
// forbids.
func (a *Apply) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, stopTimeout)
	defer cancel()
	if a.runStarted.Load() {
		select {
		case <-a.runDone:
		case <-ctx.Done():
			return fmt.Errorf("apply loop did not stop within %s (cancel Run's context before Stop): %w", stopTimeout, ctx.Err())
		}
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("in-flight applies did not finish within %s: %w", stopTimeout, ctx.Err())
	}
}

func (a *Apply) applyLogged(ctx context.Context, id uuid.UUID) {
	if err := a.ApplyNode(ctx, id); err != nil {
		a.log.Warn("apply failed", "node", id, "err", err)
	}
}

// ApplyNode pushes the desired state to one node and records the outcome.
func (a *Apply) ApplyNode(ctx context.Context, nodeID uuid.UUID) error {
	a.mu.Lock()
	if a.inFlight[nodeID] {
		a.mu.Unlock()
		return errors.New("apply already in progress")
	}
	a.inFlight[nodeID] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.inFlight, nodeID)
		a.mu.Unlock()
	}()

	if !a.driver.Online(nodeID) {
		return nodedriver.ErrOffline
	}
	des, err := DesiredState(ctx, a.st, a.box, nodeID)
	if err != nil {
		return err
	}
	kind := db.ApplyKindProfiles
	if des.Req.Site != nil {
		kind = db.ApplyKindBoth
	}
	job, err := a.st.Q.CreateApplyJob(ctx, db.CreateApplyJobParams{NodeID: nodeID, Kind: kind})
	if err != nil {
		return err
	}
	res, applyErr := a.driver.Apply(ctx, nodeID, des.Req)
	if applyErr != nil {
		status := db.ApplyStatusFailed
		if res.RolledBack {
			status = db.ApplyStatusRolledBack
		}
		errMsg := applyErr.Error()
		firstLine, _, _ := strings.Cut(errMsg, "\n")
		// One transaction so the job row, the profile sync states and the apply_failed alert
		// can never disagree. Only the profiles this apply actually pushed are marked failed;
		// anything created after the snapshot is still 'pending' and belongs to the next apply.
		if txErr := a.st.Tx(ctx, func(q *db.Queries) error {
			if err := q.FinishApplyJob(ctx, db.FinishApplyJobParams{ID: job.ID, Status: status, Error: errMsg, Log: res.Log}); err != nil {
				return err
			}
			if err := q.SetProfilesSyncByIDs(ctx, db.SetProfilesSyncByIDsParams{Ids: des.ProfileIDs, SyncState: db.SyncStateFailed}); err != nil {
				return err
			}
			_, err := q.InsertAlert(ctx, db.InsertAlertParams{NodeID: nullUUID(nodeID), Kind: "apply_failed", Message: firstLine})
			return err
		}); txErr != nil {
			a.log.Error("record apply failure", "node", nodeID, "err", txErr)
		} else if a.alerts != nil {
			// Notify after the transaction commits: the Telegram call is a network round
			// trip and must not hold the DB transaction open.
			if node, err := a.st.Q.GetNode(ctx, nodeID); err == nil {
				a.alerts.ApplyFailed(ctx, node, errMsg)
			}
		}
		return fmt.Errorf("apply: %w", applyErr)
	}
	return a.st.Tx(ctx, func(q *db.Queries) error {
		if err := q.FinishApplyJob(ctx, db.FinishApplyJobParams{ID: job.ID, Status: db.ApplyStatusOk, Log: res.Log}); err != nil {
			return err
		}
		if err := q.SetProfilesSyncByIDs(ctx, db.SetProfilesSyncByIDsParams{Ids: des.ProfileIDs, SyncState: db.SyncStateSynced}); err != nil {
			return err
		}
		if des.Req.Site != nil {
			// des.SiteHash, not a fresh read: the row may already carry a bundle
			// assigned while this apply was in flight, which was never pushed. The
			// query's own bundle_hash guard then matches nothing, deployed_hash stays
			// as it was and the next sweep deploys the new bundle.
			hash := des.SiteHash
			if err := q.SetNodeSiteDeployed(ctx, db.SetNodeSiteDeployedParams{NodeID: nodeID, Hash: &hash}); err != nil {
				return err
			}
		}
		// Conditional on dirty_seq: if anything dirtied the node while this apply was in
		// flight the update matches no row, the node stays dirty and the sweep re-applies.
		if err := q.SetNodeApplied(ctx, db.SetNodeAppliedParams{ID: nodeID, DirtySeq: des.DirtySeq}); err != nil {
			return err
		}
		// A prior failure's alert, if any, is now stale; close it silently (no notification
		// on recovery from an apply failure - only node offline/online gets a "back" message).
		if _, err := q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: nullUUID(nodeID), Kind: "apply_failed"}); err != nil {
			return err
		}
		return q.ActivatePendingKeysForNode(ctx)
	})
}
