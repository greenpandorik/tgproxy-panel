package subpage

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/subscription"
)

func fakePanel(t *testing.T, down *atomic.Bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/subpage/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/api/v1/subpage/pages/"):
			calls.Add(1)
			d := subscription.PageData{Settings: subscription.DefaultSettings(), Branding: subscription.Branding{PanelName: "Demo", Theme: "dark"}}
			if strings.HasSuffix(r.URL.Path, "/off") {
				d.State = "disabled"
			} else {
				d.Locations = []keys.NodeLinks{keys.LinksFor(keys.LinkTarget{
					NodeID: uuid.New(), NodeName: "Amsterdam", Hostname: "ams1.example.net", Engine: db.NodeEngineTelemt,
					TLSDomain: "ams1.example.net", ClassicPort: 8443,
				}, "0123456789abcdef0123456789abcdef")}
			}
			_ = json.NewEncoder(w).Encode(d)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func get(t *testing.T, h http.Handler, path string, header map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

func TestServesPagesAndKeepsThemWhenThePanelIsDown(t *testing.T) {
	var down atomic.Bool
	var calls atomic.Int32
	panel := fakePanel(t, &down, &calls)
	defer panel.Close()
	s := New(Config{PanelURL: panel.URL, Token: "good-token", Version: "test"}, slog.New(slog.DiscardHandler))
	h := s.Handler()

	code, body := get(t, h, "/s/abc", nil)
	if code != 200 || !strings.Contains(body, "Amsterdam") || strings.Contains(body, panel.URL) {
		t.Fatalf("page %d, panel address must never appear on the page", code)
	}
	if code, body := get(t, h, "/s/abc.json", nil); code != 200 || !strings.Contains(body, `"panel_name":"Demo"`) {
		t.Fatalf("json %d %s", code, body)
	}
	if calls.Load() != 1 {
		t.Fatalf("a fresh copy should be reused, panel asked %d times", calls.Load())
	}

	down.Store(true)
	s.mu.Lock()
	e := s.cache["abc"]
	e.fetched = time.Now().Add(-time.Hour)
	s.cache["abc"] = e
	s.mu.Unlock()
	if code, body := get(t, h, "/s/abc", nil); code != 200 || !strings.Contains(body, "Amsterdam") {
		t.Fatalf("with the panel down a saved copy should be served, got %d", code)
	}
	if code, _ := get(t, h, "/s/never-seen", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("an unknown link with the panel down: %d", code)
	}

	down.Store(false)
	if code, body := get(t, h, "/s/off", nil); code != 403 || !strings.Contains(body, "временно выключен") {
		t.Fatalf("disabled page %d", code)
	}
}

func TestRefusesAWrongTokenAndLimitsVisitors(t *testing.T) {
	var down atomic.Bool
	var calls atomic.Int32
	panel := fakePanel(t, &down, &calls)
	defer panel.Close()
	bad := New(Config{PanelURL: panel.URL, Token: "wrong"}, slog.New(slog.DiscardHandler))
	if code, _ := get(t, bad.Handler(), "/s/abc", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("a wrong token must not show pages: %d", code)
	}
	if err := bad.heartbeat(t.Context()); err != errUnauthorized {
		t.Fatalf("heartbeat with a wrong token: %v", err)
	}

	s := New(Config{PanelURL: panel.URL, Token: "good-token"}, slog.New(slog.DiscardHandler))
	h := s.Handler()
	for i := 0; i < requestsPerMin; i++ {
		get(t, h, "/s/abc", nil)
	}
	if code, _ := get(t, h, "/s/abc", nil); code != http.StatusTooManyRequests {
		t.Fatalf("the %dth request in a minute should be refused: %d", requestsPerMin+1, code)
	}
	if code, _ := get(t, h, "/", nil); code != 404 {
		t.Fatalf("root %d", code)
	}
	if code, body := get(t, h, "/robots.txt", nil); code != 200 || !strings.Contains(body, "Disallow: /") {
		t.Fatalf("robots %d", code)
	}
}

func TestClientIPTrustsOnlyAPrivateProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "172.18.0.5:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("behind the service's own proxy: %s", got)
	}
	r.RemoteAddr = "198.51.100.1:5000"
	if got := clientIP(r); got != "198.51.100.1" {
		t.Fatalf("a public peer must not choose its address: %s", got)
	}
}

func TestConfigNeedsPanelAndToken(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if _, err := ConfigFromEnv(getenv, "v"); err == nil {
		t.Fatal("empty config accepted")
	}
	env["SUBPAGE_PANEL_URL"] = "https://panel.example.com/"
	if _, err := ConfigFromEnv(getenv, "v"); err == nil {
		t.Fatal("missing token accepted")
	}
	env["SUBPAGE_TOKEN"] = "t"
	c, err := ConfigFromEnv(getenv, "v")
	if err != nil || c.PanelURL != "https://panel.example.com" || c.Listen != ":8080" {
		t.Fatalf("config %+v %v", c, err)
	}
}
