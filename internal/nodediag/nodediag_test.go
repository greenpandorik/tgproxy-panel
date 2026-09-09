package nodediag

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/nodedriver"
)

const webMetricsText = `# TYPE telemt_web_carrier_selections_total counter
telemt_web_carrier_selections_total{carrier="https",disposition="learned"} 12
# TYPE telemt_web_carrier_reported_failures_total counter
telemt_web_carrier_reported_failures_total{carrier="https",phase="dial",reason="timeout"} 0
# TYPE telemt_web_rejections_total counter
telemt_web_rejections_total{reason="quota"} 0
`

type fakeNode struct {
	online     bool
	health     nodedriver.HealthReport
	healthErr  error
	metrics    string
	metricsErr error
}

func (f fakeNode) Online(uuid.UUID) bool { return f.online }

func (f fakeNode) Health(context.Context, uuid.UUID) (nodedriver.HealthReport, error) {
	return f.health, f.healthErr
}

func (f fakeNode) Metrics(context.Context, uuid.UUID) (string, error) {
	return f.metrics, f.metricsErr
}

type fakeResolver struct {
	addrs []net.IPAddr
	err   error
}

func (f fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return f.addrs, f.err
}

func healthyHealth() nodedriver.HealthReport {
	return nodedriver.HealthReport{
		RelayActive: true, MTProxyActive: true, CaddyActive: true, Healthz: true, Readyz: true,
		TProxyVersion: "telemt 3.5.7", UpstreamHealthy: true, EffectiveLatencyMs: 42,
		ConnectSuccessTotal: 100, DcDataAvailable: true,
		DCs: []nodedriver.DcLatency{{DC: 1, LatencyMs: 40, Known: true}, {DC: 2}},
	}
}

// frontServer is a TLS front that negotiates HTTP/2 and serves a decoy page, reachable on 443
// and on the Fake-TLS port.
func frontServer(t *testing.T) (*httptest.Server, *nodecheck.Checker) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a decoy page"))
	}))
	srv.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	dial := func(_ context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected dial host %q", host)
		}
		switch port {
		case "443", "8443":
			return net.Dial(network, srv.Listener.Addr().String())
		default:
			return nil, fmt.Errorf("unexpected dial port %q", port)
		}
	}
	checker := &nodecheck.Checker{
		Resolver:  fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
		Dial:      dial,
		TLSConfig: &tls.Config{RootCAs: pool},
		Timeout:   3 * time.Second,
	}
	return srv, checker
}

func telemtTarget() Target {
	return Target{
		NodeID: uuid.New(), Hostname: "example.com", PublicIP: "5.6.7.8",
		TLSDomain: "example.com", ClassicPort: 8443, Telemt: true,
	}
}

func checkIn(t *testing.T, run domain.DiagnosticsRun, group, key string) domain.DiagnosticCheck {
	t.Helper()
	for _, g := range run.Groups {
		if g.Key != group {
			continue
		}
		for _, c := range g.Checks {
			if c.Key == key {
				return c
			}
		}
		t.Fatalf("group %q has no check %q: %+v", group, key, g.Checks)
	}
	t.Fatalf("run has no group %q", group)
	return domain.DiagnosticCheck{}
}

func detailOf(c domain.DiagnosticCheck) string {
	if c.Detail == nil {
		return ""
	}
	return *c.Detail
}

func countByStatus(run domain.DiagnosticsRun) map[domain.CheckStatus]int {
	out := map[domain.CheckStatus]int{}
	for _, g := range run.Groups {
		for _, c := range g.Checks {
			out[c.Status]++
		}
	}
	return out
}

func TestRunHealthyNode(t *testing.T) {
	_, checker := frontServer(t)
	e := &Engine{
		Checker: checker,
		Node:    fakeNode{online: true, health: healthyHealth(), metrics: webMetricsText},
		Timeout: 10 * time.Second,
	}
	run := e.Run(t.Context(), telemtTarget(), domain.TriggerManual)

	if run.Status != domain.DiagnosticsHealthy {
		t.Fatalf("status = %q, want healthy: %+v", run.Status, run.Groups)
	}
	if run.Trigger != domain.TriggerManual || run.FinishedAt == nil {
		t.Fatalf("run = %+v", run)
	}
	counts := countByStatus(run)
	if counts[domain.CheckFail] != 0 || counts[domain.CheckWarn] != 0 {
		for _, g := range run.Groups {
			for _, c := range g.Checks {
				if c.Status == domain.CheckFail || c.Status == domain.CheckWarn {
					t.Errorf("%s/%s = %s (%s)", g.Key, c.Key, c.Status, detailOf(c))
				}
			}
		}
		t.Fatalf("a healthy node must not fail or warn")
	}
	wantGroups := []string{
		domain.GroupDNS, domain.GroupTransport, domain.GroupReverseProxy,
		domain.GroupWebTransport, domain.GroupTelemt, domain.GroupTelegram,
	}
	for i, key := range wantGroups {
		if run.Groups[i].Key != key {
			t.Fatalf("group[%d] = %q, want %q", i, run.Groups[i].Key, key)
		}
	}
	for _, want := range []struct{ group, key string }{
		{domain.GroupDNS, "a_record"},
		{domain.GroupDNS, "public_ip_match"},
		{domain.GroupTransport, "tcp_443"},
		{domain.GroupTransport, "tls_handshake"},
		{domain.GroupTransport, "certificate"},
		{domain.GroupTransport, "certificate_expiry"},
		{domain.GroupTransport, "fake_tls_listener"},
		{domain.GroupReverseProxy, "https_response"},
		{domain.GroupReverseProxy, "decoy_site"},
		{domain.GroupReverseProxy, "caddy_service"},
		{domain.GroupWebTransport, "http2"},
		{domain.GroupWebTransport, "web_upstream"},
		{domain.GroupTelemt, "control_api"},
		{domain.GroupTelemt, "readiness"},
		{domain.GroupTelegram, "upstream_health"},
		{domain.GroupTelegram, "datacenters"},
	} {
		if c := checkIn(t, run, want.group, want.key); c.Status != domain.CheckOK {
			t.Errorf("%s/%s = %s (%s), want ok", want.group, want.key, c.Status, detailOf(c))
		}
	}
	if h2 := checkIn(t, run, domain.GroupWebTransport, "http2"); h2.Value == nil || *h2.Value != "h2" {
		t.Fatalf("http2 = %+v, want the negotiated h2", h2)
	}
}

// A node the panel cannot reach reports what it could not check, not a wall of failures.
func TestRunUnreachableNodeIsNotAllFailures(t *testing.T) {
	e := &Engine{
		Checker: &nodecheck.Checker{
			Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
			Dial: func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("connection refused")
			},
			Timeout: time.Second,
		},
		Node:    fakeNode{online: false},
		Timeout: 10 * time.Second,
	}
	run := e.Run(t.Context(), telemtTarget(), domain.TriggerManual)

	if run.Status != domain.DiagnosticsOffline {
		t.Fatalf("status = %q, want offline: %+v", run.Status, run.Groups)
	}
	counts := countByStatus(run)
	if counts[domain.CheckNotAvailable] < counts[domain.CheckFail] {
		t.Fatalf("an unreachable node should mostly be not_available, got %v", counts)
	}
	// The two reachability checks are the only failures; everything downstream is unknown.
	if got := counts[domain.CheckFail]; got != 2 {
		t.Fatalf("failures = %d, want the two reachability checks: %v", got, counts)
	}
	if c := checkIn(t, run, domain.GroupTransport, "tcp_443"); c.Status != domain.CheckFail {
		t.Fatalf("tcp_443 = %+v, want fail", c)
	}
	if c := checkIn(t, run, domain.GroupTelemt, "agent_link"); c.Status != domain.CheckFail {
		t.Fatalf("agent_link = %+v, want fail", c)
	}
	for _, key := range []string{"tls_handshake", "certificate", "certificate_expiry", "fake_tls_listener"} {
		if c := checkIn(t, run, domain.GroupTransport, key); c.Status != domain.CheckNotAvailable {
			t.Errorf("%s = %s, want not_available", key, c.Status)
		}
	}
}

// telemt not answering is not the same as telemt being broken.
func TestTelemtUnreachableIsNotAvailable(t *testing.T) {
	_, checker := frontServer(t)
	e := &Engine{
		Checker: checker,
		Node:    fakeNode{online: true, healthErr: errors.New("stream closed"), metricsErr: errors.New("stream closed")},
		Timeout: 10 * time.Second,
	}
	run := e.Run(t.Context(), telemtTarget(), domain.TriggerManual)

	for _, key := range []string{"service", "control_api", "readiness", "build"} {
		c := checkIn(t, run, domain.GroupTelemt, key)
		if c.Status != domain.CheckNotAvailable {
			t.Errorf("telemt/%s = %s, want not_available", key, c.Status)
		}
		if !strings.Contains(detailOf(c), "stream closed") {
			t.Errorf("telemt/%s detail = %q, want the reason it could not be read", key, detailOf(c))
		}
	}
	for _, key := range []string{"upstream_health", "upstream_latency", "datacenters", "connect_attempts"} {
		if c := checkIn(t, run, domain.GroupTelegram, key); c.Status != domain.CheckNotAvailable {
			t.Errorf("telegram/%s = %s, want not_available", key, c.Status)
		}
	}
	for _, key := range []string{"carrier_selections", "carrier_failures", "web_rejections", "web_upstream"} {
		if c := checkIn(t, run, domain.GroupWebTransport, key); c.Status != domain.CheckNotAvailable {
			t.Errorf("web_transport/%s = %s, want not_available", key, c.Status)
		}
	}
	// The public side still answered, so the run is not offline and the front's checks stand.
	if run.Status == domain.DiagnosticsOffline {
		t.Fatalf("a reachable front must not be reported offline: %+v", run)
	}
	if c := checkIn(t, run, domain.GroupTransport, "tcp_443"); c.Status != domain.CheckOK {
		t.Fatalf("tcp_443 = %+v, want ok", c)
	}
}

// The panel counts only the checks it ran: "n / n passed" never hides a skipped one.
func TestTallyExcludesNotAvailable(t *testing.T) {
	_, checker := frontServer(t)
	e := &Engine{
		Checker: checker,
		Node:    fakeNode{online: false},
		Timeout: 10 * time.Second,
	}
	run := e.Run(t.Context(), telemtTarget(), domain.TriggerManual)

	counts := countByStatus(run)
	if counts[domain.CheckNotAvailable] == 0 {
		t.Fatal("this run must contain not_available checks to be worth counting")
	}
	if run.NotRun != counts[domain.CheckNotAvailable] {
		t.Fatalf("not_run = %d, want %d", run.NotRun, counts[domain.CheckNotAvailable])
	}
	ran := counts[domain.CheckOK] + counts[domain.CheckWarn] + counts[domain.CheckFail]
	if run.Total != ran {
		t.Fatalf("total = %d, want %d (not_available excluded)", run.Total, ran)
	}
	if run.Passed != counts[domain.CheckOK] {
		t.Fatalf("passed = %d, want %d", run.Passed, counts[domain.CheckOK])
	}
}

// A metric family that never appeared is absent, not zero.
func TestAbsentMetricFamiliesAreNotAvailable(t *testing.T) {
	_, checker := frontServer(t)
	e := &Engine{
		Checker: checker,
		Node:    fakeNode{online: true, health: healthyHealth(), metrics: "telemt_uptime_seconds 12\n"},
		Timeout: 10 * time.Second,
	}
	run := e.Run(t.Context(), telemtTarget(), domain.TriggerManual)

	for _, key := range []string{"carrier_selections", "carrier_failures", "web_rejections"} {
		if c := checkIn(t, run, domain.GroupWebTransport, key); c.Status != domain.CheckNotAvailable {
			t.Errorf("web_transport/%s = %s (%s), want not_available", key, c.Status, detailOf(c))
		}
	}
	if c := checkIn(t, run, domain.GroupWebTransport, "web_upstream"); c.Status != domain.CheckOK {
		t.Fatalf("web_upstream = %+v, want ok", c)
	}
}

// A hostname pointing at another machine is a fault the panel can prove.
func TestPublicIPMismatchFails(t *testing.T) {
	_, checker := frontServer(t)
	target := telemtTarget()
	target.PublicIP = "9.9.9.9"
	e := &Engine{Checker: checker, Node: fakeNode{online: true, health: healthyHealth()}, Timeout: 10 * time.Second}
	run := e.Run(t.Context(), target, domain.TriggerManual)

	c := checkIn(t, run, domain.GroupDNS, "public_ip_match")
	if c.Status != domain.CheckFail || !strings.Contains(detailOf(c), "9.9.9.9") {
		t.Fatalf("public_ip_match = %+v (%s), want a failure naming the node's IP", c, detailOf(c))
	}
	if run.Status != domain.DiagnosticsDegraded {
		t.Fatalf("status = %q, want degraded", run.Status)
	}
}

func TestRunStaysInsideItsBudget(t *testing.T) {
	e := &Engine{
		Checker: &nodecheck.Checker{
			Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			Timeout: 30 * time.Second,
		},
		Node:    fakeNode{online: true, health: healthyHealth(), metrics: webMetricsText},
		Timeout: 700 * time.Millisecond,
	}
	start := time.Now()
	run := e.Run(context.Background(), telemtTarget(), domain.TriggerManual)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the pass took %v; it must divide its own 700ms budget", elapsed)
	}
	if len(run.Groups) != 6 {
		t.Fatalf("a timed-out pass still reports every group, got %d", len(run.Groups))
	}
}
