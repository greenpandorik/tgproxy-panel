package protocolprobe

import (
	"context"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in test runs inside the isolated real-engine bench. Its TLS terminator is
// local and test-only because the bench intentionally ships without Caddy.
func TestRealTelemtAuthenticatedExchange(t *testing.T) {
	path := os.Getenv("TGWP_PROTOCOL_E2E_CONFIG")
	if path == "" {
		t.Skip("isolated real telemt config not provided")
	}
	cfg, e := LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	if cfg.FakeTLS == nil || cfg.WEB == nil {
		t.Fatal("both test transports required")
	}
	result := Check(t.Context(), "127.0.0.1", Config{FakeTLS: cfg.FakeTLS})
	if result.FakeTLS.Status != "ok" {
		t.Fatalf("real FakeTLS: %+v", result)
	}
	upstream, _ := url.Parse("http://127.0.0.1:18080")
	proxy := &httputil.ReverseProxy{}
	proxy.Rewrite = func(r *httputil.ProxyRequest) {
		r.SetURL(upstream)
		r.Out.Host = "fakenode-telemt.local"
		r.Out.Header.Set("X-Forwarded-For", "127.0.0.1")
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		t.Logf("WEB boundary %s HTTP %d", r.Request.URL.Path, r.StatusCode)
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { w.WriteHeader(502) }
	server := httptest.NewTLSServer(proxy)
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.ServerName = "example.com"
	address := server.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	tcfg := *cfg.WEB
	tcfg.Port = server.Listener.Addr().(*net.TCPAddr).Port
	secret, _ := hex.DecodeString(tcfg.Secret)
	x, request, e := newExchange(secret, 2)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if e = probeWEB(ctx, "fakenode-telemt.local", tcfg, secret, request, x, client); e != nil {
		t.Fatal("real WEB protocol exchange failed")
	}
	t.Log("real FakeTLS and WEB returned nonce-matched resPQ")
	t.Run("WrongSecretsRejected", func(t *testing.T) {
		wrongFake := *cfg.FakeTLS
		wrongFake.Secret = strings.Repeat("ff", 16)
		child, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		failed := Check(child, "127.0.0.1", Config{FakeTLS: &wrongFake})
		if failed.FakeTLS.Status != "failed" {
			t.Fatal("real FakeTLS accepted wrong key")
		}
		wrongWeb := tcfg
		wrongWeb.Secret = strings.Repeat("ff", 16)
		wrongSecret, _ := hex.DecodeString(wrongWeb.Secret)
		exchange, payload, e := newExchange(wrongSecret, 2)
		if e != nil {
			t.Fatal(e)
		}
		childWeb, cancelWeb := context.WithTimeout(t.Context(), time.Second)
		defer cancelWeb()
		if probeWEB(childWeb, "fakenode-telemt.local", wrongWeb, wrongSecret, payload, exchange, client) == nil {
			t.Fatal("real WEB accepted wrong key")
		}
	})
}
