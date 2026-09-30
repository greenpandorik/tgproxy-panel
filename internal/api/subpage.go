package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/subscription"
	"tgwebproxy/internal/version"
)

const (
	settingSubscriptionService       = "subscription_service"
	settingSubscriptionServiceStatus = "subscription_service_status"
	// serviceOnlineWindow is how long after its last heartbeat a page service counts as online.
	serviceOnlineWindow = 3 * time.Minute
	serviceCacheTTL     = 10 * time.Second
)

// subscriptionService says where subscription pages live when not on the panel's own domain.
type subscriptionService struct {
	PublicURL      string     `json:"public_url"`
	HideOnPanel    bool       `json:"hide_on_panel"`
	TokenHash      string     `json:"token_hash,omitempty"`
	TokenCreatedAt *time.Time `json:"token_created_at,omitempty"`
}

type subscriptionServiceStatus struct {
	LastSeenAt *time.Time `json:"last_seen_at"`
	Version    string     `json:"version"`
	Address    string     `json:"address"`
}

type serviceCache struct {
	mu      sync.Mutex
	value   subscriptionService
	expires time.Time
}

func (s *Server) serviceSettings(ctx context.Context) subscriptionService {
	s.svcCache.mu.Lock()
	defer s.svcCache.mu.Unlock()
	if time.Now().Before(s.svcCache.expires) {
		return s.svcCache.value
	}
	var out subscriptionService
	if raw, err := s.store.Q.GetSetting(ctx, settingSubscriptionService); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	s.svcCache.value, s.svcCache.expires = out, time.Now().Add(serviceCacheTTL)
	return out
}

func (s *Server) saveServiceSettings(ctx context.Context, v subscriptionService) error {
	raw, _ := json.Marshal(v)
	if err := s.store.Q.UpsertSetting(ctx, db.UpsertSettingParams{Key: settingSubscriptionService, Value: raw}); err != nil {
		return err
	}
	s.svcCache.mu.Lock()
	s.svcCache.expires = time.Time{}
	s.svcCache.mu.Unlock()
	return nil
}

// subscriptionBase is the scheme and host subscription links are built on.
func (s *Server) subscriptionBase(ctx context.Context) string {
	if u := s.serviceSettings(ctx).PublicURL; u != "" {
		return u
	}
	return s.cfg.PublicURL
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func requestHost(r *http.Request) string { return strings.ToLower(r.Host) }

// normalizePublicURL accepts "sub.example.com" or "https://sub.example.com/" and returns
// "https://sub.example.com"; a path, query or fragment is refused.
func normalizePublicURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", false
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), true
}

// onSubscriptionHost reports whether the request came in on the separate subscription domain.
func (s *Server) onSubscriptionHost(r *http.Request) bool {
	svc := s.serviceSettings(r.Context())
	if svc.PublicURL == "" {
		return false
	}
	h := hostOf(svc.PublicURL)
	return h != "" && h != hostOf(s.cfg.PublicURL) && requestHost(r) == h
}

// subscriptionHostGuard keeps the panel itself off the subscription domain when both share a server.
func (s *Server) subscriptionHostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.onSubscriptionHost(r) && !strings.HasPrefix(r.URL.Path, "/s/") && r.URL.Path != "/healthz" {
			if r.URL.Path == "/robots.txt" {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
				return
			}
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// movedSubscription answers a /s/ request on the panel's domain once pages live elsewhere:
// a redirect to the same path there, or not found when the panel should not show them at all.
func (s *Server) movedSubscription(w http.ResponseWriter, r *http.Request, asJSON bool) bool {
	svc := s.serviceSettings(r.Context())
	if svc.PublicURL == "" || s.onSubscriptionHost(r) {
		return false
	}
	if !svc.HideOnPanel {
		http.Redirect(w, r, svc.PublicURL+r.URL.RequestURI(), http.StatusFound)
		return true
	}
	subscription.SecurityHeaders(w)
	d := subscription.PageData{State: "not_found", Settings: s.subscriptionSettings(r.Context()), Branding: s.subscriptionBranding(r.Context())}
	if asJSON {
		_ = subscription.ServeJSON(w, d)
	} else {
		_ = subscription.ServePage(w, r, d)
	}
	return true
}

func (s *Server) serviceTokenOK(r *http.Request) bool {
	svc := s.serviceSettings(r.Context())
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || svc.TokenHash == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(crypto.HashToken(token)), []byte(svc.TokenHash)) == 1
}

// handleSubpageData gives a page service what one subscription link shows.
func (s *Server) handleSubpageData(w http.ResponseWriter, r *http.Request) {
	if !s.serviceTokenOK(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "service token required", nil)
		return
	}
	d, err := s.subscriptionData(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		s.log.Error("subpage: load page", "err", err)
		internal(w)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) handleSubpageHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.serviceTokenOK(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "service token required", nil)
		return
	}
	var in struct {
		Version string `json:"version"`
	}
	_ = decodeJSON(r, &in)
	now := time.Now()
	raw, _ := json.Marshal(subscriptionServiceStatus{LastSeenAt: &now, Version: in.Version, Address: ipFrom(r.Context())})
	if err := s.store.Q.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: settingSubscriptionServiceStatus, Value: raw}); err != nil {
		s.log.Error("subpage: heartbeat", "err", err)
		internal(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type serviceJSON struct {
	PublicURL      string                    `json:"public_url"`
	HideOnPanel    bool                      `json:"hide_on_panel"`
	TokenSet       bool                      `json:"token_set"`
	TokenCreatedAt *time.Time                `json:"token_created_at"`
	PanelURL       string                    `json:"panel_url"`
	Version        string                    `json:"version"`
	Status         subscriptionServiceStatus `json:"status"`
	Online         bool                      `json:"online"`
}

func (s *Server) serviceView(ctx context.Context) serviceJSON {
	svc := s.serviceSettings(ctx)
	var st subscriptionServiceStatus
	if raw, err := s.store.Q.GetSetting(ctx, settingSubscriptionServiceStatus); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	return serviceJSON{
		PublicURL: svc.PublicURL, HideOnPanel: svc.HideOnPanel, TokenSet: svc.TokenHash != "", TokenCreatedAt: svc.TokenCreatedAt,
		PanelURL: s.cfg.PublicURL, Version: version.Version, Status: st,
		Online: svc.TokenHash != "" && st.LastSeenAt != nil && time.Since(*st.LastSeenAt) < serviceOnlineWindow,
	}
}

func (s *Server) handleGetSubscriptionService(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.serviceView(r.Context()))
}

func (s *Server) handlePutSubscriptionService(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PublicURL   string `json:"public_url"`
		HideOnPanel bool   `json:"hide_on_panel"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	u, ok := normalizePublicURL(in.PublicURL)
	if !ok {
		validation(w, map[string]string{"public_url": "a domain like sub.example.com, without a path"})
		return
	}
	if u != "" && hostOf(u) == hostOf(s.cfg.PublicURL) {
		validation(w, map[string]string{"public_url": "must differ from the panel's own domain"})
		return
	}
	svc := s.serviceSettings(r.Context())
	svc.PublicURL, svc.HideOnPanel = u, in.HideOnPanel && u != ""
	if err := s.saveServiceSettings(r.Context(), svc); err != nil {
		s.log.Error("subscription service: save", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "settings.subscription_service", "settings", settingSubscriptionService, map[string]any{"public_url": u, "hide_on_panel": svc.HideOnPanel})
	writeJSON(w, 200, s.serviceView(r.Context()))
}

// subpageInstallCommand is the one line that sets up a page service on another server.
func (s *Server) subpageInstallCommand(publicURL, token string) string {
	domain := hostOf(publicURL)
	if domain == "" {
		domain = "sub.example.com"
	}
	return "curl -fsSL https://raw.githubusercontent.com/greenpandorik/tgproxy-panel/main/install.sh | sudo bash -s -- --subpage" +
		" --domain " + domain + " --panel-url " + s.cfg.PublicURL + " --token " + token + " --version " + version.Version
}

func (s *Server) handleIssueServiceToken(w http.ResponseWriter, r *http.Request) {
	token, err := crypto.NewToken(32)
	if err != nil {
		internal(w)
		return
	}
	svc := s.serviceSettings(r.Context())
	now := time.Now()
	svc.TokenHash, svc.TokenCreatedAt = crypto.HashToken(token), &now
	if err := s.saveServiceSettings(r.Context(), svc); err != nil {
		s.log.Error("subscription service: token", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "settings.subscription_service_token", "settings", settingSubscriptionService, nil)
	writeJSON(w, 200, map[string]string{"token": token, "command": s.subpageInstallCommand(svc.PublicURL, token)})
}

func (s *Server) handleRevokeServiceToken(w http.ResponseWriter, r *http.Request) {
	svc := s.serviceSettings(r.Context())
	svc.TokenHash, svc.TokenCreatedAt = "", nil
	if err := s.saveServiceSettings(r.Context(), svc); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "settings.subscription_service_token_revoke", "settings", settingSubscriptionService, nil)
	w.WriteHeader(http.StatusNoContent)
}
