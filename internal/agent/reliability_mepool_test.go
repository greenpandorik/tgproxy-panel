package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

const mePayloadFixture = `{"generations":{"active_generation":7,"warm_generation":8,"warm_generations":[8],"pending_hardswap_generation":8,"pending_hardswap_age_secs":1600,"reinit_inflight":1,"reinit_max_concurrency_effective":2,"draining_generations":[6]},"hardswap":{"enabled":true,"pending":true,"pending_writers_current":2,"pending_writer_deficit":3,"pending_missing_dc_groups":1,"pending_map_current":true,"orphan_warm_writers_current":1,"replacement_preparing_current":2,"replacement_retiring_current":0},"writers":{"total":6,"alive_non_draining":5,"draining":1,"degraded":0,"contour":{"warm":3,"active":2,"draining":1},"health":{"healthy":5,"degraded":0,"draining":1}},"refill":{"inflight_endpoints_total":2,"inflight_dc_total":1,"running_dc_total":1,"pending_dc_total":0,"by_dc":[{"dc":2,"family":"ipv4","inflight":2}]}}`

func TestReliabilityHealthCollectsAuthoritativeMEPoolProjection(t *testing.T) {
	generated := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runtime/me-pool-state" {
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer control" {
				t.Errorf("request: %s auth=%q", r.Method, r.Header.Get("Authorization"))
			}
			_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"enabled":true,"generated_at_epoch_secs":%d,"data":%s}}`, generated.Unix(), mePayloadFixture)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"middle_proxy_enabled":false,"dcs":[]}}`))
	}))
	defer srv.Close()
	h := &Handler{cfg: Config{StateDir: t.TempDir()}, exec: &fakeExec{}, tm: telemt.New(srv.URL, "Bearer control")}
	var rep agentv1.HealthReport
	h.reliabilityHealth(context.Background(), &rep, nil)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rep.ReliabilityJson, &fields); err != nil {
		t.Fatal(err)
	}
	raw, ok := fields["me_pool"]
	if !ok {
		t.Fatal("ME pool source missing from reliability report")
	}
	var pool struct {
		At      time.Time
		Enabled bool
		State   struct {
			ActiveGeneration uint64  `json:"active_generation"`
			PendingAge       uint64  `json:"pending_age_seconds"`
			Deficit          uint64  `json:"writer_deficit"`
			OrphanObserved   *uint64 `json:"orphan_observed_seconds"`
		}
	}
	if err := json.Unmarshal(raw, &pool); err != nil {
		t.Fatal(err)
	}
	if !pool.Enabled || !pool.At.Equal(generated) || pool.State.ActiveGeneration != 7 || pool.State.PendingAge != 1600 || pool.State.Deficit != 3 || pool.State.OrphanObserved == nil || *pool.State.OrphanObserved != 0 {
		t.Fatalf("projection %s", raw)
	}
	var report reliability.Report
	if err := json.Unmarshal(rep.ReliabilityJson, &report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range reliability.Findings(report, time.Now()) {
		if f.Kind == "me_hardswap_pending" && f.Failed && f.Known {
			found = true
		}
	}
	if !found {
		t.Fatal("projected pending age did not produce diagnostic")
	}
}

func TestReliabilityHealthMEUnavailableAndOldAPIStayUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		present bool
	}{
		{"old endpoint", 404, `{"ok":false,"error":{"code":"not_found","message":"not found"}}`, false},
		{"unavailable", 200, `{"ok":true,"data":{"enabled":false,"reason":"source_unavailable","generated_at_epoch_secs":1}}`, true},
		{"missing payload", 200, `{"ok":true,"data":{"enabled":true,"generated_at_epoch_secs":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/runtime/me-pool-state" {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				_, _ = w.Write([]byte(`{"ok":true,"data":{"middle_proxy_enabled":false,"dcs":[]}}`))
			}))
			defer srv.Close()
			h := &Handler{cfg: Config{StateDir: t.TempDir()}, exec: &fakeExec{}, tm: telemt.New(srv.URL, "")}
			var rep agentv1.HealthReport
			h.reliabilityHealth(context.Background(), &rep, nil)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rep.ReliabilityJson, &fields); err != nil {
				t.Fatal(err)
			}
			_, present := fields["me_pool"]
			if present != tc.present {
				t.Fatalf("ME source present=%v expected %v: %s", present, tc.present, rep.ReliabilityJson)
			}
			var report reliability.Report
			_ = json.Unmarshal(rep.ReliabilityJson, &report)
			for _, f := range reliability.Findings(report, time.Now()) {
				if len(f.Kind) > 3 && f.Kind[:3] == "me_" {
					t.Fatalf("unavailable source used: %+v", f)
				}
			}
		})
	}
}

func TestReliabilityHealthAcceptsSnapshotGeneratedDuringRequest(t *testing.T) {
	// The API can cross an epoch-second boundary after heartbeat collection starts.
	generated := time.Now().UTC().Truncate(time.Second).Add(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runtime/me-pool-state" {
			time.Sleep(time.Until(generated))
			_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"enabled":true,"generated_at_epoch_secs":%d,"data":%s}}`, generated.Unix(), mePayloadFixture)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"middle_proxy_enabled":false,"dcs":[]}}`))
	}))
	defer srv.Close()
	h := &Handler{cfg: Config{StateDir: t.TempDir()}, exec: &fakeExec{}, tm: telemt.New(srv.URL, "")}
	var rep agentv1.HealthReport
	h.reliabilityHealth(context.Background(), &rep, nil)
	var report reliability.Report
	if err := json.Unmarshal(rep.ReliabilityJson, &report); err != nil {
		t.Fatal(err)
	}
	if report.MEPool == nil || report.MEPool.State == nil || report.MEPool.State.OrphanObservedSeconds == nil {
		t.Fatalf("fresh response discarded as future: %s", rep.ReliabilityJson)
	}
}
