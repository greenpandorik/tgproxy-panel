// tgwp-probe runs outside the proxy's hosting network. It does not need panel login cookies.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"tgwebproxy/internal/protocolprobe"
	"tgwebproxy/internal/reliability"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func run() error {
	panel := flag.String("panel", "", "panel HTTPS URL")
	host := flag.String("host", "", "node public hostname")
	node := flag.String("node", "", "node UUID")
	location := flag.String("location", "", "configured probe location")
	runner := flag.String("client-check", "", "optional absolute executable that tests authenticated FakeTLS and WEB and returns JSON")
	configPath := flag.String("protocol-config", os.Getenv("TGWP_PROBE_CONFIG"), "private JSON file for bundled authenticated checks")
	checkOnly := flag.Bool("check-only", false, "print authenticated check results without reporting to panel")
	flag.Parse()
	if *checkOnly {
		if *host == "" {
			return errors.New("--host is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		defer cancel()
		checks, e := authenticatedChecks(ctx, *host, *configPath, *runner)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(checks)
	}
	if *configPath != "" && *runner != "" {
		return errors.New("--protocol-config and --client-check are mutually exclusive")
	}
	if *configPath != "" {
		if _, e := protocolprobe.LoadConfig(*configPath); e != nil {
			return e
		}
	}

	u, e := url.Parse(*panel)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("--panel must be an HTTPS URL")
	}
	if *host == "" || *node == "" || *location == "" || os.Getenv("TGWP_PROBE_TOKEN") == "" {
		return errors.New("--host, --node, --location and TGWP_PROBE_TOKEN are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := reliability.ProbeReport{NodeID: *node, Location: *location, FakeTLS: reliability.ProbeCheck{Status: "not_run"}, WEB: reliability.ProbeCheck{Status: "not_run"}}
	start := time.Now()
	dial := tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second}, Config: &tls.Config{ServerName: *host, MinVersion: tls.VersionTLS12}}
	conn, e := dial.DialContext(ctx, "tcp", net.JoinHostPort(*host, "443"))
	p.TLS = reliability.ProbeCheck{Status: "failed", LatencyMS: time.Since(start).Milliseconds()}
	if e == nil {
		_ = conn.Close()
		p.TLS.Status = "ok"
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	start = time.Now()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+*host+"/", nil)
	if e != nil {
		return errors.New("invalid host")
	}
	resp, e := client.Do(req)
	p.HTTP = reliability.ProbeCheck{Status: "failed", LatencyMS: time.Since(start).Milliseconds()}
	if e == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode == 200 {
			p.HTTP.Status = "ok"
		}
	}
	checks, e := authenticatedChecks(ctx, *host, *configPath, *runner)
	if e != nil {
		return e
	}
	p.FakeTLS = checks.FakeTLS
	p.WEB = checks.WEB

	p.At = time.Now().UTC()
	if e = p.Validate(p.At); e != nil {
		return e
	}
	raw, _ := json.Marshal(p)
	req, e = http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(*panel, "/")+"/api/v1/probes/report", bytes.NewReader(raw))
	if e != nil {
		return errors.New("invalid panel URL")
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("TGWP_PROBE_TOKEN"))
	req.Header.Set("Content-Type", "application/json")
	resp, e = client.Do(req)
	if e != nil {
		return errors.New("probe delivery failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 202 {
		return fmt.Errorf("probe delivery returned HTTP %d", resp.StatusCode)
	}
	fmt.Println("probe report accepted")
	return nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, errors.New("client check output exceeded 4096 bytes")
	}
	return b.Buffer.Write(p)
}
