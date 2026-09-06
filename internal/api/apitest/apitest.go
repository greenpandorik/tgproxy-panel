// Package apitest provides an HTTP test harness for the panel API.
package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/updates"
)

type Harness struct {
	T        *testing.T
	Server   *httptest.Server
	Store    *store.Store
	Box      *crypto.Box
	Deps     api.Deps
	Mock     *nodedriver.Mock
	Presence *nodesvc.Presence

	router chi.Router
}

// Option lets later parts inject a NodeDriver or other deps into the harness.
type Option func(*api.Deps)

// WithTOTP turns on FEATURE_TOTP, so the /auth/totp/* routes answer instead of
// returning 404 feature_disabled. Off by default, matching a stock install.
func WithTOTP() Option { return func(d *api.Deps) { d.Cfg.FeatureTOTP = true } }

// WithUpdates turns on the GitHub update check and serves it through c, whose
// BaseURL the test points at an httptest stub. Off by default so no test ever
// reaches api.github.com.
func WithUpdates(c *updates.Checker) Option {
	return func(d *api.Deps) {
		d.Cfg.UpdateCheck = true
		d.Cfg.GitHubRepo = c.Repo
		d.Updates = c
	}
}

func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	st := store.OpenTest(t)
	if err := store.SeedPresets(context.Background(), st, sitekit.Presets()); err != nil {
		t.Fatalf("seed presets: %v", err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	box, _ := crypto.NewBox(1, map[int][]byte{1: key})
	cfg := config.Config{
		PublicURL: "http://panel.test", DataDir: t.TempDir(), NodeDriver: "mock",
		MasterKey: key, MasterKeyVersion: 1, SessionSecret: key, TProxyCommit: config.DefaultTProxyCommit,
		TelemtVersion: config.DefaultTelemtVersion, TelemtSHA256: strings.Repeat("5c", 32),
		ApplyInterval: 45, OfflineAfter: 90,
	}
	log := slog.New(slog.DiscardHandler)
	mock := nodedriver.NewMock()
	presence := nodesvc.NewPresence(st, log)
	deps := api.Deps{Store: st, Box: box, Signer: crypto.NewSigner(key), Log: log, Cfg: cfg, Driver: mock, Presence: presence, Keys: keys.New(st, box)}
	for _, o := range opts {
		o(&deps)
	}
	apiSrv := api.New(deps)
	deps.SiteProvider = apiSrv.NodeSiteFiles
	router := apiSrv.Handler()
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &Harness{T: t, Server: srv, Store: st, Box: box, Deps: deps, Mock: mock, Presence: presence, router: router}
}

// Router exposes the chi router the harness serves, so tests can walk the
// route table (see TestEveryMutatingRouteIsProtected).
func (h *Harness) Router() chi.Router { return h.router }

func (h *Harness) CreateAdmin(username, password, role string) uuid.UUID {
	h.T.Helper()
	hash, err := crypto.HashPassword(password)
	if err != nil {
		h.T.Fatal(err)
	}
	u, err := h.Store.Q.CreateAdmin(context.Background(), db.CreateAdminParams{Username: username, PasswordHash: hash, Role: db.AdminRole(role)})
	if err != nil {
		h.T.Fatal(err)
	}
	return u.ID
}

type Client struct {
	h        *Harness
	http     *http.Client
	SendCSRF bool
	// Headers are added to every request this client sends. Tests that need to
	// exercise proxy-supplied headers (X-Forwarded-For, say) set them here.
	Headers http.Header
}

func (h *Harness) Anonymous() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{h: h, http: &http.Client{Jar: jar}, SendCSRF: true, Headers: http.Header{}}
}

// SetHeader adds a header sent with every subsequent request and returns the
// client, so a header can be attached inline where the client is created.
func (c *Client) SetHeader(key, value string) *Client {
	if c.Headers == nil {
		c.Headers = http.Header{}
	}
	c.Headers.Set(key, value)
	return c
}

func (c *Client) applyHeaders(req *http.Request) {
	for k, vs := range c.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
}

func (h *Harness) Login(username, password string) *Client {
	h.T.Helper()
	c := h.Anonymous()
	resp := c.Post("/api/v1/auth/login", map[string]string{"username": username, "password": password})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		h.T.Fatalf("login failed: %d %s", resp.StatusCode, b)
	}
	return c
}

func (c *Client) do(method, path string, body any) *http.Response {
	c.h.T.Helper()
	var rd io.Reader
	if body != nil {
		if raw, ok := body.([]byte); ok {
			rd = bytes.NewReader(raw)
		} else {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
	}
	req, _ := http.NewRequest(method, c.h.Server.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	c.applyHeaders(req)
	if c.SendCSRF {
		u, _ := req.URL.Parse("/")
		for _, ck := range c.http.Jar.Cookies(u) {
			if ck.Name == "tgwp_csrf" {
				req.Header.Set("X-CSRF-Token", ck.Value)
			}
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.h.T.Fatal(err)
	}
	return resp
}

// PostRaw sends body as-is with an explicit Content-Type, still attaching the CSRF header.
func (c *Client) PostRaw(path, contentType string, body []byte) *http.Response {
	c.h.T.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.h.Server.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	c.applyHeaders(req)
	if c.SendCSRF {
		u, _ := req.URL.Parse("/")
		for _, ck := range c.http.Jar.Cookies(u) {
			if ck.Name == "tgwp_csrf" {
				req.Header.Set("X-CSRF-Token", ck.Value)
			}
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.h.T.Fatal(err)
	}
	return resp
}

// Do issues an arbitrary request; body is JSON-encoded unless it is nil or []byte.
func (c *Client) Do(method, path string, body any) *http.Response { return c.do(method, path, body) }

func (c *Client) Get(p string) *http.Response          { return c.do(http.MethodGet, p, nil) }
func (c *Client) Post(p string, b any) *http.Response  { return c.do(http.MethodPost, p, b) }
func (c *Client) Patch(p string, b any) *http.Response { return c.do(http.MethodPatch, p, b) }
func (c *Client) Put(p string, b any) *http.Response   { return c.do(http.MethodPut, p, b) }
func (c *Client) Delete(p string) *http.Response       { return c.do(http.MethodDelete, p, nil) }

func (c *Client) JSON(resp *http.Response, out any) {
	c.h.T.Helper()
	defer resp.Body.Close() //nolint:errcheck
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, out); err != nil {
		c.h.T.Fatalf("decode %s: %v", b, err)
	}
}
