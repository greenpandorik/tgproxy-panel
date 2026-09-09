package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodediag"
	"tgwebproxy/internal/store/db"
)

const (
	// diagRunTimeout is the hard ceiling on one pass, so the handler always answers.
	diagRunTimeout = 25 * time.Second
	// diagStoreTimeout bounds the write that keeps a finished pass.
	diagStoreTimeout      = 5 * time.Second
	diagHistoryDefault    = 20
	diagHistoryMax        = 100
	diagPanelConcurrency  = 4
	diagUnavailableReason = "too many diagnostics passes are running; try again shortly"
)

var (
	errDiagNodeBusy  = errors.New("a diagnostics pass is already running for this node")
	errDiagPanelBusy = errors.New(diagUnavailableReason)
)

// acquireDiag reserves this node's single diagnostics slot and one of the panel-wide slots.
// The returned closure gives both back.
func (s *Server) acquireDiag(id uuid.UUID) (func(), error) {
	s.diagMu.Lock()
	if _, running := s.diagRuns[id]; running {
		s.diagMu.Unlock()
		return nil, errDiagNodeBusy
	}
	s.diagRuns[id] = struct{}{}
	s.diagMu.Unlock()

	select {
	case s.diagSlots <- struct{}{}:
	default:
		s.releaseDiagNode(id)
		return nil, errDiagPanelBusy
	}
	return func() {
		<-s.diagSlots
		s.releaseDiagNode(id)
	}, nil
}

func (s *Server) releaseDiagNode(id uuid.UUID) {
	s.diagMu.Lock()
	delete(s.diagRuns, id)
	s.diagMu.Unlock()
}

func (s *Server) diagEngine() *nodediag.Engine {
	return &nodediag.Engine{Checker: s.checker(), Node: s.driver, Timeout: diagRunTimeout}
}

func diagTarget(n db.Node) nodediag.Target {
	t := nodediag.Target{
		NodeID:   n.ID,
		Hostname: n.Hostname,
		PublicIP: n.PublicIp,
		Telemt:   n.Engine == db.NodeEngineTelemt,
	}
	if t.Telemt {
		t.TLSDomain, t.ClassicPort = n.TlsDomain, int(n.ClassicPort)
	}
	return t
}

func (s *Server) handleNodeWebDiagnostics(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	release, err := s.acquireDiag(n.ID)
	switch {
	case errors.Is(err, errDiagNodeBusy):
		conflict(w, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "busy", err.Error(), nil)
		return
	}
	defer release()

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), diagRunTimeout)
	defer cancel()
	run := s.diagEngine().Run(runCtx, diagTarget(n), domain.TriggerManual)

	storeCtx, cancelStore := context.WithTimeout(context.WithoutCancel(r.Context()), diagStoreTimeout)
	defer cancelStore()
	if err := s.saveDiagnostics(storeCtx, n.ID, &run); err != nil {
		s.log.Error("store diagnostics", "err", err, "node", n.ID)
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.diagnostics", "node", n.ID.String(),
		map[string]any{"overall_status": string(run.Status), "passed": run.Passed, "total": run.Total, "not_run": run.NotRun})
	writeJSON(w, 200, run)
}

func (s *Server) saveDiagnostics(ctx context.Context, nodeID uuid.UUID, run *domain.DiagnosticsRun) error {
	raw, err := json.Marshal(run.Groups)
	if err != nil {
		return err
	}
	row, err := s.store.Q.InsertNodeDiagnostics(ctx, db.InsertNodeDiagnosticsParams{
		NodeID:        nodeID,
		StartedAt:     run.StartedAt,
		FinishedAt:    run.FinishedAt,
		OverallStatus: string(run.Status),
		Trigger:       string(run.Trigger),
		Checks:        raw,
	})
	if err != nil {
		return err
	}
	run.ID = row.ID
	return nil
}

func (s *Server) handleListNodeDiagnostics(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	limit := diagHistoryDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > diagHistoryMax {
			validation(w, map[string]string{"limit": "must be between 1 and " + strconv.Itoa(diagHistoryMax)})
			return
		}
		limit = v
	}
	rows, err := s.store.Q.ListNodeDiagnostics(r.Context(), db.ListNodeDiagnosticsParams{NodeID: n.ID, Limit: int32(limit)})
	if err != nil {
		internal(w)
		return
	}
	items := make([]domain.DiagnosticsRun, 0, len(rows))
	for _, row := range rows {
		items = append(items, diagnosticsRunJSON(row))
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

// diagnosticsRunJSON rebuilds a stored run: the counts come from the checks, the overall
// status from the pass that recorded it.
func diagnosticsRunJSON(row db.NodeDiagnostic) domain.DiagnosticsRun {
	run := domain.DiagnosticsRun{
		ID:         row.ID,
		NodeID:     row.NodeID.String(),
		StartedAt:  row.StartedAt,
		FinishedAt: row.FinishedAt,
		Trigger:    domain.DiagnosticsTrigger(row.Trigger),
		Groups:     []domain.DiagnosticGroup{},
	}
	if len(row.Checks) > 0 {
		_ = json.Unmarshal(row.Checks, &run.Groups)
	}
	run.Tally()
	run.Status = domain.DiagnosticsStatus(row.OverallStatus)
	return run
}
