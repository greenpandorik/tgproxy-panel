package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type fleetRollout struct {
	ID         uuid.UUID                      `json:"id"`
	Status     string                         `json:"status"`
	Request    nodedriver.TelemtUpdateRequest `json:"-"`
	NodeIDs    []uuid.UUID                    `json:"node_ids"`
	Cursor     int                            `json:"cursor"`
	DispatchAt *time.Time                     `json:"dispatch_at"`
	JobID      *uuid.UUID                     `json:"job_id"`
	Error      string                         `json:"error"`
	CreatedAt  time.Time                      `json:"created_at"`
	UpdatedAt  time.Time                      `json:"updated_at"`
}

const fleetColumns = "id,status,request,node_ids,cursor,dispatch_at,job_id,error,created_at,updated_at"

func scanRollout(row pgx.Row) (fleetRollout, error) {
	var v fleetRollout
	var req, ids []byte
	err := row.Scan(&v.ID, &v.Status, &req, &ids, &v.Cursor, &v.DispatchAt, &v.JobID, &v.Error, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(req, &v.Request); err != nil {
		return v, err
	}
	err = json.Unmarshal(ids, &v.NodeIDs)
	return v, err
}

func (s *Server) handleFleetList(w http.ResponseWriter, r *http.Request) {
	rows, e := s.store.Pool.Query(r.Context(), "SELECT "+fleetColumns+" FROM fleet_rollouts ORDER BY created_at DESC LIMIT 20")
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []fleetRollout{}
	for rows.Next() {
		v, e := scanRollout(rows)
		if e != nil {
			internal(w)
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "version": s.cfg.TelemtVersion})
}

func (s *Server) handleFleetStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeIDs []uuid.UUID `json:"node_ids"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body) != nil || len(body.NodeIDs) == 0 || len(body.NodeIDs) > 100 {
		badRequest(w, "select 1 to 100 nodes in rollout order")
		return
	}
	req, e := s.pinnedTelemt()
	if e != nil {
		conflict(w, e.Error())
		return
	}
	req.RequireDrain = true
	req.DrainTimeoutSecs = 120
	seen := map[uuid.UUID]bool{}
	for _, id := range body.NodeIDs {
		n, e := s.store.Q.GetNode(r.Context(), id)
		if e != nil || seen[id] || n.Engine != db.NodeEngineTelemt {
			badRequest(w, "each node must be a distinct telemt node")
			return
		}
		seen[id] = true
		if !s.driver.Online(id) {
			conflict(w, "all selected nodes must be online")
			return
		}
		if _, e = s.store.Q.GetRunningTelemtUpdateJob(r.Context(), id); !errors.Is(e, pgx.ErrNoRows) {
			conflict(w, "a node has an update in progress")
			return
		}
	}
	request, _ := json.Marshal(req)
	ids, _ := json.Marshal(body.NodeIDs)
	id := uuid.New()
	_, e = s.store.Pool.Exec(r.Context(), "INSERT INTO fleet_rollouts(id,request,node_ids) VALUES($1,$2,$3)", id, request, ids)
	if e != nil {
		conflict(w, "another rollout may already be running")
		return
	}
	s.Audit(r.Context(), "fleet.update", "fleet", id.String(), map[string]any{"node_ids": body.NodeIDs, "version": req.Version})
	writeJSON(w, 202, map[string]any{"id": id})
}

func (s *Server) handleFleetStop(w http.ResponseWriter, r *http.Request) {
	id, e := uuid.Parse(chi.URLParam(r, "id"))
	if e != nil {
		notFound(w)
		return
	}
	tag, e := s.store.Pool.Exec(r.Context(), "UPDATE fleet_rollouts SET status='stopped',updated_at=now() WHERE id=$1 AND status='running'", id)
	if e != nil {
		internal(w)
		return
	}
	if tag.RowsAffected() == 0 {
		conflict(w, "rollout is not running")
		return
	}
	s.Audit(r.Context(), "fleet.stop", "fleet", id.String(), nil)
	writeJSON(w, 200, map[string]bool{"stopped": true})
}

// RunOperations resumes durable rollouts; a session advisory lock excludes a second panel.
func (s *Server) RunOperations(ctx context.Context, alerts *worker.Alerts) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, 90*time.Second)
		s.fleetTick(call)
		s.scheduledDiagnostics(call, alerts)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) fleetTick(ctx context.Context) {
	conn, e := s.store.Pool.Acquire(ctx)
	if e != nil {
		return
	}
	defer conn.Release()
	var locked bool
	if conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(74190016)").Scan(&locked) != nil || !locked {
		return
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(c, "SELECT pg_advisory_unlock(74190016)"); e != nil {
			_ = conn.Conn().Close(c)
		}
	}()
	v, e := scanRollout(conn.QueryRow(ctx, "SELECT "+fleetColumns+" FROM fleet_rollouts WHERE status='running' LIMIT 1"))
	if e != nil {
		return
	}
	stop := func(reason string) {
		_, _ = conn.Exec(ctx, "UPDATE fleet_rollouts SET status='failed',error=$2,updated_at=now() WHERE id=$1 AND status='running'", v.ID, reason)
	}
	if v.Cursor >= len(v.NodeIDs) {
		_, _ = conn.Exec(ctx, "UPDATE fleet_rollouts SET status='complete',updated_at=now() WHERE id=$1 AND status='running'", v.ID)
		return
	}
	n, e := s.store.Q.GetNode(ctx, v.NodeIDs[v.Cursor])
	if e != nil {
		stop("selected node was removed")
		return
	}
	if v.DispatchAt != nil {
		// Recover the hand-off window without ever issuing a second Update request.
		if v.JobID == nil {
			var id uuid.UUID
			e = conn.QueryRow(ctx, "SELECT id FROM telemt_update_jobs WHERE node_id=$1 AND started_at >= $2 ORDER BY started_at LIMIT 1", n.ID, *v.DispatchAt).Scan(&id)
			if e != nil {
				stop("dispatch was interrupted; inspect the node before starting a new rollout")
				return
			}
			v.JobID = &id
			_, e = conn.Exec(ctx, "UPDATE fleet_rollouts SET job_id=$2 WHERE id=$1", v.ID, id)
			if e != nil {
				return
			}
		}
		if e = s.updater().Reconcile(ctx, n); e != nil {
			return
		}
		var status string
		var finished *time.Time
		if conn.QueryRow(ctx, "SELECT status,finished_at FROM telemt_update_jobs WHERE id=$1", *v.JobID).Scan(&status, &finished) != nil {
			return
		}
		if status == "running" {
			return
		}
		if status != "ok" {
			stop("node update ended " + status + "; remaining nodes were not started")
			return
		}
		if finished == nil {
			return
		}
		h, e := s.driver.Health(ctx, n.ID)
		if e != nil || !h.Healthz || !h.Readyz {
			stop("post-update observation failed; rollout stopped")
			return
		}
		var report reliability.Report
		if json.Unmarshal(h.Reliability, &report) == nil {
			for _, f := range reliability.Findings(report, time.Now()) {
				if f.Known && f.Failed {
					stop(f.Message)
					return
				}
			}
		}
		if time.Since(*finished) < time.Minute {
			return
		}
		_, _ = conn.Exec(ctx, "UPDATE fleet_rollouts SET cursor=cursor+1,dispatch_at=NULL,job_id=NULL,updated_at=now() WHERE id=$1 AND status='running'", v.ID)
		return
	}
	if sameVersion(n.TelemtVersion, v.Request.Version) {
		_, _ = conn.Exec(ctx, "UPDATE fleet_rollouts SET cursor=cursor+1,updated_at=now() WHERE id=$1 AND status='running'", v.ID)
		return
	}
	h, e := s.driver.Health(ctx, n.ID)
	if e != nil || !h.Healthz || !h.Readyz {
		stop("pre-update health check failed")
		return
	}
	var health reliability.Report
	if json.Unmarshal(h.Reliability, &health) == nil && health.Policy.Maintenance {
		stop("node is in maintenance mode")
		return
	}
	// Commit dispatch intent first. Stop may race here, so the guarded UPDATE is decisive.
	tag, e := conn.Exec(ctx, "UPDATE fleet_rollouts SET dispatch_at=now(),updated_at=now() WHERE id=$1 AND status='running'", v.ID)
	if e != nil || tag.RowsAffected() == 0 {
		return
	}
	job, e := s.updater().Start(ctx, n, v.Request)
	if e != nil {
		stop(e.Error())
		return
	}
	_, _ = conn.Exec(ctx, "UPDATE fleet_rollouts SET job_id=$2,updated_at=now() WHERE id=$1", v.ID, job.ID)
}
