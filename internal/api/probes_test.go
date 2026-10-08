package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/reliability"
)

func TestProbeAuthenticationAndReplay(t *testing.T) {
	h := apitest.New(t, func(d *api.Deps) {
		d.Cfg.ProbeToken = strings.Repeat("x", 32)
		d.Cfg.ProbeLocations = []string{"isp-a"}
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")
	node, _ := createNode(t, owner, "probe.test")
	anon := h.Anonymous()
	ok := reliability.ProbeCheck{Status: "ok"}
	unknown := reliability.ProbeCheck{Status: "not_run"}
	p := reliability.ProbeReport{NodeID: node.ID.String(), Location: "isp-a", At: time.Now().UTC(), TLS: ok, HTTP: ok, FakeTLS: unknown, WEB: unknown}
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	anon.Headers.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 202 {
		t.Fatalf("probe: %d", code)
	}
	p.At = p.At.Add(-time.Minute)
	p.TLS.Status = "failed"
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 202 {
		t.Fatalf("old report: %d", code)
	}
	var raw []byte
	if e := h.Store.Pool.QueryRow(t.Context(), "SELECT report FROM probe_reports WHERE node_id=$1", node.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var kept reliability.ProbeReport
	if e := json.Unmarshal(raw, &kept); e != nil {
		t.Fatal(e)
	}
	if kept.TLS.Status != "ok" {
		t.Fatal("replay replaced newer report")
	}
	p.Location = "untrusted"
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 422 {
		t.Fatalf("unknown location: %d", code)
	}
}

func TestFleetRolloutPersistenceAndStop(t *testing.T) {
	h, c, node := ownerWithNode(t)
	_, e := h.Store.Pool.Exec(t.Context(), "UPDATE nodes SET engine='telemt' WHERE id=$1", node.ID)
	if e != nil {
		t.Fatal(e)
	}
	h.Mock.SetOnline(node.ID, true)
	var result struct {
		ID string `json:"id"`
	}
	response := c.Post("/api/v1/fleet/updates", map[string]any{"node_ids": []string{node.ID.String()}})
	if response.StatusCode != 202 {
		t.Fatalf("start: %d", statusOf(t, response))
	}
	c.JSON(response, &result)
	if result.ID == "" {
		t.Fatal("no durable id")
	}
	if code := statusOf(t, c.Post("/api/v1/fleet/updates", map[string]any{"node_ids": []string{node.ID.String()}})); code != 409 {
		t.Fatalf("overlap: %d", code)
	}
	if code := statusOf(t, c.Post("/api/v1/fleet/updates/"+result.ID+"/stop", nil)); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	var status string
	if e = h.Store.Pool.QueryRow(t.Context(), "SELECT status FROM fleet_rollouts WHERE id=$1", result.ID).Scan(&status); e != nil || status != "stopped" {
		t.Fatalf("durable stop: %s %v", status, e)
	}
}

func TestProbeIngestionRetainsAuthenticatedHistoryBeforeWorkerPoll(t *testing.T) {
	h := apitest.New(t, func(d *api.Deps) {
		d.Cfg.ProbeToken = strings.Repeat("x", 32)
		d.Cfg.ProbeLocations = []string{"isp-a"}
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")
	node, _ := createNode(t, owner, "history.test")
	anon := h.Anonymous()
	anon.Headers.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	marked := reliability.ProbeCheck{Status: "ok", Method: reliability.AuthenticatedMTProto}
	notRun := reliability.ProbeCheck{Status: "not_run"}
	p := reliability.ProbeReport{NodeID: node.ID.String(), Location: "isp-a", At: time.Now().UTC(), TLS: marked, HTTP: marked, FakeTLS: marked, WEB: marked}
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 202 {
		t.Fatalf("first report: %d", code)
	}
	p.At = p.At.Add(time.Second)
	p.FakeTLS = notRun
	p.WEB.Status = "failed"
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 202 {
		t.Fatalf("replacement: %d", code)
	}
	var count int
	if e := h.Store.Pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_findings WHERE node_id=$1 AND source='blocking_evidence' AND kind=ANY($2::text[])`, node.ID, []string{"isp-a|faketls", "isp-a|web"}).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 2 {
		t.Fatalf("replacement before poll lost authenticated history: %d", count)
	}
	if e := h.Store.Pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_findings WHERE node_id=$1 AND source='blocking_evidence'`, node.ID).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 2 {
		t.Fatalf("public checks became authenticated history: %d", count)
	}
}

func TestReplayedMarkedProbeDoesNotCreateAuthenticatedHistory(t *testing.T) {
	h := apitest.New(t, func(d *api.Deps) {
		d.Cfg.ProbeToken = strings.Repeat("x", 32)
		d.Cfg.ProbeLocations = []string{"isp-a"}
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")
	node, _ := createNode(t, owner, "replay-history.test")
	anon := h.Anonymous()
	anon.Headers.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	marked := reliability.ProbeCheck{Status: "ok", Method: reliability.AuthenticatedMTProto}
	notRun := reliability.ProbeCheck{Status: "not_run"}
	p := reliability.ProbeReport{NodeID: node.ID.String(), Location: "isp-a", At: time.Now().UTC(), TLS: notRun, HTTP: notRun, FakeTLS: notRun, WEB: marked}
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 202 {
		t.Fatal(code)
	}
	p.At = p.At.Add(-time.Minute)
	p.FakeTLS = marked
	p.WEB = notRun
	if code := statusOf(t, anon.Post("/api/v1/probes/report", p)); code != 202 {
		t.Fatal(code)
	}
	var count int
	if e := h.Store.Pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_findings WHERE node_id=$1 AND source='blocking_evidence' AND kind='isp-a|faketls'`, node.ID).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 {
		t.Fatal("replay added authenticated history")
	}
	if e := h.Store.Pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_findings WHERE node_id=$1 AND source='blocking_evidence' AND kind='isp-a|web'`, node.ID).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 1 {
		t.Fatal("newer report did not retain WEB history")
	}
}
