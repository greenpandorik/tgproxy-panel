package nodesvc_test

import (
	"context"
	"log/slog"
	"testing"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func TestPresenceLifecycle(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	n, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "a", Hostname: "a.test"})
	tok := "node-token"
	_ = st.Q.RegisterNode(ctx, db.RegisterNodeParams{ID: n.ID, AgentTokenHash: ptr(crypto.HashToken(tok)), TproxyVersion: "v", AgentVersion: "a"})
	p := nodesvc.NewPresence(st, slog.New(slog.DiscardHandler))

	id, err := p.NodeByToken(ctx, tok)
	if err != nil || id != n.ID {
		t.Fatalf("auth: %v %v", id, err)
	}
	if _, err := p.NodeByToken(ctx, "wrong"); err == nil {
		t.Fatal("wrong token accepted")
	}
	p.OnHello(ctx, n.ID, &agentv1.Hello{AgentVersion: "0.1.0", TproxyVersion: "52a5feb"})
	got, _ := st.Q.GetNode(ctx, n.ID)
	if got.Status != db.NodeStatusOnline || got.AgentVersion != "0.1.0" {
		t.Fatalf("after hello: %+v", got)
	}
	p.OnHeartbeat(ctx, n.ID, &agentv1.HealthReport{RelayActive: true, MtproxyActive: false, Healthz: true})
	got, _ = st.Q.GetNode(ctx, n.ID)
	if got.Status != db.NodeStatusDegraded || got.LastHealth == nil {
		t.Fatalf("after degraded heartbeat: %+v", got)
	}
	p.OnDisconnect(ctx, n.ID)
	got, _ = st.Q.GetNode(ctx, n.ID)
	if got.Status != db.NodeStatusOffline {
		t.Fatalf("after disconnect: %s", got.Status)
	}
}

func ptr(s string) *string { return &s }

func TestPresenceRecordsTelemtVersionAndKeepsUnknownOnes(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	telemtNode, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "t", Hostname: "t.test",
		Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTelemt, Valid: true},
	})
	tproxyNode, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "p", Hostname: "p.test",
		Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTproxy, Valid: true},
	})
	p := nodesvc.NewPresence(st, slog.New(slog.DiscardHandler))

	// The telemt agent labels the shared version field; the panel stores the bare version.
	p.OnHello(ctx, telemtNode.ID, &agentv1.Hello{AgentVersion: "0.1.0", TproxyVersion: "telemt 3.5.5"})
	got, _ := st.Q.GetNode(ctx, telemtNode.ID)
	if got.TelemtVersion != "3.5.5" || got.TproxyVersion != "telemt 3.5.5" {
		t.Fatalf("after hello: telemt=%q tproxy=%q", got.TelemtVersion, got.TproxyVersion)
	}
	// A report with no version (telemt's control API was unreachable) must not blank it out.
	p.OnHeartbeat(ctx, telemtNode.ID, &agentv1.HealthReport{RelayActive: true, MtproxyActive: true, Healthz: true, Readyz: true})
	got, _ = st.Q.GetNode(ctx, telemtNode.ID)
	if got.TelemtVersion != "3.5.5" || got.TproxyVersion != "telemt 3.5.5" || got.AgentVersion != "0.1.0" {
		t.Fatalf("empty version overwrote a known one: %+v", got)
	}
	// A later heartbeat that does carry the version updates it.
	p.OnHeartbeat(ctx, telemtNode.ID, &agentv1.HealthReport{RelayActive: true, MtproxyActive: true, Healthz: true, Readyz: true, TproxyVersion: "telemt 3.5.6"})
	got, _ = st.Q.GetNode(ctx, telemtNode.ID)
	if got.TelemtVersion != "3.5.6" {
		t.Fatalf("telemt version not refreshed: %q", got.TelemtVersion)
	}

	// A tproxy node never gets a telemt version, whatever it reports.
	p.OnHello(ctx, tproxyNode.ID, &agentv1.Hello{AgentVersion: "0.1.0", TproxyVersion: "52a5feb"})
	got, _ = st.Q.GetNode(ctx, tproxyNode.ID)
	if got.TelemtVersion != "" || got.TproxyVersion != "52a5feb" {
		t.Fatalf("tproxy node: telemt=%q tproxy=%q", got.TelemtVersion, got.TproxyVersion)
	}
}
