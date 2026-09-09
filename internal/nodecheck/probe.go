package nodecheck

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// TLSProbe is a completed TLS handshake, reduced to what a caller can judge.
type TLSProbe struct {
	Version        uint16
	NegotiatedALPN string
	CommonName     string
	Issuer         string
	DNSNames       []string
	NotBefore      time.Time
	NotAfter       time.Time
	// HostnameErr is set when the certificate does not cover the name asked for; the
	// handshake itself still succeeded.
	HostnameErr error
}

// VersionName is the negotiated TLS version, e.g. "TLS 1.3".
func (p TLSProbe) VersionName() string { return tls.VersionName(p.Version) }

// Summary is the certificate line the panel shows.
func (p TLSProbe) Summary() string {
	return fmt.Sprintf("CN=%s issuer=%s notAfter=%s", p.CommonName, p.Issuer, p.NotAfter.Format(time.RFC3339))
}

// HTTPProbe is one HTTP response, reduced to what a caller can judge.
type HTTPProbe struct {
	Status   int
	Proto    string
	Location string
	Server   string
	Bytes    int
}

// ProbeBudget is how long the next probe may take when remaining probes share ctx's deadline.
func (c *Checker) ProbeBudget(ctx context.Context, remaining int) time.Duration {
	return c.probeBudget(ctx, remaining)
}

// LookupIPs resolves host's A/AAAA records within timeout.
func (c *Checker) LookupIPs(ctx context.Context, timeout time.Duration, host string) ([]net.IPAddr, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.resolver().LookupIPAddr(ctx, host)
}

// DialTCP opens and closes one TCP connection to host:port within timeout.
func (c *Checker) DialTCP(ctx context.Context, timeout time.Duration, host, port string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.dial(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	return conn.Close()
}

// ProbeTLS handshakes with host:port offering serverName as the SNI and alpn as the protocol
// list. A certificate that does not cover serverName is reported in TLSProbe.HostnameErr, not
// as an error: the handshake happened and its certificate is worth reporting.
func (c *Checker) ProbeTLS(ctx context.Context, timeout time.Duration, host, port, serverName string, alpn []string) (TLSProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.dial(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return TLSProbe{}, err
	}
	defer func() { _ = conn.Close() }()

	cfg := &tls.Config{}
	if c.TLSConfig != nil {
		cfg = c.TLSConfig.Clone()
	}
	cfg.ServerName = serverName
	if len(alpn) > 0 {
		cfg.NextProtos = alpn
	}

	tlsConn := tls.Client(conn, cfg)
	defer func() { _ = tlsConn.Close() }()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return TLSProbe{}, err
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return TLSProbe{}, errors.New("no certificate presented")
	}
	cert := state.PeerCertificates[0]
	probe := TLSProbe{
		Version:        state.Version,
		NegotiatedALPN: state.NegotiatedProtocol,
		CommonName:     cert.Subject.CommonName,
		Issuer:         cert.Issuer.CommonName,
		DNSNames:       cert.DNSNames,
		NotBefore:      cert.NotBefore,
		NotAfter:       cert.NotAfter,
		HostnameErr:    cert.VerifyHostname(serverName),
	}
	if probe.CommonName == "" && len(cert.DNSNames) > 0 {
		probe.CommonName = strings.Join(cert.DNSNames, ",")
	}
	if probe.Issuer == "" && len(cert.Issuer.Organization) > 0 {
		probe.Issuer = strings.Join(cert.Issuer.Organization, ",")
	}
	return probe, nil
}

// ProbeHTTP issues GET https://host/ without following redirects.
func (c *Checker) ProbeHTTP(ctx context.Context, timeout time.Duration, host string) (HTTPProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return HTTPProbe{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return HTTPProbe{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return HTTPProbe{
		Status:   resp.StatusCode,
		Proto:    resp.Proto,
		Location: resp.Header.Get("Location"),
		Server:   resp.Header.Get("Server"),
		Bytes:    len(body),
	}, nil
}
