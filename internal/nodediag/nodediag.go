// Package nodediag runs one end-to-end diagnostic pass over a node, from the hostname's DNS
// record through to Telegram reachability, and reports what it could not determine as such.
package nodediag

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/telemt"
)

// DefaultTimeout bounds a whole pass when Engine.Timeout is unset.
const DefaultTimeout = 20 * time.Second

// certWarnDays is how close to expiry a certificate has to be to degrade the run.
const certWarnDays = 14

// latencyWarnMs is the upstream latency above which Telegram reachability is only a warning.
const latencyWarnMs = 1000

// Target is the node a pass runs against.
type Target struct {
	NodeID      uuid.UUID
	Hostname    string
	PublicIP    string
	TLSDomain   string
	ClassicPort int
	// Telemt is false for a tproxy node, which has no WEB runtime and no Fake-TLS listener.
	Telemt bool
}

// NodeSource is what the panel can read from the node itself. nodedriver.Driver satisfies it.
type NodeSource interface {
	Online(nodeID uuid.UUID) bool
	Health(ctx context.Context, nodeID uuid.UUID) (nodedriver.HealthReport, error)
	Metrics(ctx context.Context, nodeID uuid.UUID) (string, error)
}

// Engine assembles a diagnostics run out of public probes and node-side readings.
type Engine struct {
	Checker *nodecheck.Checker
	Node    NodeSource
	Timeout time.Duration
	Now     func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

func (e *Engine) checker() *nodecheck.Checker {
	if e.Checker != nil {
		return e.Checker
	}
	return &nodecheck.Checker{}
}

func (e *Engine) timeout() time.Duration {
	if e.Timeout > 0 {
		return e.Timeout
	}
	return DefaultTimeout
}

// Run performs one pass. It never returns an error: an unreachable source is a check the panel
// could not perform, which the run reports rather than raises.
func (e *Engine) Run(ctx context.Context, t Target, trigger domain.DiagnosticsTrigger) domain.DiagnosticsRun {
	started := e.now()
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()

	var public publicFacts
	var node nodeFacts
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		public = e.probePublic(ctx, t)
	}()
	go func() {
		defer wg.Done()
		node = e.readNode(ctx, t)
	}()
	wg.Wait()

	finished := e.now()
	run := domain.DiagnosticsRun{
		NodeID:     t.NodeID.String(),
		StartedAt:  started,
		FinishedAt: &finished,
		Trigger:    trigger,
		Groups: []domain.DiagnosticGroup{
			dnsGroup(t, public),
			transportGroup(t, public),
			reverseProxyGroup(public, node),
			webTransportGroup(t, public, node),
			telemtGroup(t, node),
			telegramGroup(node),
		},
	}
	run.Tally()
	if run.Total > 0 && !public.reachable() && !node.reachable() {
		run.Status = domain.DiagnosticsOffline
	}
	return run
}

// publicFacts is what the panel learned by probing the node's public address.
type publicFacts struct {
	attempted bool
	addrs     []net.IPAddr
	dnsErr    error
	tcpErr    error
	tcpDone   bool
	tls       nodecheck.TLSProbe
	tlsErr    error
	tlsDone   bool
	http      nodecheck.HTTPProbe
	httpErr   error
	mask      nodecheck.TLSProbe
	maskErr   error
	maskDone  bool
}

func (p publicFacts) resolved() bool { return p.dnsErr == nil && len(p.addrs) > 0 }
func (p publicFacts) tcpOK() bool    { return p.tcpDone && p.tcpErr == nil }
func (p publicFacts) tlsOK() bool    { return p.tlsDone && p.tlsErr == nil }
func (p publicFacts) reachable() bool {
	return p.tcpOK()
}

// alpnOffer is what the panel asks the front to speak; what it picks is the HTTP/2 answer.
var alpnOffer = []string{"h2", "http/1.1"}

func (e *Engine) probePublic(ctx context.Context, t Target) publicFacts {
	var f publicFacts
	if t.Hostname == "" {
		return f
	}
	f.attempted = true
	c := e.checker()
	wantMask := t.Telemt && t.TLSDomain != "" && t.ClassicPort > 0
	remaining := 4
	if wantMask {
		remaining = 5
	}
	next := func() time.Duration {
		d := c.ProbeBudget(ctx, remaining)
		remaining--
		return d
	}

	f.addrs, f.dnsErr = c.LookupIPs(ctx, next(), t.Hostname)
	if !f.resolved() {
		return f
	}

	f.tcpDone, f.tcpErr = true, c.DialTCP(ctx, next(), t.Hostname, "443")
	if f.tcpErr != nil {
		return f
	}

	f.tlsDone = true
	f.tls, f.tlsErr = c.ProbeTLS(ctx, next(), t.Hostname, "443", t.Hostname, alpnOffer)
	if f.tlsErr == nil {
		f.http, f.httpErr = c.ProbeHTTP(ctx, next(), t.Hostname)
	}
	if wantMask {
		f.maskDone = true
		f.mask, f.maskErr = c.ProbeTLS(ctx, next(), t.Hostname, strconv.Itoa(t.ClassicPort), t.TLSDomain, nil)
	}
	return f
}

// nodeFacts is what the panel read from the node over the agent link.
type nodeFacts struct {
	configured bool
	online     bool
	health     nodedriver.HealthReport
	healthErr  error
	metrics    telemt.WebMetrics
	metricsErr error
}

func (n nodeFacts) reachable() bool { return n.online && n.healthErr == nil }

// linkDetail explains, in one line, why nothing could be read from the node.
func (n nodeFacts) linkDetail() string {
	switch {
	case !n.configured:
		return "the panel has no driver for this node"
	case !n.online:
		return "the node's agent link is down"
	case n.healthErr != nil:
		return "the node did not answer: " + n.healthErr.Error()
	}
	return ""
}

func (e *Engine) readNode(ctx context.Context, t Target) nodeFacts {
	var f nodeFacts
	if e.Node == nil {
		return f
	}
	f.configured = true
	f.online = e.Node.Online(t.NodeID)
	if !f.online {
		f.healthErr = nodedriver.ErrOffline
		f.metricsErr = nodedriver.ErrOffline
		return f
	}
	f.health, f.healthErr = e.Node.Health(ctx, t.NodeID)
	text, err := e.Node.Metrics(ctx, t.NodeID)
	if err != nil {
		f.metricsErr = err
		return f
	}
	f.metrics = telemt.ParseWebMetrics(text)
	return f
}

func dnsGroup(t Target, p publicFacts) domain.DiagnosticGroup {
	g := newGroup(domain.GroupDNS)
	switch {
	case t.Hostname == "":
		g.add(na("a_record", "the node has no hostname"))
	case p.dnsErr != nil && !nameNotFound(p.dnsErr):
		g.add(na("a_record", "the lookup did not answer: "+p.dnsErr.Error()))
	case p.dnsErr != nil:
		g.add(fail("a_record", "", t.Hostname+" does not resolve: "+p.dnsErr.Error()))
	case len(p.addrs) == 0:
		g.add(fail("a_record", "", t.Hostname+" has no A/AAAA record"))
	default:
		g.add(ok("a_record", joinIPs(p.addrs), t.Hostname+" resolves"))
	}

	switch {
	case !p.resolved():
		g.add(na("public_ip_match", "the hostname did not resolve"))
	case t.PublicIP == "":
		g.add(na("public_ip_match", "the panel does not know this node's public IP"))
	case containsIP(p.addrs, t.PublicIP):
		g.add(ok("public_ip_match", t.PublicIP, "the hostname points at this node"))
	default:
		g.add(fail("public_ip_match", joinIPs(p.addrs), "the hostname points elsewhere; this node is "+t.PublicIP))
	}
	return g.group()
}

func transportGroup(t Target, p publicFacts) domain.DiagnosticGroup {
	g := newGroup(domain.GroupTransport)
	switch {
	case !p.attempted:
		g.add(na("tcp_443", "the node has no hostname"))
	case !p.resolved():
		g.add(na("tcp_443", "the hostname did not resolve"))
	case p.tcpErr != nil:
		g.add(fail("tcp_443", "", "port 443 refused the connection: "+p.tcpErr.Error()))
	default:
		g.add(ok("tcp_443", "open", "port 443 accepts connections"))
	}

	switch {
	case !p.tcpDone || p.tcpErr != nil:
		g.add(na("tls_handshake", "port 443 could not be reached"))
		g.add(na("certificate", "port 443 could not be reached"))
		g.add(na("certificate_expiry", "port 443 could not be reached"))
	case p.tlsErr != nil:
		g.add(fail("tls_handshake", "", "the TLS handshake failed: "+p.tlsErr.Error()))
		g.add(na("certificate", "there was no TLS handshake"))
		g.add(na("certificate_expiry", "there was no TLS handshake"))
	default:
		g.add(ok("tls_handshake", p.tls.VersionName(), "the front completed a TLS handshake"))
		if err := p.tls.HostnameErr; err != nil {
			g.add(fail("certificate", p.tls.Summary(), "the certificate does not cover "+t.Hostname+": "+err.Error()))
		} else {
			g.add(ok("certificate", p.tls.Summary(), "the certificate covers "+t.Hostname))
		}
		g.add(certExpiry(p.tls.NotAfter))
	}

	g.add(fakeTLS(t, p))
	return g.group()
}

func certExpiry(notAfter time.Time) domain.DiagnosticCheck {
	days := int(time.Until(notAfter).Hours() / 24)
	value := strconv.Itoa(days) + " days"
	switch {
	case days < 0:
		return fail("certificate_expiry", value, "the certificate expired on "+notAfter.Format(time.RFC3339))
	case days < certWarnDays:
		return warn("certificate_expiry", value, "the certificate expires on "+notAfter.Format(time.RFC3339))
	default:
		return ok("certificate_expiry", value, "the certificate is valid until "+notAfter.Format(time.RFC3339))
	}
}

func fakeTLS(t Target, p publicFacts) domain.DiagnosticCheck {
	port := strconv.Itoa(t.ClassicPort)
	switch {
	case !t.Telemt:
		return na("fake_tls_listener", "this node does not run telemt")
	case t.TLSDomain == "" || t.ClassicPort <= 0:
		return na("fake_tls_listener", "no Fake-TLS listener is configured for this node")
	case !p.resolved():
		return na("fake_tls_listener", "the hostname did not resolve")
	case !p.maskDone:
		return na("fake_tls_listener", "the listener was not probed")
	case p.maskErr != nil:
		return fail("fake_tls_listener", "", "port "+port+" did not handshake as "+t.TLSDomain+": "+p.maskErr.Error())
	case p.mask.HostnameErr != nil:
		return fail("fake_tls_listener", p.mask.Summary(), "port "+port+" presents a certificate that is not "+t.TLSDomain)
	default:
		return ok("fake_tls_listener", "port "+port, "the listener masks as "+t.TLSDomain)
	}
}

func reverseProxyGroup(p publicFacts, n nodeFacts) domain.DiagnosticGroup {
	g := newGroup(domain.GroupReverseProxy)
	switch {
	case !p.tlsOK():
		g.add(na("https_response", "there was no TLS handshake to send a request over"))
		g.add(na("decoy_site", "there was no TLS handshake to send a request over"))
	case p.httpErr != nil:
		g.add(fail("https_response", "", "the front accepted TLS but not a request: "+p.httpErr.Error()))
		g.add(na("decoy_site", "the request did not complete"))
	default:
		g.add(httpStatus(p.http))
		g.add(decoySite(p.http))
	}

	if detail := n.linkDetail(); detail != "" {
		g.add(na("caddy_service", detail))
	} else if n.health.CaddyActive {
		g.add(ok("caddy_service", "active", "caddy is running on the node"))
	} else {
		g.add(fail("caddy_service", "inactive", "caddy is not running on the node"))
	}
	return g.group()
}

func httpStatus(h nodecheck.HTTPProbe) domain.DiagnosticCheck {
	value := strconv.Itoa(h.Status)
	switch {
	case h.Status >= 300 && h.Status < 400:
		return warn("https_response", value, "the front redirects to "+h.Location)
	case h.Status != 200:
		return warn("https_response", value, "the front answered with "+strconv.Itoa(h.Status))
	default:
		return ok("https_response", value, "the front answered 200 over "+h.Proto)
	}
}

func decoySite(h nodecheck.HTTPProbe) domain.DiagnosticCheck {
	if h.Status != 200 {
		return na("decoy_site", "the front did not answer 200")
	}
	value := strconv.Itoa(h.Bytes) + " bytes"
	if h.Bytes == 0 {
		return warn("decoy_site", value, "the front answered 200 with an empty body")
	}
	return ok("decoy_site", value, "the decoy site is served")
}

func webTransportGroup(t Target, p publicFacts, n nodeFacts) domain.DiagnosticGroup {
	g := newGroup(domain.GroupWebTransport)
	switch {
	case !p.tlsOK():
		g.add(na("http2", "there was no TLS handshake to negotiate over"))
	case p.tls.NegotiatedALPN == "h2":
		g.add(ok("http2", "h2", "the front negotiates HTTP/2"))
	case p.tls.NegotiatedALPN == "":
		g.add(warn("http2", "none", "the front negotiated no ALPN protocol; WEB carriers fall back to HTTP/1.1"))
	default:
		g.add(warn("http2", p.tls.NegotiatedALPN, "the front chose "+p.tls.NegotiatedALPN+" over HTTP/2"))
	}

	if !t.Telemt {
		g.add(na("web_upstream", "this node does not run telemt"))
		g.add(na("carrier_selections", "this node does not run telemt"))
		g.add(na("carrier_failures", "this node does not run telemt"))
		g.add(na("web_rejections", "this node does not run telemt"))
		return g.group()
	}
	g.add(webUpstream(p, n))
	if n.metricsErr != nil {
		detail := "the node's metrics could not be read: " + n.metricsErr.Error()
		if d := n.linkDetail(); d != "" {
			detail = d
		}
		g.add(na("carrier_selections", detail))
		g.add(na("carrier_failures", detail))
		g.add(na("web_rejections", detail))
		return g.group()
	}
	g.add(carrierSelections(n.metrics))
	g.add(carrierFailures(n.metrics))
	g.add(webRejections(n.metrics))
	return g.group()
}

// webUpstream judges the hop between the front and telemt, which the panel cannot address
// directly and can only see through both of its ends.
func webUpstream(p publicFacts, n nodeFacts) domain.DiagnosticCheck {
	if detail := n.linkDetail(); detail != "" {
		return na("web_upstream", detail)
	}
	switch {
	case !p.tlsOK():
		return na("web_upstream", "the front could not be reached from the panel")
	case !n.health.CaddyActive:
		return fail("web_upstream", "", "caddy is not running, so nothing forwards to telemt")
	case !n.health.Healthz:
		return fail("web_upstream", "", "caddy is running but telemt does not answer its control API")
	default:
		return ok("web_upstream", "caddy -> telemt", "the front is up and telemt answers behind it")
	}
}

func carrierSelections(m telemt.WebMetrics) domain.DiagnosticCheck {
	if !m.CarrierSelections.Present {
		return na("carrier_selections", "the node reports no "+telemt.MetricCarrierSelections+" family yet")
	}
	total := m.CarrierSelections.Total()
	value := formatCount(total)
	if total == 0 {
		return ok("carrier_selections", value, "the WEB runtime is exporting carrier metrics and has selected none yet")
	}
	return ok("carrier_selections", value, "carriers chosen: "+joinStrings(m.CarrierSelections.Carriers()))
}

func carrierFailures(m telemt.WebMetrics) domain.DiagnosticCheck {
	if !m.CarrierFailures.Present {
		return na("carrier_failures", "the node reports no "+telemt.MetricCarrierFailures+" family yet")
	}
	total := m.CarrierFailures.Total()
	value := formatCount(total)
	if total == 0 {
		return ok("carrier_failures", value, "no carrier has reported a failure")
	}
	return warn("carrier_failures", value, "carriers reporting failures: "+joinStrings(sortedCarriers(m.CarrierFailures.CarrierTotals())))
}

func webRejections(m telemt.WebMetrics) domain.DiagnosticCheck {
	if !m.Rejections.Present {
		return na("web_rejections", "the node reports no "+telemt.MetricRejections+" family yet")
	}
	total := m.Rejections.Total()
	value := formatCount(total)
	if total == 0 {
		return ok("web_rejections", value, "no WEB session has been rejected")
	}
	return warn("web_rejections", value, "rejection reasons: "+joinStrings(m.Rejections.Labels()))
}

func telemtGroup(t Target, n nodeFacts) domain.DiagnosticGroup {
	g := newGroup(domain.GroupTelemt)
	switch {
	case !n.configured:
		g.add(na("agent_link", "the panel has no driver for this node"))
	case !n.online:
		g.add(fail("agent_link", "offline", "the node's agent has no live session with the panel"))
	case n.healthErr != nil:
		g.add(fail("agent_link", "error", "the node's agent did not answer: "+n.healthErr.Error()))
	default:
		g.add(ok("agent_link", "online", "the node's agent is connected"))
	}

	detail := n.linkDetail()
	if detail != "" {
		g.add(na("service", detail))
		g.add(na("control_api", detail))
		g.add(na("readiness", detail))
		g.add(na("build", detail))
		g.add(na("tls_emulation", detail))
		g.add(na("tls_front_cache", detail))
		g.add(na("tls_front_errors", detail))
		return g.group()
	}
	engine := "telemt"
	if !t.Telemt {
		engine = "the proxy"
	}
	if n.health.RelayActive {
		g.add(ok("service", "active", engine+" is running on the node"))
	} else {
		g.add(fail("service", "inactive", engine+" is not running on the node"))
	}
	if n.health.Healthz {
		g.add(ok("control_api", "ok", "the control API answers"))
	} else {
		g.add(fail("control_api", "", "the control API did not answer on the node"))
	}
	if n.health.Readyz {
		g.add(ok("readiness", "ready", "the proxy reports itself ready to serve"))
	} else {
		g.add(fail("readiness", "not ready", "the proxy is running but not ready to serve"))
	}
	if v := n.health.TProxyVersion; v != "" {
		g.add(ok("build", v, "the node reports its build"))
	} else {
		g.add(na("build", "the node did not report a build"))
	}
	for _, tlsCheck := range tlsEmulationChecks(t, n) {
		g.add(tlsCheck)
	}
	return g.group()
}

func tlsEmulationChecks(t Target, n nodeFacts) []domain.DiagnosticCheck {
	if !t.Telemt {
		detail := "this node does not run telemt"
		return []domain.DiagnosticCheck{na("tls_emulation", detail), na("tls_front_cache", detail), na("tls_front_errors", detail)}
	}
	if supported, known := n.health.Capabilities.Determined(telemt.CapTLSEmulation); known && !supported {
		detail := "this telemt version does not support TLS emulation"
		return []domain.DiagnosticCheck{na("tls_emulation", detail), na("tls_front_cache", detail), na("tls_front_errors", detail)}
	} else if !known {
		detail := "the node did not report whether TLS emulation is supported"
		return []domain.DiagnosticCheck{na("tls_emulation", detail), na("tls_front_cache", detail), na("tls_front_errors", detail)}
	}
	if n.metricsErr != nil || !n.metrics.TLSFrontDomains.Present {
		detail := "the node reports no " + telemt.MetricTLSFrontDomains + " family"
		return []domain.DiagnosticCheck{na("tls_emulation", detail), na("tls_front_cache", detail), tlsFrontErrors(n.metrics)}
	}
	configured, _ := n.metrics.TLSFrontDomains.Get("configured")
	emitted, _ := n.metrics.TLSFrontDomains.Get("emitted")
	suppressed, _ := n.metrics.TLSFrontDomains.Get("suppressed")
	var enabled domain.DiagnosticCheck
	switch {
	case configured == 0:
		enabled = warn("tls_emulation", "no domains", "TLS emulation is supported, but no TLS-front domain is configured")
	case emitted > 0:
		enabled = ok("tls_emulation", "enabled", "Telemt exports active TLS-front profiles")
	default:
		enabled = fail("tls_emulation", "inactive", "no TLS-front profile is active; emulation may be disabled or bootstrap failed")
	}
	var cache domain.DiagnosticCheck
	if configured > 0 && emitted >= configured && suppressed == 0 {
		cache = ok("tls_front_cache", formatCount(emitted), "every configured TLS-front profile is available")
	} else if configured > 0 {
		cache = fail("tls_front_cache", formatCount(emitted)+" / "+formatCount(configured), "one or more TLS-front profiles are missing from the runtime cache")
	} else {
		cache = na("tls_front_cache", "there is no configured TLS-front domain to cache")
	}
	return []domain.DiagnosticCheck{enabled, cache, tlsFrontErrors(n.metrics)}
}

func tlsFrontErrors(metrics telemt.WebMetrics) domain.DiagnosticCheck {
	if !metrics.HandshakeFailures.Present {
		return na("tls_front_errors", "the node reports no "+telemt.MetricHandshakeFailures+" family")
	}
	total := metrics.HandshakeFailures.Total()
	if total == 0 {
		return ok("tls_front_errors", "0", "no TLS handshake failures have been recorded in this process")
	}
	return warn("tls_front_errors", formatCount(total), "process-lifetime TLS handshake failures: "+joinStrings(metrics.HandshakeFailures.Labels()))
}

func telegramGroup(n nodeFacts) domain.DiagnosticGroup {
	g := newGroup(domain.GroupTelegram)
	detail := n.linkDetail()
	if detail == "" && !n.health.DcDataAvailable {
		detail = "the node reports no upstream health data"
	}
	if detail != "" {
		g.add(na("upstream_health", detail))
		g.add(na("upstream_latency", detail))
		g.add(na("datacenters", detail))
		g.add(na("connect_attempts", detail))
		return g.group()
	}

	h := n.health
	age := strconv.FormatInt(h.UpstreamLastCheckAgeSecs, 10) + "s ago"
	if h.UpstreamHealthy {
		g.add(ok("upstream_health", "healthy", "telemt's route to Telegram is healthy, last checked "+age))
	} else {
		g.add(fail("upstream_health", "unhealthy", "telemt cannot reach Telegram ("+strconv.Itoa(h.UpstreamFails)+" failures, last checked "+age+")"))
	}

	switch {
	case h.EffectiveLatencyMs <= 0:
		g.add(na("upstream_latency", "telemt has not measured a latency yet"))
	case h.EffectiveLatencyMs > latencyWarnMs:
		g.add(warn("upstream_latency", formatMs(h.EffectiveLatencyMs), "the route to Telegram is slow"))
	default:
		g.add(ok("upstream_latency", formatMs(h.EffectiveLatencyMs), "the route to Telegram is responsive"))
	}

	known := 0
	for _, dc := range h.DCs {
		if dc.Known {
			known++
		}
	}
	value := strconv.Itoa(known) + " of " + strconv.Itoa(len(h.DCs))
	switch {
	case len(h.DCs) == 0:
		g.add(na("datacenters", "telemt listed no datacenters"))
	case known == 0:
		g.add(fail("datacenters", value, "no Telegram datacenter has answered a latency probe"))
	default:
		g.add(ok("datacenters", value, "Telegram datacenters with a measured latency"))
	}

	switch {
	case h.ConnectSuccessTotal > 0:
		g.add(ok("connect_attempts", formatCount(float64(h.ConnectSuccessTotal)), "connections to Telegram are succeeding"))
	case h.ConnectFailTotal > 0:
		g.add(fail("connect_attempts", formatCount(float64(h.ConnectFailTotal))+" failed", "every connection attempt to Telegram has failed"))
	default:
		g.add(na("connect_attempts", "telemt has not attempted a connection yet"))
	}
	return g.group()
}

func nameNotFound(err error) bool {
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		return false
	}
	return dnsErr.IsNotFound
}

func containsIP(addrs []net.IPAddr, want string) bool {
	ip := net.ParseIP(want)
	if ip == nil {
		return false
	}
	for _, a := range addrs {
		if ip.Equal(a.IP) {
			return true
		}
	}
	return false
}
