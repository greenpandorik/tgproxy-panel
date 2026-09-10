package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/nodeinstall"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

const (
	// telemtUpdateDrainDefault is the drain budget when the operator names none.
	telemtUpdateDrainDefault = 120
	telemtUpdateDrainMax     = 3600
	telemtUpdateHistoryLimit = 20
	telemtUpdateHistoryMax   = 100
	telemtVerifyTimeout      = 30 * time.Second
)

type telemtUpdateBody struct {
	DrainTimeoutSecs int `json:"drain_timeout_secs"`
	// RequireDrain defaults to true: an undetermined drain capability stops the update
	// rather than silently dropping live sessions.
	RequireDrain *bool `json:"require_drain"`
}

type telemtUpdateJobJSON struct {
	ID           string                  `json:"id"`
	NodeID       string                  `json:"node_id"`
	Status       string                  `json:"status"`
	Outcome      string                  `json:"outcome"`
	FromVersion  string                  `json:"from_version"`
	ToVersion    string                  `json:"to_version"`
	Error        string                  `json:"error"`
	Steps        []nodedriver.UpdateStep `json:"steps"`
	Verification json.RawMessage         `json:"verification"`
	StartedAt    time.Time               `json:"started_at"`
	FinishedAt   *time.Time              `json:"finished_at"`
}

func telemtUpdateJobFrom(row db.TelemtUpdateJob) telemtUpdateJobJSON {
	out := telemtUpdateJobJSON{
		ID: row.ID.String(), NodeID: row.NodeID.String(), Status: row.Status, Outcome: row.Outcome,
		FromVersion: row.FromVersion, ToVersion: row.ToVersion, Error: row.Error,
		Steps: []nodedriver.UpdateStep{}, Verification: json.RawMessage(row.Verification),
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
	}
	if len(row.Steps) > 0 {
		_ = json.Unmarshal(row.Steps, &out.Steps)
	}
	return out
}

// pinnedTelemt is the build this panel says every telemt node should run.
func (s *Server) pinnedTelemt() (nodedriver.TelemtUpdateRequest, error) {
	if s.cfg.TelemtSHA256 == "" {
		return nodedriver.TelemtUpdateRequest{}, errors.New("this panel has no sha256 for telemt " + s.cfg.TelemtVersion + ", so the download cannot be verified; set TELEMT_SHA256_X86_64 before updating a node")
	}
	return nodedriver.TelemtUpdateRequest{
		Version: s.cfg.TelemtVersion, SHA256: s.cfg.TelemtSHA256,
		URL: nodeinstall.TelemtReleaseURL(s.cfg.TelemtVersion),
	}, nil
}

// updater is the panel's telemt update runner.
func (s *Server) updater() *worker.TelemtUpdater { return s.telemtUpdater }

func (s *Server) handleStartNodeTelemtUpdate(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if n.Engine != db.NodeEngineTelemt {
		conflict(w, "this node does not run the telemt engine")
		return
	}
	var body telemtUpdateBody
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			validation(w, map[string]string{"body": "invalid JSON"})
			return
		}
	}
	if body.DrainTimeoutSecs < 0 || body.DrainTimeoutSecs > telemtUpdateDrainMax {
		validation(w, map[string]string{"drain_timeout_secs": "must be between 0 and " + strconv.Itoa(telemtUpdateDrainMax)})
		return
	}
	req, err := s.pinnedTelemt()
	if err != nil {
		writeError(w, http.StatusConflict, "unpinned_telemt", err.Error(), nil)
		return
	}
	if sameVersion(n.TelemtVersion, req.Version) {
		conflict(w, "this node already runs telemt "+req.Version)
		return
	}
	req.DrainTimeoutSecs = body.DrainTimeoutSecs
	if req.DrainTimeoutSecs == 0 {
		req.DrainTimeoutSecs = telemtUpdateDrainDefault
	}
	req.RequireDrain = body.RequireDrain == nil || *body.RequireDrain

	job, err := s.updater().Start(r.Context(), n, req)
	switch {
	case errors.Is(err, worker.ErrUpdateInFlight):
		conflict(w, err.Error())
		return
	case err != nil:
		s.driverErr(w, err)
		return
	}
	s.Audit(r.Context(), "node.telemt_update", "node", n.ID.String(),
		map[string]any{"to_version": req.Version, "drain_timeout_secs": req.DrainTimeoutSecs, "require_drain": req.RequireDrain})
	writeJSON(w, http.StatusAccepted, telemtUpdateJobFrom(job))
}

func sameVersion(a, b string) bool {
	normalize := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
	return normalize(a) != "" && normalize(a) == normalize(b)
}

// handleListNodeTelemtUpdates is how the panel follows a running update and reads past ones.
func (s *Server) handleListNodeTelemtUpdates(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if err := s.updater().Reconcile(r.Context(), n); err != nil {
		s.log.Warn("recover telemt update", "node", n.ID, "err", err)
	}
	limit := telemtUpdateHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > telemtUpdateHistoryMax {
			validation(w, map[string]string{"limit": "must be between 1 and " + strconv.Itoa(telemtUpdateHistoryMax)})
			return
		}
		limit = v
	}
	rows, err := s.store.Q.ListNodeTelemtUpdateJobs(r.Context(), db.ListNodeTelemtUpdateJobsParams{NodeID: n.ID, Limit: int32(limit)})
	if err != nil {
		internal(w)
		return
	}
	items := make([]telemtUpdateJobJSON, 0, len(rows))
	var running *telemtUpdateJobJSON
	for _, row := range rows {
		job := telemtUpdateJobFrom(row)
		if row.FinishedAt == nil && running == nil {
			r := job
			running = &r
		}
		items = append(items, job)
	}
	writeJSON(w, 200, map[string]any{
		"items": items, "running": running,
		"installed_version": n.TelemtVersion, "pinned_version": s.cfg.TelemtVersion,
		"update_available": n.TelemtUpdateAvailable,
	})
}

// VerifyNode is the panel's own post-update check: a full diagnostics pass, kept with the
// node's history, so a node is only called updated when something other than the node itself
// says it is serving.
func (s *Server) VerifyNode(ctx context.Context, n db.Node) (bool, []byte, string) {
	ctx, cancel := context.WithTimeout(ctx, telemtVerifyTimeout)
	defer cancel()
	run := s.diagEngine().Run(ctx, diagTarget(n), domain.TriggerPostUpdate)
	if err := s.saveDiagnostics(ctx, n.ID, &run); err != nil {
		s.log.Error("store post-update diagnostics", "err", err, "node", n.ID)
	}
	raw, err := json.Marshal(run)
	if err != nil {
		raw = nil
	}
	summary := fmt.Sprintf("%s, %d of %d checks passed", run.Status, run.Passed, run.Total)
	ok := run.Status != domain.DiagnosticsOffline && run.Status != domain.DiagnosticsUnknown && !failedTelemtChecks(run)
	return ok, raw, summary
}

// failedTelemtChecks reports whether a check about the engine we just replaced failed.
func failedTelemtChecks(run domain.DiagnosticsRun) bool {
	for _, g := range run.Groups {
		if g.Key != domain.GroupTelemt && g.Key != domain.GroupWebTransport {
			continue
		}
		for _, c := range g.Checks {
			if c.Status == domain.CheckFail {
				return true
			}
		}
	}
	return false
}
