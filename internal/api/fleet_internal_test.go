package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/worker"
)

func fleetFixture(t *testing.T) (*Server, *nodedriver.Mock, uuid.UUID, uuid.UUID) {
	t.Helper()
	st := store.OpenTest(t)
	driver := nodedriver.NewMock()
	log := slog.New(slog.DiscardHandler)
	s := &Server{store: st, driver: driver, log: log, telemtUpdater: worker.NewTelemtUpdater(st, driver, log)}
	node, rollout := uuid.New(), uuid.New()
	_, e := st.Pool.Exec(t.Context(), "INSERT INTO nodes(id,name,hostname,engine,telemt_version) VALUES($1,'fixture','fixture.test','telemt','3.5.6')", node)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := json.Marshal(nodedriver.TelemtUpdateRequest{Version: "3.5.7", RequireDrain: true})
	ids, _ := json.Marshal([]uuid.UUID{node})
	_, e = st.Pool.Exec(t.Context(), "INSERT INTO fleet_rollouts(id,request,node_ids) VALUES($1,$2,$3)", rollout, req, ids)
	if e != nil {
		t.Fatal(e)
	}
	driver.SetOnline(node, true)
	t.Cleanup(s.telemtUpdater.Wait)
	return s, driver, node, rollout
}

func TestFleetInterruptedDispatchNeverResends(t *testing.T) {
	s, driver, node, id := fleetFixture(t)
	_, e := s.store.Pool.Exec(t.Context(), "UPDATE fleet_rollouts SET dispatch_at=now() WHERE id=$1", id)
	if e != nil {
		t.Fatal(e)
	}
	s.fleetTick(t.Context())
	var status string
	if e = s.store.Pool.QueryRow(t.Context(), "SELECT status FROM fleet_rollouts WHERE id=$1", id).Scan(&status); e != nil {
		t.Fatal(e)
	}
	if status != "failed" || len(driver.TelemtUpdateRequests(node)) != 0 {
		t.Fatalf("ambiguous dispatch repeated, status %s", status)
	}
}

func TestFleetHealthGateStopsBeforeNextNode(t *testing.T) {
	s, driver, node, id := fleetFixture(t)
	job := uuid.New()
	_, e := s.store.Pool.Exec(t.Context(), "INSERT INTO telemt_update_jobs(id,node_id,from_version,to_version,status,finished_at) VALUES($1,$2,'3.5.6','3.5.7','ok',now()-interval '2 minutes')", job, node)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.store.Pool.Exec(t.Context(), "UPDATE fleet_rollouts SET dispatch_at=now()-interval '3 minutes',job_id=$2 WHERE id=$1", id, job)
	if e != nil {
		t.Fatal(e)
	}
	report, _ := json.Marshal(reliability.Report{Version: 1, At: time.Now(), Routes: []reliability.Route{{Healthy: false, Age: 1}}})
	driver.SetHealth(node, nodedriver.HealthReport{Healthz: true, Readyz: true, Reliability: report})
	s.fleetTick(context.Background())
	var status string
	var cursor int
	if e = s.store.Pool.QueryRow(t.Context(), "SELECT status,cursor FROM fleet_rollouts WHERE id=$1", id).Scan(&status, &cursor); e != nil {
		t.Fatal(e)
	}
	if status != "failed" || cursor != 0 || len(driver.TelemtUpdateRequests(node)) != 0 {
		t.Fatalf("unhealthy node passed gate: %s %d", status, cursor)
	}
}
