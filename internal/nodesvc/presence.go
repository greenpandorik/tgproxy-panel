// Package nodesvc holds node-related services shared by API and workers.
package nodesvc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	agentv1 "tgwebproxy/proto/agent/v1"
)

type Presence struct {
	st  *store.Store
	log *slog.Logger
}

func NewPresence(st *store.Store, log *slog.Logger) *Presence { return &Presence{st: st, log: log} }

func (p *Presence) NodeByToken(ctx context.Context, token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, errors.New("missing token")
	}
	n, err := p.st.Q.GetNodeByAgentToken(ctx, ptr(crypto.HashToken(token)))
	if err != nil {
		return uuid.Nil, errors.New("unknown token")
	}
	return n.ID, nil
}

// telemtVersion turns the version string a telemt agent reports ("telemt 3.5.5".
func telemtVersion(v string) string {
	return strings.TrimSpace(strings.TrimPrefix(v, "telemt "))
}

func (p *Presence) OnHello(ctx context.Context, id uuid.UUID, h *agentv1.Hello) {
	if err := p.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{
		ID: id, TproxyVersion: h.GetTproxyVersion(), AgentVersion: h.GetAgentVersion(),
		TelemtVersion: telemtVersion(h.GetTproxyVersion()),
	}); err != nil {
		p.log.Error("set online", "err", err)
	}
}

func (p *Presence) OnHeartbeat(ctx context.Context, id uuid.UUID, h *agentv1.HealthReport) {
	status := db.NodeStatusOnline
	if !h.GetRelayActive() || !h.GetMtproxyActive() || !h.GetHealthz() || !h.GetReadyz() {
		status = db.NodeStatusDegraded
	}
	report := nodedriver.HealthFromProto(h)
	raw, _ := json.Marshal(report)
	if err := p.st.Q.SetNodeHeartbeat(ctx, db.SetNodeHeartbeatParams{
		ID: id, Status: status, LastHealth: raw,
		TproxyVersion: h.GetTproxyVersion(), TelemtVersion: telemtVersion(h.GetTproxyVersion()),
		TelemtBuild: report.TelemtBuild, TelemtCapabilities: capabilitiesJSON(report.Capabilities),
	}); err != nil {
		p.log.Error("heartbeat", "err", err)
	}
}

// capabilitiesJSON renders the capability set for the nodes.telemt_capabilities column. A
// capability the agent could not determine is stored as an explicit null, so the panel can tell
// "not available" from a node that answered and does not support it. A heartbeat that carried
// no capability set at all stores nothing and leaves whatever the panel already knew.
func capabilitiesJSON(caps nodedriver.TelemtCapabilities) []byte {
	if caps == nil {
		return nil
	}
	raw, err := json.Marshal(caps)
	if err != nil {
		return nil
	}
	return raw
}

func (p *Presence) OnDisconnect(ctx context.Context, id uuid.UUID) {
	_ = p.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: id, Status: db.NodeStatusOffline})
}

func ptr(s string) *string { return &s }
