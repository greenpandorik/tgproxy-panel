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

	runStarted atomic.Bool
	runDone    chan struct{}
}

// applyTimeout bounds a single detached apply.
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

func (a *Apply) SetIntervalFunc(f func(context.Context) time.Duration) {
	a.intervalFunc = f
}

// SetAlerts installs the Telegram alert notifier; nil (the default) means no notifications are sent.
func (a *Apply) SetAlerts(alerts *Alerts) { a.alerts = alerts }

func (a *Apply) Trigger(nodeID uuid.UUID) {
	select {
	case a.trigger <- nodeID:
	default:
	}
}

func (a *Apply) Run(ctx context.Context) {
	a.runStarted.Store(true)
	defer close(a.runDone)
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

// Stop waits for the applies Run has spawned, so the process does not exit while a node is mid-restart.
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
			hash := des.SiteHash
			if err := q.SetNodeSiteDeployed(ctx, db.SetNodeSiteDeployedParams{NodeID: nodeID, Hash: &hash}); err != nil {
				return err
			}
		}
		if err := q.SetNodeApplied(ctx, db.SetNodeAppliedParams{ID: nodeID, DirtySeq: des.DirtySeq}); err != nil {
			return err
		}
		if _, err := q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: nullUUID(nodeID), Kind: "apply_failed"}); err != nil {
			return err
		}
		if err := recordDeferred(ctx, q, nodeID, res); err != nil {
			return err
		}
		return q.ActivatePendingKeysForNode(ctx)
	})
}

// recordDeferred raises an alert for config the node persisted without activating. The apply
// itself succeeded, so nothing retries it: only a restart the operator chooses makes those
// keys live, and an apply log nobody reads is not a way to say so.
func recordDeferred(ctx context.Context, q *db.Queries, nodeID uuid.UUID, res nodedriver.ApplyResult) error {
	if len(res.DeferredFields) == 0 {
		if _, err := q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: nullUUID(nodeID), Kind: "config_deferred"}); err != nil {
			return err
		}
		return nil
	}
	msg := "telemt persisted but did not activate " + strings.Join(res.DeferredFields, ", ") + "; a restart is required"
	_, err := q.InsertAlert(ctx, db.InsertAlertParams{NodeID: nullUUID(nodeID), Kind: "config_deferred", Message: msg})
	return err
}
