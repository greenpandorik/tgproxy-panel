package agent

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

// Steps of the maintenance sequence, in the order they run.
const (
	UpdateStepPreflight = "preflight"
	UpdateStepDownload  = "download"
	UpdateStepPause     = "pause"
	UpdateStepDrain     = "drain"
	UpdateStepSwap      = "swap"
	UpdateStepRestart   = "restart"
	UpdateStepVerify    = "verify"
	UpdateStepResume    = "resume"
	UpdateStepRollback  = "rollback"
)

// Step states.
const (
	UpdateStateRunning = "running"
	UpdateStateOK      = "ok"
	UpdateStateFailed  = "failed"
	UpdateStateSkipped = "skipped"
)

// Phases of the whole run.
const (
	UpdatePhaseIdle    = "idle"
	UpdatePhaseRunning = "running"
	UpdatePhaseDone    = "done"
)

// Outcomes. Only UpdateOutcomeUpdated means the node is running the new build and serving.
const (
	UpdateOutcomeUpdated = "updated"
	// UpdateOutcomeRefused means the node was not touched.
	UpdateOutcomeRefused = "refused"
	// UpdateOutcomeFailed means the run stopped before the binary was swapped.
	UpdateOutcomeFailed = "failed"
	// UpdateOutcomeRolledBack means the new build failed and the previous one is healthy again.
	UpdateOutcomeRolledBack = "rolled_back"
	// UpdateOutcomeRollbackFailed means the new build failed and the node is still broken.
	UpdateOutcomeRollbackFailed = "rollback_failed"
	// UpdateOutcomeAdmissionClosed means the new build is healthy but WEB admission stayed shut.
	UpdateOutcomeAdmissionClosed = "updated_admission_closed"
)

const (
	defaultUpdatePoll        = time.Second
	defaultUpdateDrainSecs   = 60
	updateDownloadTimeout    = 15 * time.Minute
	updateRunTimeoutOverhead = 10 * time.Minute
)

var errUpdateInFlight = errors.New("a telemt update is already running on this node")

// telemtUpdate tracks one update so a concurrent status call sees the step the node is on.
type telemtUpdate struct {
	mu    sync.Mutex
	state *agentv1.TelemtUpdate
}

func newTelemtUpdate(from, to string) *telemtUpdate {
	return &telemtUpdate{state: &agentv1.TelemtUpdate{
		JobId: uuid.NewString(), Phase: UpdatePhaseRunning, FromVersion: from, ToVersion: to,
		StartedUnixMs: time.Now().UnixMilli(),
	}}
}

func (u *telemtUpdate) snapshot() *agentv1.TelemtUpdate {
	u.mu.Lock()
	defer u.mu.Unlock()
	return proto.Clone(u.state).(*agentv1.TelemtUpdate)
}

func (u *telemtUpdate) running() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state.Phase == UpdatePhaseRunning
}

func (u *telemtUpdate) begin(key, format string, a ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Steps = append(u.state.Steps, &agentv1.UpdateStep{
		Key: key, State: UpdateStateRunning, Message: fmt.Sprintf(format, a...),
		StartedUnixMs: time.Now().UnixMilli(),
	})
}

// step returns the last recorded step with this key.
func (u *telemtUpdate) step(key string) *agentv1.UpdateStep {
	for i := len(u.state.Steps) - 1; i >= 0; i-- {
		if u.state.Steps[i].Key == key {
			return u.state.Steps[i]
		}
	}
	return nil
}

func (u *telemtUpdate) progress(key string, sessions, streams uint64, format string, a ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.step(key)
	if s == nil {
		return
	}
	s.Message = fmt.Sprintf(format, a...)
	s.RemainingSessions, s.RemainingStreams = sessions, streams
}

func (u *telemtUpdate) finish(key, state, format string, a ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.step(key)
	if s == nil {
		s = &agentv1.UpdateStep{Key: key, StartedUnixMs: time.Now().UnixMilli()}
		u.state.Steps = append(u.state.Steps, s)
	}
	s.State, s.Message = state, fmt.Sprintf(format, a...)
	s.FinishedUnixMs = time.Now().UnixMilli()
}

func (u *telemtUpdate) skip(key, format string, a ...any) {
	u.finish(key, UpdateStateSkipped, format, a...)
}

func (u *telemtUpdate) setFrom(v string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.FromVersion = v
}

func (u *telemtUpdate) setDrained() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Drained = true
}

func (u *telemtUpdate) end(outcome string, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Phase, u.state.Outcome = UpdatePhaseDone, outcome
	u.state.Ok = outcome == UpdateOutcomeUpdated
	u.state.RolledBack = outcome == UpdateOutcomeRolledBack
	u.state.FinishedUnixMs = time.Now().UnixMilli()
	if err != nil {
		u.state.Error = err.Error()
	}
}

func (h *Handler) updatePollInterval() time.Duration {
	if h.updatePoll > 0 {
		return h.updatePoll
	}
	return defaultUpdatePoll
}

func (h *Handler) updateClient() *http.Client {
	if h.updateHTTP != nil {
		return h.updateHTTP
	}
	return &http.Client{Timeout: updateDownloadTimeout}
}

// TelemtUpdateStatus reports the update the node is running, or the last one it ran.
func (h *Handler) TelemtUpdateStatus() *agentv1.TelemtUpdate {
	h.updMu.Lock()
	u := h.upd
	h.updMu.Unlock()
	if u == nil {
		return &agentv1.TelemtUpdate{Phase: UpdatePhaseIdle}
	}
	return u.snapshot()
}

// StartTelemtUpdate begins the maintenance sequence and returns as soon as it is accepted;
// progress is read with TelemtUpdateStatus.
func (h *Handler) StartTelemtUpdate(ctx context.Context, req *agentv1.UpdateTelemtRequest) (*agentv1.TelemtUpdate, error) {
	if h.cfg.Engine != EngineTelemt || h.tm == nil {
		return nil, errors.New("this node does not run the telemt engine")
	}
	if !h.maintenance.TryLock() {
		return nil, errors.New("node maintenance is already in progress")
	}
	h.updMu.Lock()
	if h.upd != nil && h.upd.running() {
		h.updMu.Unlock()
		h.maintenance.Unlock()
		return nil, errUpdateInFlight
	}
	u := newTelemtUpdate("", strings.TrimSpace(req.GetVersion()))
	h.upd = u
	h.updMu.Unlock()

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx),
		time.Duration(drainTimeoutSecs(req))*time.Second+updateRunTimeoutOverhead)
	go func() {
		defer cancel()
		defer h.maintenance.Unlock()
		h.runTelemtUpdate(runCtx, u, req)
	}()
	return u.snapshot(), nil
}

func drainTimeoutSecs(req *agentv1.UpdateTelemtRequest) int {
	secs := int(req.GetDrainTimeoutSecs())
	if secs < telemt.MinDrainTimeoutSecs || secs > telemt.MaxDrainTimeoutSecs {
		return defaultUpdateDrainSecs
	}
	return secs
}

// updatePlan is what the preflight settled before anything on the node was touched.
type updatePlan struct {
	installed string
	drain     bool
	pause     bool
	resume    bool
	// drainSkip is why the drain will not run; empty when it will.
	drainSkip string
	hadWeb    bool
}

func (h *Handler) runTelemtUpdate(ctx context.Context, u *telemtUpdate, req *agentv1.UpdateTelemtRequest) {
	plan, err := h.updatePreflight(ctx, u, req)
	if err != nil {
		u.end(UpdateOutcomeRefused, err)
		return
	}

	u.begin(UpdateStepDownload, "downloading telemt %s", req.GetVersion())
	staged, err := h.stageTelemtBinary(ctx, req)
	if err != nil {
		u.finish(UpdateStepDownload, UpdateStateFailed, "%v", err)
		u.end(UpdateOutcomeRefused, err)
		return
	}
	defer func() { _ = os.Remove(staged) }()
	u.finish(UpdateStepDownload, UpdateStateOK, "downloaded and sha256 verified (%s)", shortSHA(req.GetSha256()))

	if err := h.updatePause(ctx, u, plan); err != nil {
		u.end(UpdateOutcomeFailed, err)
		return
	}
	paused := plan.pause
	if plan.drainSkip != "" {
		u.skip(UpdateStepDrain, "%s", plan.drainSkip)
	} else {
		paused = true
		if err := h.updateDrain(ctx, u, drainTimeoutSecs(req)); err != nil {
			recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			resumeErr := h.updateResume(recoveryCtx, u, plan, paused)
			cancel()
			u.end(UpdateOutcomeFailed, errors.Join(err, resumeErr))
			return
		}
	}

	u.begin(UpdateStepSwap, "replacing %s", h.cfg.TelemtBin)
	kept, err := h.swapTelemtBinary(staged)
	if err != nil {
		u.finish(UpdateStepSwap, UpdateStateFailed, "%v", err)
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		resumeErr := h.updateResume(recoveryCtx, u, plan, paused)
		cancel()
		u.end(UpdateOutcomeFailed, errors.Join(err, resumeErr))
		return
	}
	u.finish(UpdateStepSwap, UpdateStateOK, "%s replaced; %s", h.cfg.TelemtBin, kept.describe())

	if err := h.updateRestartAndVerify(ctx, u, plan, req.GetVersion()); err != nil {
		h.rollbackTelemt(ctx, u, plan, kept, err)
		return
	}
	if err := h.updateResume(ctx, u, plan, paused); err != nil {
		u.end(UpdateOutcomeAdmissionClosed, err)
		return
	}
	kept.discard()
	u.end(UpdateOutcomeUpdated, nil)
}

// updatePreflight settles what the node can do and refuses before anything is touched.
func (h *Handler) updatePreflight(ctx context.Context, u *telemtUpdate, req *agentv1.UpdateTelemtRequest) (updatePlan, error) {
	u.begin(UpdateStepPreflight, "checking the pinned build and what this node supports")
	fail := func(err error) (updatePlan, error) {
		u.finish(UpdateStepPreflight, UpdateStateFailed, "%v", err)
		return updatePlan{}, err
	}
	if strings.TrimSpace(req.GetVersion()) == "" {
		return fail(errors.New("the panel pinned no telemt version; nothing was changed"))
	}
	if err := validSHA256(req.GetSha256()); err != nil {
		return fail(fmt.Errorf("%w; nothing was changed", err))
	}
	if strings.TrimSpace(req.GetUrl()) == "" {
		return fail(errors.New("the panel published no download URL for this build; nothing was changed"))
	}

	info, err := h.tm.SystemInfo(ctx)
	if err != nil {
		return fail(fmt.Errorf("telemt's control API did not answer, so the running version cannot be read: %w", err))
	}
	plan := updatePlan{installed: ParseVersionOutput(info.Version)}
	if plan.installed == "" {
		plan.installed = strings.TrimSpace(info.Version)
	}
	u.setFrom(plan.installed)
	if sameTelemtVersion(plan.installed, req.GetVersion()) {
		return fail(fmt.Errorf("telemt %s is already installed; nothing was changed", plan.installed))
	}

	caps, unknown, err := h.tm.Capabilities(ctx)
	if err != nil {
		return fail(fmt.Errorf("could not work out what this node supports: %w", err))
	}
	plan.pause, plan.resume, plan.hadWeb = caps.WebPause, caps.WebResume, caps.WebRuntime
	switch {
	case caps.WebDrain:
		plan.drain = true
	case unknown.Has(telemt.CapWebDrain):
		if req.GetRequireDrain() {
			return fail(errors.New("this node's drain support could not be determined, so the update would have to drop live sessions blindly; nothing was changed"))
		}
		plan.drainSkip = "drain support could not be determined and the update was started without requiring it; live sessions are dropped by the restart"
	default:
		if req.GetRequireDrain() {
			return fail(errors.New("WEB drain is required but unsupported; nothing was changed"))
		}
		plan.drainSkip = "this node's telemt " + plan.installed + " has no WEB drain; live sessions are dropped by the restart"
	}
	u.finish(UpdateStepPreflight, UpdateStateOK, "telemt %s installed, %s pinned; %s",
		plan.installed, req.GetVersion(), describeDrainPlan(plan))
	return plan, nil
}

func sameTelemtVersion(a, b string) bool {
	normalize := func(v string) string {
		return strings.TrimPrefix(strings.TrimSpace(v), "v")
	}
	return normalize(a) != "" && normalize(a) == normalize(b)
}

func describeDrainPlan(p updatePlan) string {
	if p.drainSkip == "" {
		return "sessions will be drained first"
	}
	return p.drainSkip
}

func (h *Handler) stageTelemtBinary(ctx context.Context, req *agentv1.UpdateTelemtRequest) (string, error) {
	dir := filepath.Dir(h.cfg.TelemtBin)
	art := UpgradeArtifact{Version: req.GetVersion(), SHA256: req.GetSha256(), URL: req.GetUrl()}
	tgz, err := downloadArtifact(ctx, h.updateClient(), art, dir, ".telemt-download-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tgz) }()
	staged := filepath.Join(dir, ".telemt.new")
	if err := extractTelemtBinary(tgz, staged); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func (h *Handler) updatePause(ctx context.Context, u *telemtUpdate, plan updatePlan) error {
	if !plan.pause {
		u.skip(UpdateStepPause, "this node cannot pause WEB admission")
		return nil
	}
	u.begin(UpdateStepPause, "closing WEB admission")
	if err := h.tm.WebPause(ctx); err != nil {
		err = fmt.Errorf("pause WEB admission: %w", err)
		u.finish(UpdateStepPause, UpdateStateFailed, "%v; the binary was not touched", err)
		return err
	}
	u.finish(UpdateStepPause, UpdateStateOK, "no new WEB sessions are admitted")
	return nil
}

// updateDrain refuses to replace the binary when the drain cannot be verified.
func (h *Handler) updateDrain(ctx context.Context, u *telemtUpdate, timeoutSecs int) error {
	u.begin(UpdateStepDrain, "draining live sessions (up to %ds)", timeoutSecs)
	fail := func(err error) error {
		u.finish(UpdateStepDrain, UpdateStateFailed, "%v; binary unchanged", err)
		return err
	}
	if _, err := h.tm.WebDrain(ctx, timeoutSecs); err != nil {
		return fail(err)
	}
	drainCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second+5*time.Second)
	defer cancel()
	for {
		st, err := h.tm.WebStatus(drainCtx)
		if err != nil {
			return fail(err)
		}
		lc := st.OperatorLifecycle
		if lc == nil || lc.Drain == nil {
			return fail(errors.New("drain progress unavailable"))
		}
		sessions, streams := lc.Drain.RemainingSessions, lc.Drain.RemainingStreams
		u.progress(UpdateStepDrain, sessions, streams, "%s: %d session(s), %d stream(s) left", lc.State, sessions, streams)
		if lc.Drain.State == "cancelled" {
			return fail(errors.New("drain was cancelled"))
		}
		if lc.State == telemt.WebLifecycleDrained || lc.Drain.State == "completed" {
			if sessions != 0 || streams != 0 || lc.Drain.RemainingWebsockets != 0 {
				return fail(errors.New("drain ended with live sessions or streams"))
			}
			u.setDrained()
			u.finish(UpdateStepDrain, UpdateStateOK, "drained: no sessions or streams left")
			return nil
		}
		select {
		case <-drainCtx.Done():
			return fail(drainCtx.Err())
		case <-time.After(h.updatePollInterval()):
		}
	}
}

// keptFiles is what a rollback puts back: the binary and the config the update replaced.
type keptFiles struct {
	bin, binPrev       string
	config, configPrev string
}

func (k keptFiles) describe() string {
	if k.binPrev == "" {
		return "there was no previous binary to keep"
	}
	if k.configPrev == "" {
		return "the previous binary is kept as " + filepath.Base(k.binPrev)
	}
	return "the previous binary and config are kept as " + filepath.Base(k.binPrev) + " and " + filepath.Base(k.configPrev)
}

func (k keptFiles) discard() {
	if k.binPrev != "" {
		_ = os.Remove(k.binPrev)
	}
	if k.configPrev != "" {
		_ = os.Remove(k.configPrev)
	}
}

func (h *Handler) swapTelemtBinary(staged string) (keptFiles, error) {
	kept := keptFiles{bin: h.cfg.TelemtBin, config: h.cfg.TelemtConfigPath}
	prev, err := backupFile(kept.bin, 0o755)
	if err != nil {
		return kept, err
	}
	kept.binPrev = prev
	if kept.config != "" {
		cfgPrev, err := backupFile(kept.config, 0o600)
		if err != nil {
			return kept, fmt.Errorf("keep a copy of %s: %w", kept.config, err)
		}
		kept.configPrev = cfgPrev
	}
	if err := os.Rename(staged, kept.bin); err != nil {
		return kept, fmt.Errorf("install %s: %w", kept.bin, err)
	}
	if err := os.Chmod(kept.bin, 0o755); err != nil {
		restoreErr := restoreFile(kept.binPrev, kept.bin, 0o755)
		return kept, errors.Join(err, restoreErr)
	}
	return kept, nil
}

func (h *Handler) updateRestartAndVerify(ctx context.Context, u *telemtUpdate, plan updatePlan, want string) error {
	u.begin(UpdateStepRestart, "restarting telemt")
	if err := h.restartTelemt(ctx); err != nil {
		u.finish(UpdateStepRestart, UpdateStateFailed, "%v", err)
		return err
	}
	u.finish(UpdateStepRestart, UpdateStateOK, "telemt restarted and its control API reports ready")

	u.begin(UpdateStepVerify, "checking what is actually running")
	if err := h.verifyTelemtVersion(ctx, want); err != nil {
		u.finish(UpdateStepVerify, UpdateStateFailed, "%v", err)
		return err
	}
	if plan.hadWeb {
		if err := h.verifyConfiguredDecoy(ctx); err != nil {
			u.finish(UpdateStepVerify, UpdateStateFailed, "%v", err)
			return err
		}
		if _, err := h.tm.WebStatus(ctx); err != nil {
			err = fmt.Errorf("telemt %s is up but its WEB runtime does not answer: %w", want, err)
			u.finish(UpdateStepVerify, UpdateStateFailed, "%v", err)
			return err
		}
	}
	u.finish(UpdateStepVerify, UpdateStateOK, "telemt %s is running, ready and serving its control API", want)
	return nil
}

func (h *Handler) verifyTelemtVersion(ctx context.Context, want string) error {
	info, err := h.tm.SystemInfo(ctx)
	if err != nil {
		return fmt.Errorf("telemt's control API did not answer after the restart: %w", err)
	}
	if !sameVersion(info.Version, want) {
		return fmt.Errorf("telemt reports %s after the restart, not the pinned %s", strings.TrimSpace(info.Version), want)
	}
	return nil
}

// updateResume reopens admission and confirms from telemt that it is open.
func (h *Handler) updateResume(ctx context.Context, u *telemtUpdate, plan updatePlan, paused bool) error {
	if !paused {
		u.skip(UpdateStepResume, "admission was never closed")
		return nil
	}
	u.begin(UpdateStepResume, "reopening WEB admission")
	var resumeErr error
	if plan.resume {
		resumeErr = h.tm.WebResume(ctx)
	}
	st, err := h.tm.WebStatus(ctx)
	switch {
	case err != nil && resumeErr != nil:
		err = fmt.Errorf("reopening WEB admission failed: %w", resumeErr)
		u.finish(UpdateStepResume, UpdateStateFailed, "%v", err)
		return err
	case err != nil:
		u.finish(UpdateStepResume, UpdateStateFailed, "admission could not be verified: %v", err)
		return err
	case st.OperatorLifecycle == nil:
		err = errors.New("WEB admission status unavailable")
		u.finish(UpdateStepResume, UpdateStateFailed, "%v", err)
		return err
	case st.OperatorLifecycle.AdmissionOpen:
		u.finish(UpdateStepResume, UpdateStateOK, "WEB admission is open")
		return nil
	}
	err = fmt.Errorf("telemt is running the new build but WEB admission is still closed (state %s)", st.OperatorLifecycle.State)
	if resumeErr != nil {
		err = fmt.Errorf("%w: %w", err, resumeErr)
	}
	u.finish(UpdateStepResume, UpdateStateFailed, "%v", err)
	return err
}

// rollbackTelemt puts the previous binary and config back and reports which state the node
// ended in. A rollback that fails is never reported as a recovered node.
func (h *Handler) rollbackTelemt(ctx context.Context, u *telemtUpdate, plan updatePlan, kept keptFiles, cause error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	u.begin(UpdateStepRollback, "putting the previous telemt back: %v", cause)
	fail := func(err error) {
		u.finish(UpdateStepRollback, UpdateStateFailed, "%v", err)
		u.end(UpdateOutcomeRollbackFailed, fmt.Errorf("%w; the rollback also failed: %w", cause, err))
	}
	if err := restoreFile(kept.binPrev, kept.bin, 0o755); err != nil {
		fail(fmt.Errorf("restoring %s failed: %w", kept.bin, err))
		return
	}
	if kept.configPrev != "" {
		if err := restoreFile(kept.configPrev, kept.config, 0o600); err != nil {
			fail(fmt.Errorf("restoring %s failed: %w", kept.config, err))
			return
		}
	}
	if err := h.restartTelemt(ctx); err != nil {
		fail(fmt.Errorf("telemt did not come back on the previous binary: %w", err))
		return
	}
	if plan.installed != "" {
		if err := h.verifyTelemtVersion(ctx, plan.installed); err != nil {
			fail(err)
			return
		}
	}
	u.finish(UpdateStepRollback, UpdateStateOK, "telemt %s is running again and reports ready", plan.installed)
	if err := h.updateResume(ctx, u, plan, true); err != nil {
		u.end(UpdateOutcomeRollbackFailed, fmt.Errorf("%w; the previous binary is back but %w", cause, err))
		return
	}
	u.end(UpdateOutcomeRolledBack, cause)
}

func validSHA256(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("the panel published no sha256 for this build, so the download cannot be verified")
	}
	if raw, err := hex.DecodeString(s); err != nil || len(raw) != 32 {
		return fmt.Errorf("the panel published %q as the sha256 of this build, which is not a sha256", s)
	}
	return nil
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
