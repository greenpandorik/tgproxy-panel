package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func lifecycleFixture(state, drainState string, sessions uint64) map[string]any {
	return map[string]any{"runtime": map[string]any{"runtime_instance": "0123456789abcdef0123456789abcdef"}, "operator_lifecycle": map[string]any{"state": state, "admission_open": false, "drain": map[string]any{"state": drainState, "remaining_sessions": sessions, "remaining_streams": 0, "remaining_websockets": 0}}}
}

func TestUpdateDrainRejectsCancellationAndLiveSessions(t *testing.T) {
	for _, tc := range []struct {
		state, drain string
		sessions     uint64
	}{{"running", "cancelled", 0}, {"drained", "completed", 3}} {
		t.Run(tc.drain, func(t *testing.T) {
			h, _, _ := telemtHandler(t, &fakeExec{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := any(lifecycleFixture(tc.state, tc.drain, tc.sessions))
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusAccepted)
					data = map[string]any{"operation_id": "test", "state": "draining"}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
			}))
			defer srv.Close()
			h.tm = telemt.New(srv.URL, "token")
			u := newTelemtUpdate("3.5.6", "3.5.7")
			if err := h.updateDrain(context.Background(), u, 1); err == nil {
				t.Fatal("unverified drain accepted")
			}
		})
	}
}

func TestUpdateResumeNeverInventsOpenAdmission(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.webStatus = map[string]any{"runtime": map[string]any{"runtime_instance": "0123456789abcdef0123456789abcdef"}}
	if err := h.updateResume(context.Background(), newTelemtUpdate("old", "new"), updatePlan{}, true); err == nil {
		t.Fatal("missing lifecycle reported as resumed")
	}
}

func TestMaintenanceExcludesApply(t *testing.T) {
	h, _, _ := telemtHandler(t, &fakeExec{})
	h.maintenance.Lock()
	defer h.maintenance.Unlock()
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{})
	if res.Ok {
		t.Fatal("concurrent apply accepted")
	}
}

func TestSameTelemtVersionIgnoresReleasePrefix(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"3.5.7", "3.5.7", true},
		{"v3.5.7", "3.5.7", true},
		{"3.5.6", "3.5.7", false},
		{"", "3.5.7", false},
	} {
		if got := sameTelemtVersion(tc.a, tc.b); got != tc.want {
			t.Fatalf("sameTelemtVersion(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDecoyFailureRollsBackWebsite(t *testing.T) {
	h, cfg, _ := telemtHandler(t, &fakeExec{})
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer bad.Close()
	h.decoyHTTP = &http.Client{Transport: decoyRoundTripper{target: bad.URL}}
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "index.html", Content: []byte("new website")}}}})
	if res.Ok || !res.RolledBack {
		t.Fatalf("expected rollback: %+v", res)
	}
	old, err := os.ReadFile(filepath.Join(cfg.TelemtSiteDir, "index.html"))
	if err != nil || string(old) != "old" {
		t.Fatalf("old website lost: %s %v", old, err)
	}
}
