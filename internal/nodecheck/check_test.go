package nodecheck

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeResolver struct {
	addrs []net.IPAddr
	err   error
}

func (f fakeResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	return f.addrs, f.err
}

func resultByName(t *testing.T, report Report, name string) Result {
	t.Helper()
	for _, r := range report.Results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no result named %q in %+v", name, report.Results)
	return Result{}
}

func noDial(_ context.Context, _, _ string) (net.Conn, error) {
	return nil, errors.New("dial disabled in this test")
}

func TestDNSMissingRecord(t *testing.T) {
	c := &Checker{Resolver: fakeResolver{}, Dial: noDial}
	report := c.Run(t.Context(), "example.com", "")
	dns := resultByName(t, report, "dns_a")
	if dns.OK {
		t.Fatalf("expected dns_a to fail with no records, got %+v", dns)
	}
}

func TestDNSExpectedIPMismatch(t *testing.T) {
	c := &Checker{Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}}}, Dial: noDial}
	report := c.Run(t.Context(), "example.com", "9.9.9.9")
	dns := resultByName(t, report, "dns_a")
	if dns.OK {
		t.Fatalf("expected dns_a to fail on expectedIP mismatch, got %+v", dns)
	}
	if !strings.Contains(dns.Detail, "1.2.3.4") {
		t.Fatalf("detail should list resolved IPs, got %q", dns.Detail)
	}
}

// newLocalListener starts a plain TCP listener that immediately closes any accepted connection.
func newLocalListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return ln
}

func TestRunAllOK(t *testing.T) {
	plain := newLocalListener(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello world"))
	}))
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
		case "80":
			return net.Dial(network, plain.Addr().String())
		case "443":
			return net.Dial(network, srv.Listener.Addr().String())
		default:
			return nil, fmt.Errorf("unexpected dial port %q", port)
		}
	}

	c := &Checker{
		Resolver:  fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
		Dial:      dial,
		TLSConfig: &tls.Config{RootCAs: pool},
		Timeout:   2 * time.Second,
	}
	report := c.Run(t.Context(), "example.com", "5.6.7.8")

	if !report.AllOK {
		t.Fatalf("expected all checks ok, got %+v", report.Results)
	}
	if len(report.Results) != 6 {
		t.Fatalf("expected 6 results, got %d: %+v", len(report.Results), report.Results)
	}
	wantOrder := []string{"dns_a", "tcp_80", "tcp_443", "tls_cert", "pq_kex", "http_root"}
	for i, name := range wantOrder {
		if report.Results[i].Name != name {
			t.Fatalf("result[%d] = %q, want %q", i, report.Results[i].Name, name)
		}
	}
	httpRes := resultByName(t, report, "http_root")
	if !strings.Contains(httpRes.Detail, "status=200") || !strings.Contains(httpRes.Detail, "bytes=11") {
		t.Fatalf("http_root detail = %q", httpRes.Detail)
	}
	tlsRes := resultByName(t, report, "tls_cert")
	if !strings.Contains(tlsRes.Detail, "example.com") {
		t.Fatalf("tls_cert detail = %q", tlsRes.Detail)
	}
}

func TestRunTCP443RefusedSkipsDependents(t *testing.T) {
	plain := newLocalListener(t)
	dial := func(_ context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected dial host %q", host)
		}
		if port == "80" {
			return net.Dial(network, plain.Addr().String())
		}
		return nil, errors.New("connection refused")
	}
	c := &Checker{
		Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
		Dial:     dial,
		Timeout:  2 * time.Second,
	}
	report := c.Run(t.Context(), "example.com", "")
	if report.AllOK {
		t.Fatalf("expected AllOK false, got %+v", report.Results)
	}
	if resultByName(t, report, "tcp_80").OK != true {
		t.Fatalf("tcp_80 should still succeed independently")
	}
	if resultByName(t, report, "tcp_443").OK {
		t.Fatalf("tcp_443 should fail")
	}
	tlsRes := resultByName(t, report, "tls_cert")
	if tlsRes.OK || tlsRes.Detail != "skipped: tcp_443 failed" {
		t.Fatalf("tls_cert should be skipped, got %+v", tlsRes)
	}
	httpRes := resultByName(t, report, "http_root")
	if httpRes.OK || httpRes.Detail != "skipped: tcp_443 failed" {
		t.Fatalf("http_root should be skipped, got %+v", httpRes)
	}
	pq := resultByName(t, report, "pq_kex")
	if pq.OK || !pq.Advisory || pq.Detail != "skipped: tcp_443 failed" {
		t.Fatalf("pq_kex should be skipped and stay advisory, got %+v", pq)
	}
}

func selfSignedCert(t *testing.T, hostName string, notAfter time.Time) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostName, Organization: []string{"Test Expiring Co"}},
		DNSNames:              []string{hostName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}
}

func TestTLSCertExpiringSoonFails(t *testing.T) {
	cert := selfSignedCert(t, "example.com", time.Now().Add(3*24*time.Hour))
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(cert.Leaf)

	dial := func(_ context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected dial host %q", host)
		}
		return net.Dial(network, srv.Listener.Addr().String())
	}
	c := &Checker{
		Resolver:  fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
		Dial:      dial,
		TLSConfig: &tls.Config{RootCAs: pool},
		Timeout:   2 * time.Second,
	}
	report := c.Run(t.Context(), "example.com", "")
	tlsRes := resultByName(t, report, "tls_cert")
	if tlsRes.OK {
		t.Fatalf("expected tls_cert to fail for a cert expiring in 3 days, got %+v", tlsRes)
	}
	if !strings.Contains(tlsRes.Detail, "expir") {
		t.Fatalf("detail should mention expiry, got %q", tlsRes.Detail)
	}
}

func TestHTTPRootDoesNotFollowRedirect(t *testing.T) {
	var secretHits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret" {
			atomic.AddInt32(&secretHits, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "https://example.com/secret", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	dial := func(_ context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected dial host %q", host)
		}
		return net.Dial(network, srv.Listener.Addr().String())
	}
	c := &Checker{Dial: dial, TLSConfig: &tls.Config{RootCAs: pool}, Timeout: 2 * time.Second}
	result := c.checkHTTP(t.Context(), 2*time.Second, "example.com")

	if result.OK {
		t.Fatalf("expected http_root to fail on a redirect, got %+v", result)
	}
	if !strings.Contains(result.Detail, "redirect") {
		t.Fatalf("detail should mention the redirect, got %q", result.Detail)
	}
	if atomic.LoadInt32(&secretHits) != 0 {
		t.Fatalf("client followed the redirect - SSRF risk not mitigated")
	}
}

func TestProbeBudgetSharesTheDeadline(t *testing.T) {
	c := &Checker{Timeout: 5 * time.Second}

	// No deadline: the per-probe default, unchanged.
	if got := c.probeBudget(context.Background(), 5); got != 5*time.Second {
		t.Fatalf("without a deadline: %v, want the 5s default", got)
	}

	// 15s budget over 5 probes: 3s each, comfortably under the default.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if got := c.probeBudget(ctx, 5); got > 3*time.Second || got < 2900*time.Millisecond {
		t.Fatalf("15s over 5 probes: %v, want ~3s", got)
	}
	// The same budget with only one probe left is not cut down below the default.
	if got := c.probeBudget(ctx, 1); got != 5*time.Second {
		t.Fatalf("15s with 1 probe left: %v, want the 5s default (never longer)", got)
	}

	// An exhausted budget still yields a positive timeout so the probe runs and reports.
	spent, cancelSpent := context.WithTimeout(context.Background(), -time.Second)
	defer cancelSpent()
	if got := c.probeBudget(spent, 3); got <= 0 {
		t.Fatalf("spent budget: %v, want a positive timeout", got)
	}
}

func TestRunStaysInsideTheCallersDeadline(t *testing.T) {
	block := func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := &Checker{Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}}}, Dial: block, Timeout: 5 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	start := time.Now()
	report := c.Run(ctx, "example.com", "")
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("Run took %v; it must divide the caller's 600ms budget, not spend 5s per probe", elapsed)
	}
	if len(report.Results) != 6 {
		t.Fatalf("report has %d results, want all six named: %+v", len(report.Results), report.Results)
	}
	if report.AllOK {
		t.Fatal("expected the report to fail with an unreachable host")
	}
	if report.Results[0].Name != "dns_a" || !report.Results[0].OK {
		t.Fatalf("dns_a should still have completed: %+v", report.Results[0])
	}
}

// M9: the mask probe connects to the Fake-TLS port offering tls_domain as the SNI and no secret.
func TestMaskProbeHandshakesTheFakeTLSPort(t *testing.T) {
	plain := newLocalListener(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("decoy"))
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	dial := func(_ context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		switch port {
		case "80":
			return net.Dial(network, plain.Addr().String())
		case "443", "8443":
			return net.Dial(network, srv.Listener.Addr().String())
		default:
			return nil, fmt.Errorf("unexpected dial port %q", port)
		}
	}
	c := &Checker{
		Resolver:  fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
		Dial:      dial,
		TLSConfig: &tls.Config{RootCAs: pool},
		Timeout:   2 * time.Second,
	}

	report := c.RunTelemt(t.Context(), "example.com", "5.6.7.8", "example.com", 8443)
	if len(report.Results) != 7 || !report.AllOK {
		t.Fatalf("expected seven passing probes, got %+v", report.Results)
	}
	mask := resultByName(t, report, "mask")
	if !strings.Contains(mask.Detail, "8443") || !strings.Contains(mask.Detail, "example.com:443") {
		t.Fatalf("mask detail = %q", mask.Detail)
	}

	broken := *c
	broken.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if _, port, _ := net.SplitHostPort(addr); port == "8443" {
			return nil, errors.New("connection reset by peer")
		}
		return dial(ctx, network, addr)
	}
	report = broken.RunTelemt(t.Context(), "example.com", "5.6.7.8", "example.com", 8443)
	if report.AllOK {
		t.Fatal("a Fake-TLS port that cannot be reached must fail the report")
	}
	if m := resultByName(t, report, "mask"); m.OK || !strings.Contains(m.Detail, "connection reset") {
		t.Fatalf("mask = %+v", m)
	}
}

// A tproxy node has no Fake-TLS listener, so the report is the six public probes.
func TestRunTelemtSkipsMaskWithoutAListener(t *testing.T) {
	c := &Checker{Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}}, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("no network")
	}, Timeout: time.Second}
	if got := len(c.RunTelemt(t.Context(), "example.com", "", "example.com", 0).Results); got != 6 {
		t.Fatalf("expected 6 results without a classic port, got %d", got)
	}
	if got := len(c.RunTelemt(t.Context(), "example.com", "", "", 8443).Results); got != 6 {
		t.Fatalf("expected 6 results without a tls domain, got %d", got)
	}
}

func pqKexChecker(t *testing.T, srv *httptest.Server) *Checker {
	t.Helper()
	plain := newLocalListener(t)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	dial := func(_ context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		switch port {
		case "80":
			return net.Dial(network, plain.Addr().String())
		case "443":
			return net.Dial(network, srv.Listener.Addr().String())
		default:
			return nil, fmt.Errorf("unexpected dial port %q", port)
		}
	}
	return &Checker{
		Resolver:  fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}}},
		Dial:      dial,
		TLSConfig: &tls.Config{RootCAs: pool},
		Timeout:   2 * time.Second,
	}
}

func TestPQKexNegotiated(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(srv.Close)
	report := pqKexChecker(t, srv).Run(t.Context(), "example.com", "5.6.7.8")
	pq := resultByName(t, report, "pq_kex")
	if !pq.OK || !pq.Advisory || pq.Detail != "X25519MLKEM768 negotiated" {
		t.Fatalf("pq_kex = %+v", pq)
	}
	if !report.AllOK {
		t.Fatalf("expected AllOK, got %+v", report.Results)
	}
}

// A front that only offers classical key exchange fails the probe with the curve it chose.
func TestPQKexClassicalOnlyIsAdvisory(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	srv.TLS = &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	report := pqKexChecker(t, srv).Run(t.Context(), "example.com", "5.6.7.8")
	pq := resultByName(t, report, "pq_kex")
	if pq.OK || !pq.Advisory {
		t.Fatalf("pq_kex should fail advisory against an X25519-only server, got %+v", pq)
	}
	if !strings.Contains(pq.Detail, "X25519") || !strings.Contains(pq.Detail, "no post-quantum key exchange") {
		t.Fatalf("detail should name the chosen curve, got %q", pq.Detail)
	}
	if !report.AllOK {
		t.Fatalf("an advisory failure must not flip the roll-up: %+v", report.Results)
	}
	for _, r := range report.Results {
		if r.Name != "pq_kex" && !r.OK {
			t.Fatalf("every hard probe should pass here: %+v", r)
		}
	}
	// A hard failure still flips it, advisory or not alongside.
	broken := *pqKexChecker(t, srv)
	broken.Resolver = fakeResolver{}
	if broken.Run(t.Context(), "example.com", "5.6.7.8").AllOK {
		t.Fatal("a failing dns_a must still fail the report")
	}
}
