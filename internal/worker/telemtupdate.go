package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

// Statuses a telemt update job ends in. Only StatusOK means the node is running the pinned
// build and serving; StatusNeedsAttention is a node nobody should assume is fine.
const (
	UpdateStatusRunning        = "running"
	UpdateStatusOK             = "ok"
	UpdateStatusRefused        = "refused"
	UpdateStatusFailed         = "failed"
	UpdateStatusRolledBack     = "rolled_back"
	UpdateStatusRollbackFailed = "rollback_failed"
	UpdateStatusNeedsAttention = "needs_attention"
)

// Outcomes the agent reports, mapped onto job statuses.
var updateStatusByOutcome = map[string]string{
	"updated":                  UpdateStatusOK,
	"refused":                  UpdateStatusRefused,
	"failed":                   UpdateStatusFailed,
	"rolled_back":              UpdateStatusRolledBack,
	"rollback_failed":          UpdateStatusRollbackFailed,
	"updated_admission_closed": UpdateStatusNeedsAttention,
}

const (
	defaultUpdatePollInterval = 2 * time.Second
	defaultUpdateJobTimeout   = 30 * time.Minute
	updateStatusCallTimeout   = 20 * time.Second
)

// ErrUpdateInFlight is returned when this node already has an update running.
var ErrUpdateInFlight = errors.New("a telemt update is already running for this node")

// Verifier is the panel's own confirmation that an updated node is serving, beyond the
// node's word that its process came back.
type Verifier interface {
	VerifyNode(ctx context.Context, node db.Node) (ok bool, detail []byte, summary string)
}

// TelemtUpdater runs telemt updates and records what the node reported, step by step.
type TelemtUpdater struct {
	st       *store.Store
	driver   nodedriver.Driver
	log      *slog.Logger
	verifier Verifier

	// Poll is the interval between status reads; JobTimeout bounds one whole update.
	Poll       time.Duration
	JobTimeout time.Duration

	mu       sync.Mutex
	inFlight map[uuid.UUID]bool
	wg       sync.WaitGroup
}

func NewTelemtUpdater(st *store.Store, driver nodedriver.Driver, log *slog.Logger) *TelemtUpdater {
	return &TelemtUpdater{st: st, driver: driver, log: log, inFlight: map[uuid.UUID]bool{}}
}

// SetVerifier installs the post-update check; without one the node's own verification stands alone.
func (u *TelemtUpdater) SetVerifier(v Verifier) { u.verifier = v }

func (u *TelemtUpdater) poll() time.Duration {
	if u.Poll > 0 {
		return u.Poll
	}
	return defaultUpdatePollInterval
}

func (u *TelemtUpdater) jobTimeout() time.Duration {
	if u.JobTimeout > 0 {
		return u.JobTimeout
	}
	return defaultUpdateJobTimeout
}

// Wait blocks until the updates started in the background have finished.
func (u *TelemtUpdater) Wait() { u.wg.Wait() }

func (u *TelemtUpdater) reserve(id uuid.UUID) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.inFlight[id] {
		return false
	}
	u.inFlight[id] = true
	return true
}

func (u *TelemtUpdater) release(id uuid.UUID) {
	u.mu.Lock()
	delete(u.inFlight, id)
	u.mu.Unlock()
}

// Reconcile resumes observation of an unfinished database job after the panel process
// restarted. The agent retains its latest update state, so a completed job can be closed
// immediately and a running one can be followed by this process again.
func (u *TelemtUpdater) Reconcile(ctx context.Context, node db.Node) error {
	job, err := u.st.Q.GetRunningTelemtUpdateJob(ctx, node.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !u.reserve(node.ID) {
		return nil
	}
	keepReservation := false
	defer func() {
		if !keepReservation {
			u.release(node.ID)
		}
	}()

	req := nodedriver.TelemtUpdateRequest{Version: job.ToVersion}
	elapsed := time.Since(job.StartedAt)
	if !u.driver.Online(node.ID) {
		if elapsed >= u.jobTimeout() {
			u.finish(ctx, job.ID, node, req, nodedriver.TelemtUpdate{FromVersion: job.FromVersion}, UpdateStatusFailed,
				"the panel restarted during this update and the node stayed offline until its update deadline")
		}
		return nil
	}

	callCtx, cancel := context.WithTimeout(ctx, updateStatusCallTimeout)
	status, err := u.driver.TelemtUpdateStatus(callCtx, node.ID)
	cancel()
	if err != nil {
		if elapsed >= u.jobTimeout() {
			u.finish(ctx, job.ID, node, req, nodedriver.TelemtUpdate{FromVersion: job.FromVersion}, UpdateStatusFailed,
				"the panel restarted during this update and could not recover its state: "+err.Error())
		}
		return nil
	}
	if status.ToVersion != "" && !sameUpdateVersion(status.ToVersion, job.ToVersion) {
		u.finish(ctx, job.ID, node, req, nodedriver.TelemtUpdate{FromVersion: job.FromVersion}, UpdateStatusFailed,
			"the agent reports a different update target ("+status.ToVersion+") than this job ("+job.ToVersion+")")
		return nil
	}
	if status.Done() {
		u.finish(ctx, job.ID, node, req, status, statusFor(status), status.Error)
		return nil
	}
	if status.Phase != "running" {
		u.finish(ctx, job.ID, node, req, nodedriver.TelemtUpdate{FromVersion: job.FromVersion}, UpdateStatusFailed,
			"the panel restarted during this update, but the agent no longer reports it")
		return nil
	}
	remaining := u.jobTimeout() - elapsed
	if remaining <= 0 {
		u.finish(ctx, job.ID, node, req, status, UpdateStatusFailed, "the recovered update exceeded its deadline")
		return nil
	}
	runCtx, runCancel := context.WithTimeout(context.Background(), remaining)
	u.wg.Add(1)
	keepReservation = true
	go func() {
		defer runCancel()
		defer u.wg.Done()
		defer u.release(node.ID)
		u.follow(runCtx, job.ID, node, req)
	}()
	return nil
}

func sameUpdateVersion(a, b string) bool {
	normalize := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
	return normalize(a) != "" && normalize(a) == normalize(b)
}

// Start records the job, hands the node the pinned build and follows it in the background.
// It returns as soon as the node has accepted the update.
func (u *TelemtUpdater) Start(ctx context.Context, node db.Node, req nodedriver.TelemtUpdateRequest) (db.TelemtUpdateJob, error) {
	if !u.driver.Online(node.ID) {
		return db.TelemtUpdateJob{}, nodedriver.ErrOffline
	}
	if _, err := u.st.Q.GetRunningTelemtUpdateJob(ctx, node.ID); err == nil {
		return db.TelemtUpdateJob{}, ErrUpdateInFlight
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.TelemtUpdateJob{}, err
	}
	if !u.reserve(node.ID) {
		return db.TelemtUpdateJob{}, ErrUpdateInFlight
	}
	job, err := u.st.Q.CreateTelemtUpdateJob(ctx, db.CreateTelemtUpdateJobParams{
		NodeID: node.ID, FromVersion: node.TelemtVersion, ToVersion: req.Version,
	})
	if err != nil {
		u.release(node.ID)
		return db.TelemtUpdateJob{}, err
	}

	started, err := u.driver.UpdateTelemt(ctx, node.ID, req)
	if err != nil {
		u.release(node.ID)
		u.finish(ctx, job.ID, node, req, nodedriver.TelemtUpdate{}, UpdateStatusFailed, err.Error())
		return db.TelemtUpdateJob{}, err
	}
	u.storeSteps(ctx, job.ID, started)

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), u.jobTimeout())
	u.wg.Add(1)
	go func() {
		defer cancel()
		defer u.wg.Done()
		defer u.release(node.ID)
		u.follow(runCtx, job.ID, node, req)
	}()
	return job, nil
}

// follow polls the node until it reports the update finished, writing every step it sees.
func (u *TelemtUpdater) follow(ctx context.Context, jobID uuid.UUID, node db.Node, req nodedriver.TelemtUpdateRequest) {
	var lastErr error
	for {
		callCtx, cancel := context.WithTimeout(ctx, updateStatusCallTimeout)
		st, err := u.driver.TelemtUpdateStatus(callCtx, node.ID)
		cancel()
		switch {
		case err != nil:
			lastErr = err
		case st.Done():
			u.finish(ctx, jobID, node, req, st, statusFor(st), st.Error)
			return
		default:
			lastErr = nil
			u.storeSteps(ctx, jobID, st)
		}
		select {
		case <-ctx.Done():
			msg := fmt.Sprintf("the node stopped reporting on this update after %s", u.jobTimeout())
			if lastErr != nil {
				msg += ": " + lastErr.Error()
			}
			u.finish(ctx, jobID, node, req, nodedriver.TelemtUpdate{}, UpdateStatusFailed, msg)
			return
		case <-time.After(u.poll()):
		}
	}
}

func statusFor(st nodedriver.TelemtUpdate) string {
	if s, ok := updateStatusByOutcome[st.Outcome]; ok {
		return s
	}
	return UpdateStatusFailed
}

func (u *TelemtUpdater) storeSteps(ctx context.Context, jobID uuid.UUID, st nodedriver.TelemtUpdate) {
	raw, err := json.Marshal(st.Steps)
	if err != nil {
		return
	}
	if err := u.st.Q.SetTelemtUpdateJobSteps(ctx, db.SetTelemtUpdateJobStepsParams{ID: jobID, Steps: raw}); err != nil {
		u.log.Warn("record telemt update progress", "job", jobID, "err", err)
	}
}

// finish verifies a successful update from the panel's side, writes the final row and moves
// the node's recorded version to whatever it is actually running.
func (u *TelemtUpdater) finish(ctx context.Context, jobID uuid.UUID, node db.Node, req nodedriver.TelemtUpdateRequest, st nodedriver.TelemtUpdate, status, errMsg string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if status == UpdateStatusOK && u.verifier != nil {
		ok, detail, summary := u.verifier.VerifyNode(ctx, node)
		if len(detail) > 0 && !bytes.Equal(detail, []byte("null")) {
			if err := u.st.Q.SetTelemtUpdateJobVerification(ctx, db.SetTelemtUpdateJobVerificationParams{ID: jobID, Verification: detail}); err != nil {
				u.log.Warn("record telemt update verification", "job", jobID, "err", err)
			}
		}
		if !ok {
			status = UpdateStatusNeedsAttention
			errMsg = "telemt " + req.Version + " is running but the panel's checks did not pass: " + summary
		}
	}
	raw, err := json.Marshal(st.Steps)
	if err != nil {
		raw = []byte("[]")
	}
	if _, err := u.st.Q.FinishTelemtUpdateJob(ctx, db.FinishTelemtUpdateJobParams{
		ID: jobID, Status: status, Outcome: st.Outcome, Error: errMsg, Steps: raw, FromVersion: st.FromVersion,
	}); err != nil {
		u.log.Error("finish telemt update job", "job", jobID, "err", err)
	}
	u.recordNodeVersion(ctx, node, req, st, status)
	if status != UpdateStatusOK {
		u.raiseAlert(ctx, node, status, errMsg)
	}
}

// recordNodeVersion writes the version the node is actually on, and keeps the pinned build
// listed as available whenever the node did not end up running it.
func (u *TelemtUpdater) recordNodeVersion(ctx context.Context, node db.Node, req nodedriver.TelemtUpdateRequest, st nodedriver.TelemtUpdate, status string) {
	version, available := st.FromVersion, req.Version
	if status == UpdateStatusOK || status == UpdateStatusNeedsAttention {
		version, available = req.Version, ""
	}
	if version != "" && version != node.TelemtVersion {
		if err := u.st.Q.SetNodeTelemtVersion(ctx, db.SetNodeTelemtVersionParams{ID: node.ID, TelemtVersion: version}); err != nil {
			u.log.Warn("record node telemt version", "node", node.ID, "err", err)
		}
	}
	if available != node.TelemtUpdateAvailable {
		if err := u.st.Q.SetNodeTelemtUpdateAvailable(ctx, db.SetNodeTelemtUpdateAvailableParams{ID: node.ID, TelemtUpdateAvailable: available}); err != nil {
			u.log.Warn("record node telemt update availability", "node", node.ID, "err", err)
		}
	}
}

func (u *TelemtUpdater) raiseAlert(ctx context.Context, node db.Node, status, errMsg string) {
	msg := "telemt update ended " + status
	if errMsg != "" {
		line, _, _ := strings.Cut(errMsg, "\n")
		msg += ": " + line
	}
	if _, err := u.st.Q.InsertAlert(ctx, db.InsertAlertParams{NodeID: nullUUID(node.ID), Kind: "telemt_update", Message: msg}); err != nil {
		u.log.Warn("record telemt update alert", "node", node.ID, "err", err)
	}
}
