// Package nodecheck probes a node's public prerequisites - DNS resolution,
// reachability of the ports Let's Encrypt and the relay depend on, the TLS
// certificate served for the hostname, and that the site actually answers -
// so an operator can tell "the node registered" from "the node is reachable
// from the internet and ready to serve traffic".
package nodecheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Result is the outcome of a single probe.
type Result struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Advisory marks an informational probe (pq_kex): its OK is reported but never
	// counted in Report.AllOK, and the SPA renders a failing advisory in a neutral tone.
	Advisory bool `json:"advisory,omitempty"`
}

// Report is the outcome of a full Run, in check order.
type Report struct {
	RanAt   time.Time `json:"ran_at"`
	Results []Result  `json:"results"`
	// AllOK is the roll-up of every non-advisory result.
	AllOK bool `json:"all_ok"`
}

// Resolver looks up A/AAAA records for a host. *net.Resolver satisfies this.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// defaultTimeout bounds each individual probe when Checker.Timeout is unset.
const defaultTimeout = 5 * time.Second

// expiryWarnWindow is how far ahead of a certificate's NotAfter tls_cert
// starts failing, so an operator has time to renew before Let's Encrypt (or
// whatever issued it) actually lapses.
const expiryWarnWindow = 7 * 24 * time.Hour

// Checker runs node prerequisite probes. Resolver, Dial, TLSConfig and
// HTTPClient default to the real network stack when left nil; tests
// substitute a fake resolver and redirect Dial to local listeners/servers.
//
// An injected HTTPClient MUST set CheckRedirect to something that does not follow
// redirects (http.ErrUseLastResponse is what the default client below uses). The
// checked host controls its own Location header, so a client that chases redirects
// turns http_root into an SSRF primitive: the panel would issue a request to any
// host the node names, and the response line is echoed back into Report.Detail.
type Checker struct {
	Resolver   Resolver
	Dial       func(ctx context.Context, network, addr string) (net.Conn, error)
	TLSConfig  *tls.Config
	HTTPClient *http.Client
	Timeout    time.Duration
}

func (c *Checker) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

// probeBudget returns the timeout for the next probe: the per-probe default, cut down to an
// even share of whatever is left of ctx's deadline when that share is smaller.
//
// Without this the probes are independent 5s timeouts and up to five of them run, so a
// reachable-but-slow node burns the caller's whole budget on dns_a/tcp_80/tcp_443 and then
// reports tls_cert and http_root as "context deadline exceeded" - a panel-side timeout the
// operator goes looking for on the node. Sharing the remaining budget means every probe gets
// a turn and reports its own result.
func (c *Checker) probeBudget(ctx context.Context, remainingProbes int) time.Duration {
	d := c.timeout()
	deadline, ok := ctx.Deadline()
	if !ok || remainingProbes <= 0 {
		return d
	}
	if share := time.Until(deadline) / time.Duration(remainingProbes); share < d {
		// A non-positive share means the budget is already spent; the probe still runs so
		// it can report its own failure rather than being silently skipped.
		if share < time.Millisecond {
			return time.Millisecond
		}
		return share
	}
	return d
}

func (c *Checker) resolver() Resolver {
	if c.Resolver != nil {
		return c.Resolver
	}
	return net.DefaultResolver
}

func (c *Checker) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial(ctx, network, addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (c *Checker) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{
		Transport: &http.Transport{DialContext: c.dial, TLSClientConfig: c.TLSConfig},
		// http_root must never follow a redirect the checked site itself
		// issues: a node (or whoever controls its DNS/site) could otherwise
		// redirect this request to an arbitrary host and have the panel
		// make a request on its behalf (SSRF). ErrUseLastResponse makes Do
		// return the 3xx response as-is instead of chasing Location.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Run executes every check in order (dns_a, tcp_80, tcp_443, tls_cert, pq_kex,
// http_root) and returns the combined report. tls_cert, pq_kex and http_root
// depend on port 443 being reachable and are reported as skipped, not run, when
// tcp_443 fails - every other check is independent and always runs. pq_kex is
// advisory: it never flips AllOK.
//
// Run honours ctx's deadline across the whole sequence, not just per probe: its caller
// (POST /nodes/{id}/check, internal/api/nodes_check.go) gives the probes a 15s budget while
// five probes at the 5s default would want 25s, so each probe's timeout is capped at an even
// share of the time still left. Probes therefore degrade together instead of the last two
// always reporting a deadline the operator cannot see.
func (c *Checker) Run(ctx context.Context, hostname, expectedIP string) Report {
	return c.RunTelemt(ctx, hostname, expectedIP, "", 0)
}

// RunTelemt is Run plus the telemt-only `mask` probe. tlsDomain and classicPort come from the
// node row; a zero port (a tproxy node, or a node whose listener is not known) skips the probe
// entirely and the report is exactly Run's.
func (c *Checker) RunTelemt(ctx context.Context, hostname, expectedIP, tlsDomain string, classicPort int) Report {
	report := Report{RanAt: time.Now()}
	wantMask := classicPort > 0 && tlsDomain != ""
	// The number of probes still to run, counted down so each one claims its share of what
	// is left rather than of the original budget.
	remaining := 6
	if wantMask {
		remaining = 7
	}
	next := func() time.Duration {
		d := c.probeBudget(ctx, remaining)
		remaining--
		return d
	}
	report.Results = append(report.Results,
		c.checkDNS(ctx, next(), hostname, expectedIP),
		c.checkTCP(ctx, next(), hostname, "80", "tcp_80"),
	)
	tcp443 := c.checkTCP(ctx, next(), hostname, "443", "tcp_443")
	report.Results = append(report.Results, tcp443)

	if tcp443.OK {
		report.Results = append(report.Results,
			c.checkTLS(ctx, next(), hostname),
			c.checkPQKex(ctx, next(), hostname),
			c.checkHTTP(ctx, next(), hostname),
		)
	} else {
		skipped := Result{Detail: "skipped: tcp_443 failed"}
		tlsSkipped, pqSkipped, httpSkipped := skipped, skipped, skipped
		tlsSkipped.Name, pqSkipped.Name, httpSkipped.Name = "tls_cert", "pq_kex", "http_root"
		pqSkipped.Advisory = true
		report.Results = append(report.Results, tlsSkipped, pqSkipped, httpSkipped)
	}
	if wantMask {
		report.Results = append(report.Results, c.checkMask(ctx, next(), hostname, tlsDomain, classicPort))
	}

	report.AllOK = true
	for _, r := range report.Results {
		if !r.OK && !r.Advisory {
			report.AllOK = false
			break
		}
	}
	return report
}

func (c *Checker) checkDNS(ctx context.Context, timeout time.Duration, hostname, expectedIP string) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := c.resolver().LookupIPAddr(ctx, hostname)
	if err != nil {
		return Result{Name: "dns_a", OK: false, Detail: err.Error()}
	}
	if len(addrs) == 0 {
		return Result{Name: "dns_a", OK: false, Detail: "no A/AAAA records"}
	}
	// Parsed once and compared with net.IP.Equal rather than a raw string
	// match, so "1.2.3.4" still matches an IPv4-in-IPv6 form like
	// "::ffff:1.2.3.4" that String() would render differently.
	var expected net.IP
	if expectedIP != "" {
		expected = net.ParseIP(expectedIP)
	}
	ips := make([]string, len(addrs))
	found := expectedIP == ""
	for i, a := range addrs {
		ips[i] = a.IP.String()
		if expected != nil && expected.Equal(a.IP) {
			found = true
		}
	}
	detail := strings.Join(ips, ", ")
	if !found {
		return Result{Name: "dns_a", OK: false, Detail: fmt.Sprintf("%s (expected %s)", detail, expectedIP)}
	}
	return Result{Name: "dns_a", OK: true, Detail: detail}
}

func (c *Checker) checkTCP(ctx context.Context, timeout time.Duration, hostname, port, name string) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.dial(ctx, "tcp", net.JoinHostPort(hostname, port))
	if err != nil {
		return Result{Name: name, OK: false, Detail: err.Error()}
	}
	_ = conn.Close()
	return Result{Name: name, OK: true, Detail: "connected"}
}

func (c *Checker) checkTLS(ctx context.Context, timeout time.Duration, hostname string) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.dial(ctx, "tcp", net.JoinHostPort(hostname, "443"))
	if err != nil {
		return Result{Name: "tls_cert", OK: false, Detail: err.Error()}
	}
	defer func() { _ = conn.Close() }()

	cfg := &tls.Config{}
	if c.TLSConfig != nil {
		cfg = c.TLSConfig.Clone()
	}
	cfg.ServerName = hostname

	tlsConn := tls.Client(conn, cfg)
	defer func() { _ = tlsConn.Close() }()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return Result{Name: "tls_cert", OK: false, Detail: err.Error()}
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Result{Name: "tls_cert", OK: false, Detail: "no certificate presented"}
	}
	cert := state.PeerCertificates[0]
	if err := cert.VerifyHostname(hostname); err != nil {
		return Result{Name: "tls_cert", OK: false, Detail: err.Error()}
	}
	// Modern certificates (Let's Encrypt included) frequently leave the
	// subject/issuer common name blank and carry the hostname only in the
	// SAN list, so both fall back to something identifying when empty.
	cn := cert.Subject.CommonName
	if cn == "" && len(cert.DNSNames) > 0 {
		cn = strings.Join(cert.DNSNames, ",")
	}
	issuer := cert.Issuer.CommonName
	if issuer == "" && len(cert.Issuer.Organization) > 0 {
		issuer = strings.Join(cert.Issuer.Organization, ",")
	}
	detail := fmt.Sprintf("CN=%s issuer=%s notAfter=%s", cn, issuer, cert.NotAfter.Format(time.RFC3339))
	if time.Until(cert.NotAfter) < expiryWarnWindow {
		return Result{Name: "tls_cert", OK: false, Detail: detail + " (expiring soon)"}
	}
	return Result{Name: "tls_cert", OK: true, Detail: detail}
}

// checkPQKex asks the node's TLS front for a TLS 1.3 handshake with Go's default curve
// preferences, which offer the post-quantum hybrid X25519MLKEM768 first, and reports which
// key exchange the server picked. Caddy negotiates it with modern clients; a front that
// falls back to a classical curve is not broken, just not future-proof, so the result is
// advisory and never fails the report.
func (c *Checker) checkPQKex(ctx context.Context, timeout time.Duration, hostname string) Result {
	const name = "pq_kex"
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.dial(ctx, "tcp", net.JoinHostPort(hostname, "443"))
	if err != nil {
		return Result{Name: name, Advisory: true, Detail: err.Error()}
	}
	defer func() { _ = conn.Close() }()

	cfg := &tls.Config{}
	if c.TLSConfig != nil {
		cfg = c.TLSConfig.Clone()
	}
	cfg.ServerName = hostname
	cfg.MinVersion = tls.VersionTLS13
	cfg.CurvePreferences = nil // Go's defaults: X25519MLKEM768 first.

	tlsConn := tls.Client(conn, cfg)
	defer func() { _ = tlsConn.Close() }()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return Result{Name: name, Advisory: true, Detail: err.Error()}
	}
	curve := tlsConn.ConnectionState().CurveID
	if curve == tls.X25519MLKEM768 {
		return Result{Name: name, OK: true, Advisory: true, Detail: "X25519MLKEM768 negotiated"}
	}
	return Result{Name: name, Advisory: true, Detail: fmt.Sprintf("server chose %s (no post-quantum key exchange)", curve)}
}

func (c *Checker) checkHTTP(ctx context.Context, timeout time.Duration, hostname string) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+hostname+"/", nil)
	if err != nil {
		return Result{Name: "http_root", OK: false, Detail: err.Error()}
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Result{Name: "http_root", OK: false, Detail: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Result{Name: "http_root", OK: false, Detail: fmt.Sprintf("redirect to %s not followed", resp.Header.Get("Location"))}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	detail := fmt.Sprintf("status=%d bytes=%d", resp.StatusCode, len(body))
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		return Result{Name: "http_root", OK: false, Detail: detail}
	}
	return Result{Name: "http_root", OK: true, Detail: detail}
}

// checkMask probes the Fake-TLS listener the way an uninvited client would: connect to
// hostname:classic_port and offer tls_domain as the SNI, with no MTProto secret.
//
// telemt answers a known SNI without a valid secret with `unknown_sni_action = "mask"`: it
// relays the connection to tls_domain:443, which is the node's own public address served by
// Caddy. A completed handshake with a certificate valid for tls_domain therefore proves three
// things at once - the port is open, telemt is listening on it, and the node can reach its own
// public address. That last one is the part nothing else checks: where NAT hairpin or an egress
// policy stops a host from connecting to its own public IP, masking fails silently and a probe
// sees a connection error instead of the cover site, which is exactly the fingerprint the
// design exists to avoid.
func (c *Checker) checkMask(ctx context.Context, timeout time.Duration, hostname, tlsDomain string, classicPort int) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(hostname, strconv.Itoa(classicPort))
	conn, err := c.dial(ctx, "tcp", addr)
	if err != nil {
		return Result{Name: "mask", OK: false, Detail: err.Error()}
	}
	defer func() { _ = conn.Close() }()

	cfg := &tls.Config{}
	if c.TLSConfig != nil {
		cfg = c.TLSConfig.Clone()
	}
	cfg.ServerName = tlsDomain
	tlsConn := tls.Client(conn, cfg)
	defer func() { _ = tlsConn.Close() }()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return Result{Name: "mask", OK: false, Detail: fmt.Sprintf("%s (SNI %s): %v", addr, tlsDomain, err)}
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Result{Name: "mask", OK: false, Detail: "no certificate presented"}
	}
	if err := state.PeerCertificates[0].VerifyHostname(tlsDomain); err != nil {
		return Result{Name: "mask", OK: false, Detail: err.Error()}
	}
	return Result{Name: "mask", OK: true, Detail: fmt.Sprintf("%s masks to %s:443", addr, tlsDomain)}
}
