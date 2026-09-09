package nodesvc_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func TestPresenceStoresCapabilitiesAndKeepsThemOverAnOldHeartbeat(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	n, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "t", Hostname: "t.test",
		Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTelemt, Valid: true},
	})
	p := nodesvc.NewPresence(st, slog.New(slog.DiscardHandler))

	p.OnHeartbeat(ctx, n.ID, &agentv1.HealthReport{
		RelayActive: true, MtproxyActive: true, Healthz: true, Readyz: true,
		Capabilities: &agentv1.TelemtCapabilities{
			Supported:    map[string]bool{"Web": true, "WebDrain": false},
			Undetermined: []string{"CarrierLearning"},
			Build:        "x86_64-unknown-linux-gnu",
		},
	})
	got, _ := st.Q.GetNode(ctx, n.ID)
	if got.TelemtBuild != "x86_64-unknown-linux-gnu" {
		t.Fatalf("build = %q", got.TelemtBuild)
	}
	if got.TelemtCapabilitiesCheckedAt == nil {
		t.Fatal("storing a capability set must date it")
	}
	var caps map[string]*bool
	if err := json.Unmarshal(got.TelemtCapabilities, &caps); err != nil {
		t.Fatalf("capabilities %s: %v", got.TelemtCapabilities, err)
	}
	if v, ok := caps["CarrierLearning"]; !ok || v != nil {
		t.Fatalf("an undetermined capability must be stored as null: %s", got.TelemtCapabilities)
	}
	if caps["WebDrain"] == nil || *caps["WebDrain"] {
		t.Fatalf("a determined false must stay false: %s", got.TelemtCapabilities)
	}

	// An older agent sends no capability section at all; what the panel knows must survive.
	p.OnHeartbeat(ctx, n.ID, &agentv1.HealthReport{RelayActive: true, MtproxyActive: true, Healthz: true, Readyz: true})
	after, _ := st.Q.GetNode(ctx, n.ID)
	if string(after.TelemtCapabilities) != string(got.TelemtCapabilities) || after.TelemtBuild != got.TelemtBuild {
		t.Fatalf("a heartbeat without capabilities must not erase them: %s / %q", after.TelemtCapabilities, after.TelemtBuild)
	}
	if after.Status != db.NodeStatusOnline {
		t.Fatalf("the rest of the old-style heartbeat must still apply: %s", after.Status)
	}
	var health nodedriver.HealthReport
	if err := json.Unmarshal(after.LastHealth, &health); err != nil {
		t.Fatal(err)
	}
	if health.Web != nil || health.Capabilities != nil {
		t.Fatalf("an old heartbeat carries no WEB section: %+v", health)
	}
}

func TestPresenceNeverStoresCapabilitiesForANodeThatNeverReported(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	n, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "p", Hostname: "p.test"})
	p := nodesvc.NewPresence(st, slog.New(slog.DiscardHandler))
	p.OnHeartbeat(ctx, n.ID, &agentv1.HealthReport{RelayActive: true, MtproxyActive: true, Healthz: true, Readyz: true})
	got, _ := st.Q.GetNode(ctx, n.ID)
	if got.TelemtCapabilities != nil || got.TelemtCapabilitiesCheckedAt != nil {
		t.Fatalf("capabilities the panel never worked out stay NULL, not an empty set: %s", got.TelemtCapabilities)
	}
}
