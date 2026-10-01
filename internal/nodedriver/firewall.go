package nodedriver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	agentv1 "tgwebproxy/proto/agent/v1"
)

var ErrUnsupportedFirewall = errors.New("the agent on this server cannot block addresses yet; update the agent first")

type FirewallCounter struct {
	Entry   string `json:"entry"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// FirewallStatus is the blocklist a node enforces and what it has dropped since the list last changed.
type FirewallStatus struct {
	Revision       int64             `json:"revision"`
	Entries        int               `json:"entries"`
	DroppedPackets uint64            `json:"dropped_packets"`
	DroppedBytes   uint64            `json:"dropped_bytes"`
	Counters       []FirewallCounter `json:"counters,omitempty"`
	Error          string            `json:"error,omitempty"`
}

type FirewallDriver interface {
	Firewall(ctx context.Context, id uuid.UUID, set bool, revision int64, entries []string) (FirewallStatus, error)
}

func FirewallFromProto(s *agentv1.FirewallStatus) *FirewallStatus {
	if s == nil {
		return nil
	}
	out := &FirewallStatus{
		Revision: s.GetRevision(), Entries: int(s.GetEntries()),
		DroppedPackets: s.GetDroppedPackets(), DroppedBytes: s.GetDroppedBytes(), Error: s.GetError(),
	}
	for _, c := range s.GetCounters() {
		out.Counters = append(out.Counters, FirewallCounter{Entry: c.GetEntry(), Packets: c.GetPackets(), Bytes: c.GetBytes()})
	}
	return out
}

func FirewallToProto(s *FirewallStatus) *agentv1.FirewallStatus {
	if s == nil {
		return nil
	}
	out := &agentv1.FirewallStatus{
		Revision: s.Revision, Entries: int32(s.Entries),
		DroppedPackets: s.DroppedPackets, DroppedBytes: s.DroppedBytes, Error: s.Error,
	}
	for _, c := range s.Counters {
		out.Counters = append(out.Counters, &agentv1.FirewallCounter{Entry: c.Entry, Packets: c.Packets, Bytes: c.Bytes})
	}
	return out
}

func (g *Gateway) Firewall(ctx context.Context, id uuid.UUID, set bool, revision int64, entries []string) (FirewallStatus, error) {
	req := &agentv1.FirewallRequest{Set: set, Revision: revision, Entries: entries}
	r, e := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Firewall{Firewall: req}}, 30*time.Second)
	if e != nil {
		if r.GetError() == "unsupported request" {
			return FirewallStatus{}, ErrUnsupportedFirewall
		}
		return FirewallStatus{}, e
	}
	if r.GetFirewall() == nil {
		return FirewallStatus{}, ErrUnsupportedFirewall
	}
	return *FirewallFromProto(r.GetFirewall()), nil
}

func (m *Mock) Firewall(_ context.Context, id uuid.UUID, set bool, revision int64, entries []string) (FirewallStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return FirewallStatus{}, err
	}
	if n.firewallErr != "" {
		return FirewallStatus{}, errors.New(n.firewallErr)
	}
	if set {
		n.firewall = FirewallStatus{Revision: revision, Entries: len(entries)}
		n.firewallEntries = append([]string(nil), entries...)
		n.health.Firewall = &FirewallStatus{Revision: revision, Entries: len(entries)}
	}
	out := n.firewall
	out.Counters = nil
	for _, e := range n.firewallEntries {
		out.Counters = append(out.Counters, FirewallCounter{Entry: e, Packets: n.firewallDrops[e]})
		out.DroppedPackets += n.firewallDrops[e]
	}
	return out, nil
}

// SetFirewallDrops makes the mock report this many dropped packets for an entry.
func (m *Mock) SetFirewallDrops(id uuid.UUID, entry string, packets uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.node(id)
	if n.firewallDrops == nil {
		n.firewallDrops = map[string]uint64{}
	}
	n.firewallDrops[entry] = packets
}

// FailFirewall makes every firewall call to the node fail with msg; empty clears it.
func (m *Mock) FailFirewall(id uuid.UUID, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).firewallErr = msg
}

// FirewallEntries is the list the mock node last received.
func (m *Mock) FirewallEntries(id uuid.UUID) (int64, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.node(id)
	return n.firewall.Revision, append([]string(nil), n.firewallEntries...)
}
