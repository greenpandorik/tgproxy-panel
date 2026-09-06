package nodedriver

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

type mockNode struct {
	online   bool
	health   HealthReport
	profiles []Profile
	site     SiteBundle
	metrics  string
	stats    map[string]string
	applied  []ApplyRequest
	failMsg  string
	restarts int
	// tlsDomain/classicPort stand in for the node's Fake-TLS listener: an apply that
	// carries different values rewrites them and reports a relay restart, the same way
	// the telemt agent does.
	tlsDomain   string
	classicPort uint32
	publicIP    string
}

// Mock is an in-memory Driver for tests and NODE_DRIVER=mock.
type Mock struct {
	mu    sync.Mutex
	nodes map[uuid.UUID]*mockNode
}

func NewMock() *Mock { return &Mock{nodes: map[uuid.UUID]*mockNode{}} }

func (m *Mock) node(id uuid.UUID) *mockNode {
	n := m.nodes[id]
	if n == nil {
		n = &mockNode{health: HealthReport{RelayActive: true, MTProxyActive: true, CaddyActive: true, Healthz: true, Readyz: true, TProxyVersion: "mock"}, stats: map[string]string{}}
		m.nodes[id] = n
	}
	return n
}

func (m *Mock) SetOnline(id uuid.UUID, online bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).online = online
}

func (m *Mock) SetHealth(id uuid.UUID, h HealthReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).health = h
}

func (m *Mock) SetMetrics(id uuid.UUID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).metrics = text
}

func (m *Mock) SetStats(id uuid.UUID, s map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).stats = s
}

func (m *Mock) FailNextApply(id uuid.UUID, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).failMsg = msg
}

func (m *Mock) Applied(id uuid.UUID) []ApplyRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ApplyRequest(nil), m.node(id).applied...)
}

// Listeners reports the Fake-TLS listener the last apply left on the node.
func (m *Mock) Listeners(id uuid.UUID) (string, uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.node(id)
	return n.tlsDomain, n.classicPort
}

func (m *Mock) Restarts(id uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.node(id).restarts
}

func (m *Mock) get(id uuid.UUID) (*mockNode, error) {
	n := m.nodes[id]
	if n == nil || !n.online {
		return nil, ErrOffline
	}
	return n, nil
}

func (m *Mock) Online(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[id]
	return n != nil && n.online
}

func (m *Mock) Health(_ context.Context, id uuid.UUID) (HealthReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return HealthReport{}, err
	}
	h := n.health
	h.ProfileCount = len(n.profiles)
	return h, nil
}

func (m *Mock) GetProfiles(_ context.Context, id uuid.UUID) ([]Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return append([]Profile(nil), n.profiles...), nil
}

func (m *Mock) Apply(_ context.Context, id uuid.UUID, req ApplyRequest) (ApplyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return ApplyResult{}, err
	}
	n.applied = append(n.applied, req)
	if n.failMsg != "" {
		msg := n.failMsg
		n.failMsg = ""
		return ApplyResult{OK: false, RolledBack: true, Log: msg}, errors.New(msg)
	}
	res := ApplyResult{OK: true, Log: "mock apply ok"}
	if req.ApplyProfiles {
		n.profiles = append([]Profile(nil), req.Profiles...)
		res.RestartedRelay, res.RestartedMTProxy = true, true
	}
	if req.Site != nil {
		n.site = *req.Site
		res.RestartedRelay = true
	}
	// An empty domain / zero port mean "the panel has no opinion", so they never overwrite
	// what the node already holds - that is how a tproxy node's apply looks.
	if (req.TLSDomain != "" && req.TLSDomain != n.tlsDomain) || (req.ClassicPort != 0 && req.ClassicPort != n.classicPort) {
		if req.TLSDomain != "" {
			n.tlsDomain = req.TLSDomain
		}
		if req.ClassicPort != 0 {
			n.classicPort = req.ClassicPort
		}
		res.RestartedRelay = true
	}
	if req.PublicIP != "" && req.PublicIP != n.publicIP {
		n.publicIP = req.PublicIP
		res.RestartedRelay = true
	}
	return res, nil
}

func (m *Mock) GetSite(_ context.Context, id uuid.UUID) (SiteBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return SiteBundle{}, err
	}
	return n.site, nil
}

func (m *Mock) Metrics(_ context.Context, id uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return "", err
	}
	if n.metrics == "" {
		return "tproxy_sessions_live 0\ntproxy_streams_live 0\ntproxy_bytes_up_total 0\ntproxy_bytes_down_total 0\ntproxy_sessions_created_total 0\ntproxy_limit_hits_total 0\n", nil
	}
	return n.metrics, nil
}

func (m *Mock) Stats(_ context.Context, id uuid.UUID) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return n.stats, nil
}

func (m *Mock) TailLogs(ctx context.Context, id uuid.UUID, services []string, lines int, follow bool) (<-chan LogLine, error) {
	if !m.Online(id) {
		return nil, ErrOffline
	}
	out := make(chan LogLine, 8)
	go func() {
		defer close(out)
		for _, svc := range services {
			select {
			case out <- LogLine{Service: svc, Line: "mock log line", Time: time.Now()}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (m *Mock) RestartRelay(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return err
	}
	n.restarts++
	return nil
}
