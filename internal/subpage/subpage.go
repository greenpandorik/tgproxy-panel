// Package subpage is the stand-alone subscription page service: it serves /s/<token>
// on its own domain and asks the panel for what each link shows.
package subpage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tgwebproxy/internal/subscription"
)

const (
	freshFor        = 30 * time.Second
	staleFor        = 6 * time.Hour
	maxCached       = 10000
	requestsPerMin  = 60
	heartbeatEvery  = time.Minute
	panelTimeout    = 8 * time.Second
	maxPanelPayload = 4 << 20
)

type Config struct {
	PanelURL string
	Token    string
	Listen   string
	Version  string
}

// ConfigFromEnv reads SUBPAGE_PANEL_URL, SUBPAGE_TOKEN and SUBPAGE_LISTEN.
func ConfigFromEnv(getenv func(string) string, ver string) (Config, error) {
	c := Config{
		PanelURL: strings.TrimRight(strings.TrimSpace(getenv("SUBPAGE_PANEL_URL")), "/"),
		Token:    strings.TrimSpace(getenv("SUBPAGE_TOKEN")),
		Listen:   strings.TrimSpace(getenv("SUBPAGE_LISTEN")),
		Version:  ver,
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	u, err := url.Parse(c.PanelURL)
	if c.PanelURL == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return c, errors.New("SUBPAGE_PANEL_URL must be the panel address, like https://panel.example.com")
	}
	if c.Token == "" {
		return c, errors.New("SUBPAGE_TOKEN is required: take it from the panel, Subscription → Service")
	}
	return c, nil
}

type entry struct {
	data    subscription.PageData
	fetched time.Time
}

type Service struct {
	cfg     Config
	log     *slog.Logger
	client  *http.Client
	mu      sync.Mutex
	cache   map[string]entry
	limits  map[string]int
	window  time.Time
	reached atomic.Int64
}

func New(cfg Config, log *slog.Logger) *Service {
	return &Service{
		cfg: cfg, log: log, client: &http.Client{Timeout: panelTimeout},
		cache: map[string]entry{}, limits: map[string]int{},
	}
}

var errUnauthorized = errors.New("the panel refused the service token")

func (s *Service) fetch(ctx context.Context, token string) (subscription.PageData, error) {
	var d subscription.PageData
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.PanelURL+"/api/v1/subpage/pages/"+url.PathEscape(token), nil)
	if err != nil {
		return d, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	resp, err := s.client.Do(req)
	if err != nil {
		return d, err
	}
	defer resp.Body.Close() //nolint:errcheck
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return d, errUnauthorized
	case resp.StatusCode != http.StatusOK:
		return d, fmt.Errorf("panel answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPanelPayload)).Decode(&d); err != nil {
		return d, err
	}
	s.reached.Store(time.Now().Unix())
	return d, nil
}

// data returns what a token shows, from the cache while it is fresh, and from a stale copy
// when the panel cannot be reached.
func (s *Service) data(ctx context.Context, token string) (subscription.PageData, error) {
	s.mu.Lock()
	e, ok := s.cache[token]
	s.mu.Unlock()
	if ok && time.Since(e.fetched) < freshFor {
		return e.data, nil
	}
	d, err := s.fetch(ctx, token)
	if err != nil {
		if ok && time.Since(e.fetched) < staleFor {
			s.log.Warn("panel unreachable, serving a saved copy", "err", err)
			return e.data, nil
		}
		return d, err
	}
	s.mu.Lock()
	if len(s.cache) >= maxCached {
		s.evict()
	}
	s.cache[token] = entry{data: d, fetched: time.Now()}
	s.mu.Unlock()
	return d, nil
}

// evict drops copies too old to serve, and the older half if that is not enough.
func (s *Service) evict() {
	for k, e := range s.cache {
		if time.Since(e.fetched) > staleFor {
			delete(s.cache, k)
		}
	}
	if len(s.cache) < maxCached {
		return
	}
	cut := time.Now().Add(-freshFor)
	for k, e := range s.cache {
		if e.fetched.Before(cut) || len(s.cache) > maxCached/2 {
			delete(s.cache, k)
		}
	}
}

func (s *Service) allow(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.window) >= time.Minute {
		s.window, s.limits = now, map[string]int{}
	}
	s.limits[ip]++
	return s.limits[ip] <= requestsPerMin
}

// clientIP trusts X-Forwarded-For only from a proxy on a private or loopback address,
// which is where the service's own Caddy sits.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
			last := values[len(values)-1]
			if i := strings.LastIndex(last, ","); i >= 0 {
				last = last[i+1:]
			}
			if ip := net.ParseIP(strings.TrimSpace(last)); ip != nil {
				return ip.String()
			}
		}
	}
	return host
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
	})
	mux.HandleFunc("GET /s/{token}", s.handleLink)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	return mux
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	last := s.reached.Load()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "version": s.cfg.Version, "panel_reachable": last > 0 && time.Since(time.Unix(last, 0)) < 3*heartbeatEvery,
	})
}

func (s *Service) handleLink(w http.ResponseWriter, r *http.Request) {
	subscription.SecurityHeaders(w)
	token := r.PathValue("token")
	asJSON := strings.HasSuffix(token, ".json")
	token = strings.TrimSuffix(token, ".json")
	if !s.allow(clientIP(r)) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("too many requests\n"))
		return
	}
	d, err := s.data(r.Context(), token)
	if err != nil {
		s.log.Error("page data", "err", err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("the page is temporarily unavailable, try again in a minute\n"))
		return
	}
	if asJSON {
		err = subscription.ServeJSON(w, d)
	} else {
		err = subscription.ServePage(w, r, d)
	}
	if err != nil {
		s.log.Error("render", "err", err)
	}
}

func (s *Service) heartbeat(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"version": s.cfg.Version})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.PanelURL+"/api/v1/subpage/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		s.reached.Store(time.Now().Unix())
		return nil
	case http.StatusUnauthorized:
		return errUnauthorized
	}
	return fmt.Errorf("panel answered %d", resp.StatusCode)
}

func (s *Service) keepInTouch(ctx context.Context) {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		if err := s.heartbeat(ctx); err != nil {
			s.log.Warn("cannot reach the panel", "panel", s.cfg.PanelURL, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Run serves pages until ctx ends.
func Run(ctx context.Context, cfg Config, log *slog.Logger) error {
	s := New(cfg, log)
	srv := &http.Server{Addr: cfg.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go s.keepInTouch(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("subscription page service", "listen", cfg.Listen, "panel", cfg.PanelURL, "version", cfg.Version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
