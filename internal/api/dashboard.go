package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const recentJobsLimit = 20

func (s *Server) mountDashboard(r chi.Router) {
	r.Get("/dashboard/summary", s.handleDashboardSummary)
	r.Get("/alerts", s.handleListAlerts)
	r.With(RequireRole(writers...)).Post("/alerts/{id}/resolve", s.handleResolveAlert)
}

func (s *Server) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeRows, err := s.store.Q.CountNodesByStatus(ctx)
	if err != nil {
		internal(w)
		return
	}
	nodes := map[string]int{"pending": 0, "online": 0, "offline": 0, "degraded": 0}
	nodeTotal := 0
	for _, c := range nodeRows {
		nodes[string(c.Status)] = int(c.N)
		nodeTotal += int(c.N)
	}
	nodes["total"] = nodeTotal

	keyRows, err := s.store.Q.CountKeysByStatus(ctx)
	if err != nil {
		internal(w)
		return
	}
	keys := map[string]int{"pending": 0, "active": 0, "revoked": 0}
	keyTotal := 0
	for _, c := range keyRows {
		keys[string(c.Status)] = int(c.N)
		keyTotal += int(c.N)
	}
	keys["total"] = keyTotal

	snaps, err := s.store.Q.LatestSnapshots(ctx)
	if err != nil {
		internal(w)
		return
	}
	var sessionsLive, streamsLive int64
	var bytesUp, bytesDown int64
	for _, snap := range snaps {
		sessionsLive += int64(snap.SessionsLive)
		streamsLive += int64(snap.StreamsLive)
		bytesUp += snap.BytesUp
		bytesDown += snap.BytesDown
	}

	openAlerts, err := s.store.Q.ListOpenAlerts(ctx)
	if err != nil {
		internal(w)
		return
	}
	alerts := make([]map[string]any, 0, len(openAlerts))
	for _, a := range openAlerts {
		item := map[string]any{"id": a.ID, "kind": a.Kind, "message": a.Message, "created_at": a.CreatedAt}
		if a.NodeID.Valid {
			item["node_id"] = a.NodeID.UUID
		}
		if a.NodeName != nil {
			item["node_name"] = *a.NodeName
		}
		alerts = append(alerts, item)
	}

	jobRows, err := s.store.Q.ListRecentApplyJobs(ctx, recentJobsLimit)
	if err != nil {
		internal(w)
		return
	}
	jobs := make([]map[string]any, 0, len(jobRows))
	for _, j := range jobRows {
		jobs = append(jobs, map[string]any{
			"id": j.ID, "node_id": j.NodeID, "node_name": j.NodeName, "status": j.Status, "kind": j.Kind,
			"started_at": j.StartedAt, "finished_at": j.FinishedAt, "error": j.Error, "created_at": j.CreatedAt,
		})
	}

	writeJSON(w, 200, map[string]any{
		"nodes": nodes, "keys": keys, "sessions_live": sessionsLive, "streams_live": streamsLive,
		"bytes_up": bytesUp, "bytes_down": bytesDown, "alerts": alerts, "recent_jobs": jobs,
	})
}

type alertJSON struct {
	ID        int64      `json:"id"`
	NodeID    *uuid.UUID `json:"node_id,omitempty"`
	NodeName  string     `json:"node_name,omitempty"`
	Kind      string     `json:"kind"`
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListOpenAlerts(r.Context())
	if err != nil {
		internal(w)
		return
	}
	items := make([]alertJSON, 0, len(rows))
	for _, a := range rows {
		item := alertJSON{ID: a.ID, Kind: a.Kind, Message: a.Message, CreatedAt: a.CreatedAt}
		if a.NodeID.Valid {
			id := a.NodeID.UUID
			item.NodeID = &id
		}
		if a.NodeName != nil {
			item.NodeName = *a.NodeName
		}
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		notFound(w)
		return
	}
	if err := s.store.Q.ResolveAlert(r.Context(), id); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "alert.resolve", "alert", idStr, nil)
	writeJSON(w, 200, map[string]any{"resolved": true})
}
