package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveryDoesNotRestartForReadinessPolicyOrNetwork(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reason      string
		status      int
		maintenance bool
		stopped     bool
		wantRestart bool
	}{
		{name: "Telegram unavailable", reason: "no_healthy_upstreams"},
		{name: "operator admission closed", reason: "admission_closed"},
		{name: "engine starting", reason: "starting"},
		{name: "unknown readiness reason", reason: "future_reason"},
		{name: "missing readiness reason"},
		{name: "wrong API token", status: http.StatusUnauthorized},
		{name: "API rate limited", status: http.StatusTooManyRequests},
		{name: "broken control API", status: http.StatusInternalServerError, wantRestart: true},
		{name: "maintenance", status: http.StatusInternalServerError, maintenance: true},
		{name: "stopped service", status: http.StatusInternalServerError, stopped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var restarted atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/runtime/web" {
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"ok":false,"error":{"code":"not_found","message":"not supported"}}`)
					return
				}
				if restarted.Load() {
					_, _ = fmt.Fprint(w, `{"ok":true,"data":{"ready":true}}`)
					return
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, `{"ok":false,"error":{"code":"unavailable","message":"cannot serve request"}}`)
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"ready":false,"reason":%q}}`, tc.reason)
			}))
			defer srv.Close()
			ex := &fakeExec{active: map[string]bool{"telemt": !tc.stopped}}
			ex.onCall = func(cmd string) {
				if cmd == "systemctl restart telemt" {
					restarted.Store(true)
				}
			}
			h := NewHandler(Config{Engine: EngineTelemt, TelemtAPI: srv.URL, StateDir: t.TempDir(), HealthWait: time.Second}, ex, slog.New(slog.DiscardHandler))
			h.recoveryPolicy.Recovery = "restart"
			h.recoveryPolicy.Maintenance = tc.maintenance
			for range 4 {
				h.recoveryTick(context.Background())
			}
			if got := restarted.Load(); got != tc.wantRestart {
				t.Fatalf("restart=%v, want %v; commands=%s", got, tc.wantRestart, strings.Join(ex.list(), "; "))
			}
		})
	}
}
