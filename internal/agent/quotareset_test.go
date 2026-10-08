package agent

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

func TestTelemtQuotaResetSurvivesFailedApply(t *testing.T) {
	for _, failOn := range []string{"PATCH /v1/users/k1", "POST /v1/users/k2/reset-quota"} {
		t.Run(failOn, func(t *testing.T) {
			h, cfg, ft := telemtHandler(t, &fakeExec{})
			oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			k1 := limitedProfile("k1", secretK1, time.Time{})
			k2 := limitedProfile("k2", secretK1, time.Time{})
			k1.QuotaResetUnix, k2.QuotaResetUnix = oct.Unix(), oct.Unix()
			req := &agentv1.ApplyRequest{ApplyProfiles: true, Profiles: append(profiles("node", secretNode), k1, k2)}
			if res := h.Apply(context.Background(), req); !res.Ok {
				t.Fatalf("setup: %s", res.Log)
			}
			ft.resetWrites()
			k1.QuotaResetUnix, k2.QuotaResetUnix = oct.AddDate(0, 1, 0).Unix(), oct.AddDate(0, 1, 0).Unix()
			k1.DataQuotaBytes *= 2
			ft.failOn = failOn
			if res := h.Apply(context.Background(), req); res.Ok {
				t.Fatal("apply must fail when the telemt API rejects an operation")
			}
			_, writes, _, _ := ft.snapshot()
			if got := countQuotaResets(writes, "k1"); got != 1 {
				t.Fatalf("first apply reset k1 %d times, want 1: %v", got, writes)
			}

			// Start a fresh handler: completed resets must survive an agent restart,
			// while the rejected operation must still be retried.
			restarted := NewHandler(cfg, &fakeExec{}, slog.New(slog.DiscardHandler))
			restarted.decoyHTTP = h.decoyHTTP
			ft.failOn = ""
			ft.resetWrites()
			if res := restarted.Apply(context.Background(), req); !res.Ok {
				t.Fatalf("retry: %s", res.Log)
			}
			_, writes, _, _ = ft.snapshot()
			if got := countQuotaResets(writes, "k1"); got != 0 {
				t.Errorf("retry reset k1 %d more times in the same period: %v", got, writes)
			}
			if got := countQuotaResets(writes, "k2"); got != 1 {
				t.Errorf("retry must reset k2 once, got %d: %v", got, writes)
			}
			if failOn == "PATCH /v1/users/k1" && len(ft.bodies("k1")) == 0 {
				t.Error("the failed quota limit update was incorrectly treated as complete")
			}
		})
	}
}

func countQuotaResets(writes []string, user string) int {
	n := 0
	for _, w := range writes {
		if w == "POST /v1/users/"+user+"/reset-quota" {
			n++
		}
	}
	return n
}

func TestTelemtQuotaResetReportsCheckpointFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	h, cfg, ft := telemtHandler(t, &fakeExec{})
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	k1 := limitedProfile("k1", secretK1, time.Time{})
	k1.QuotaResetUnix = oct.Unix()
	req := &agentv1.ApplyRequest{ApplyProfiles: true, Profiles: append(profiles("node", secretNode), k1)}
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("setup: %s", res.Log)
	}
	if err := os.Chmod(cfg.StateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfg.StateDir, 0o700) })
	ft.resetWrites()
	k1.QuotaResetUnix = oct.AddDate(0, 1, 0).Unix()
	k1.DataQuotaBytes *= 2
	res := h.Apply(context.Background(), req)
	if res.Ok || !strings.Contains(res.Log, "record quota reset") {
		t.Fatalf("checkpoint failure must fail the apply explicitly: ok=%v log=%s", res.Ok, res.Log)
	}
	if res.RolledBack {
		t.Fatal("the quota reset cannot be undone")
	}
	if len(ft.bodies("k1")) != 0 {
		t.Fatal("user updates must stop when the reset cannot be recorded")
	}
}
