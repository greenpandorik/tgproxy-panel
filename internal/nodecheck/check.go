// Package nodecheck probes a node's public prerequisites.
package nodecheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Result is the outcome of a single probe.
type Result struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Advisory bool   `json:"advisory,omitempty"`
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

const expiryWarnWindow = 7 * 24 * time.Hour

// Checker runs node prerequisite probes.
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

func (c *Checker) probeBudget(ctx context.Context, remainingProbes int) time.Duration {
	d := c.timeout()
	deadline, ok := ctx.Deadline()
	if !ok || remainingProbes <= 0 {
		return d
	}
	if share := time.Until(deadline) / time.Duration(remainingProbes); share < d {
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
		Transport:     &http.Transport{DialContext: c.dial, TLSClientConfig: c.TLSConfig},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *Checker) Run(ctx context.Context, hostname, expectedIP string) Report {
	return c.RunTelemt(ctx, hostname, expectedIP, "", 0)
}

// RunTelemt is Run plus the telemt-only `mask` probe.
func (c *Checker) RunTelemt(ctx context.Context, hostname, expectedIP, tlsDomain string, classicPort int) Report {
	report := Report{RanAt: time.Now()}
	wantMask := classicPort > 0 && tlsDomain != ""
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
	addrs, err := c.LookupIPs(ctx, timeout, hostname)
	if err != nil {
		return Result{Name: "dns_a", OK: false, Detail: err.Error()}
	}
	if len(addrs) == 0 {
		return Result{Name: "dns_a", OK: false, Detail: "no A/AAAA records"}
	}
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
	if err := c.DialTCP(ctx, timeout, hostname, port); err != nil {
		return Result{Name: name, OK: false, Detail: err.Error()}
	}
	return Result{Name: name, OK: true, Detail: "connected"}
}

func (c *Checker) checkTLS(ctx context.Context, timeout time.Duration, hostname string) Result {
	probe, err := c.ProbeTLS(ctx, timeout, hostname, "443", hostname, nil)
	if err != nil {
		return Result{Name: "tls_cert", OK: false, Detail: err.Error()}
	}
	if probe.HostnameErr != nil {
		return Result{Name: "tls_cert", OK: false, Detail: probe.HostnameErr.Error()}
	}
	detail := probe.Summary()
	if time.Until(probe.NotAfter) < expiryWarnWindow {
		return Result{Name: "tls_cert", OK: false, Detail: detail + " (expiring soon)"}
	}
	return Result{Name: "tls_cert", OK: true, Detail: detail}
}

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
		if strings.Contains(err.Error(), "protocol version not supported") {
			return Result{Name: name, Advisory: true, Detail: "server does not offer TLS 1.3 (no post-quantum key exchange)"}
		}
		return Result{Name: name, Advisory: true, Detail: err.Error()}
	}
	curve := tlsConn.ConnectionState().CurveID
	if curve == tls.X25519MLKEM768 {
		return Result{Name: name, OK: true, Advisory: true, Detail: "X25519MLKEM768 negotiated"}
	}
	return Result{Name: name, Advisory: true, Detail: fmt.Sprintf("server chose %s (no post-quantum key exchange)", curve)}
}

func (c *Checker) checkHTTP(ctx context.Context, timeout time.Duration, hostname string) Result {
	probe, err := c.ProbeHTTP(ctx, timeout, hostname)
	if err != nil {
		return Result{Name: "http_root", OK: false, Detail: err.Error()}
	}
	if probe.Status >= 300 && probe.Status < 400 {
		return Result{Name: "http_root", OK: false, Detail: fmt.Sprintf("redirect to %s not followed", probe.Location)}
	}
	detail := fmt.Sprintf("status=%d bytes=%d", probe.Status, probe.Bytes)
	if probe.Status != http.StatusOK || probe.Bytes == 0 {
		return Result{Name: "http_root", OK: false, Detail: detail}
	}
	return Result{Name: "http_root", OK: true, Detail: detail}
}

func (c *Checker) checkMask(ctx context.Context, timeout time.Duration, hostname, tlsDomain string, classicPort int) Result {
	addr := net.JoinHostPort(hostname, strconv.Itoa(classicPort))
	probe, err := c.ProbeTLS(ctx, timeout, hostname, strconv.Itoa(classicPort), tlsDomain, nil)
	if err != nil {
		return Result{Name: "mask", OK: false, Detail: fmt.Sprintf("%s (SNI %s): %v", addr, tlsDomain, err)}
	}
	if probe.HostnameErr != nil {
		return Result{Name: "mask", OK: false, Detail: probe.HostnameErr.Error()}
	}
	return Result{Name: "mask", OK: true, Detail: fmt.Sprintf("%s masks to %s:443", addr, tlsDomain)}
}
