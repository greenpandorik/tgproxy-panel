package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/branding"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/qrlink"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/subscription"
)

// subscriptionQRSize is the edge, in pixels, of the QR code embedded in the create response.
const subscriptionQRSize = 256

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	token, err := s.keys.IssueSubscription(r.Context(), k.ID)
	if err != nil {
		s.keysErr(w, err)
		return
	}
	url := s.subscriptionURL(token)
	qr, err := qrlink.DataURI(url, subscriptionQRSize)
	if err != nil {
		s.log.Error("subscription: render qr", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "key.subscription_create", "key", k.ID.String(), map[string]any{"label": k.Label})
	writeJSON(w, 200, map[string]any{"url": url, "qr_data_uri": qr})
}

// handleSubscriptionQR draws the key's current subscription link, or its short address with ?short=1.
func (s *Server) handleSubscriptionQR(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	sub, err := s.store.Q.GetKeySubscription(r.Context(), k.ID)
	if err != nil || k.Status == db.KeyStatusRevoked {
		notFound(w)
		return
	}
	var url string
	if r.URL.Query().Get("short") == "1" && k.SubSlug != nil {
		url = s.subscriptionURL(*k.SubSlug)
	} else if token, ok := s.keys.SubscriptionToken(sub); ok {
		url = s.subscriptionURL(token)
	} else {
		notFound(w)
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 128 || size > 1024 {
		size = 256
	}
	png, err := qrlink.PNG(url, size)
	if err != nil {
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// handleRevokeSubscription revokes every live token for the key.
func (s *Server) handleRevokeSubscription(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	if err := s.store.Q.RevokeSubscriptionTokensForKey(r.Context(), k.ID); err != nil {
		s.log.Error("subscription: revoke", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "key.subscription_revoke", "key", k.ID.String(), map[string]any{"label": k.Label})
	w.WriteHeader(204)
}

// subscriptionLookup finds the key behind a token or short address. A failure comes back as
// the key's state: not_found, revoked, disabled or expired.
func (s *Server) subscriptionLookup(r *http.Request, token string) (db.AccessKey, string) {
	ctx := r.Context()
	var key db.AccessKey
	sub, err := s.store.Q.GetSubscriptionByHash(ctx, crypto.HashToken(token))
	switch {
	case err == nil:
		if sub.RevokedAt != nil {
			return db.AccessKey{}, "not_found"
		}
		if key, err = s.store.Q.GetKey(ctx, sub.AccessKeyID); err != nil {
			return db.AccessKey{}, "not_found"
		}
	case keys.ValidSlug(token):
		if key, err = s.store.Q.GetKeyBySlug(ctx, &token); err != nil {
			return db.AccessKey{}, "not_found"
		}
		if _, err := s.store.Q.GetKeySubscription(ctx, key.ID); err != nil {
			return db.AccessKey{}, "not_found"
		}
	default:
		return db.AccessKey{}, "not_found"
	}
	switch state := keyState(key, time.Now()); state {
	case "revoked", "disabled", "expired":
		return db.AccessKey{}, state
	}
	return key, ""
}

var subscriptionFailures = map[string]struct {
	status  int
	message string
}{
	"not_found": {http.StatusNotFound, "subpage.error_not_found"},
	"revoked":   {http.StatusGone, "subpage.error_gone"},
	"disabled":  {http.StatusForbidden, "subpage.error_disabled"},
	"expired":   {http.StatusGone, "subpage.error_expired"},
}

func subscriptionSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'none'")
}

// handleSubscriptionPage serves the public, human-facing subscription page.
func (s *Server) handleSubscriptionPage(w http.ResponseWriter, r *http.Request) {
	subscriptionSecurityHeaders(w)
	if !s.subLimiter.Allow(ipFrom(r.Context())) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("too many requests\n"))
		return
	}
	key, failure := s.subscriptionLookup(r, chi.URLParam(r, "token"))
	if failure != "" {
		s.writeSubscriptionErrorPage(w, r, failure)
		return
	}
	page, err := s.subscriptionPage(r, key)
	if err != nil {
		s.log.Error("subscription: build page", "err", err)
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := subscription.Render(w, page); err != nil {
		s.log.Error("subscription: render page", "err", err)
	}
}

func (s *Server) writeSubscriptionErrorPage(w http.ResponseWriter, r *http.Request, failure string) {
	b := s.subscriptionBranding(r.Context())
	lang := subscription.PageLang(r.URL.Query().Get("lang"), s.subscriptionSettings(r.Context()).Language, r.Header.Get("Accept-Language"))
	f := subscriptionFailures[failure]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(f.status)
	page := subscription.ErrorPage{Lang: string(lang), PanelName: b.PanelName, Theme: b.Theme, Message: alerttext.Default().T(lang, f.message, nil)}
	if err := subscription.RenderError(w, page); err != nil {
		s.log.Error("subscription: render error page", "err", err)
	}
}

func (s *Server) handleSubscriptionJSON(w http.ResponseWriter, r *http.Request) {
	subscriptionSecurityHeaders(w)
	if !s.subLimiter.Allow(ipFrom(r.Context())) {
		writeError(w, 429, "rate_limited", "too many requests", nil)
		return
	}
	key, failure := s.subscriptionLookup(r, chi.URLParam(r, "token"))
	switch failure {
	case "":
	case "not_found":
		notFound(w)
		return
	case "revoked":
		gone(w, "key revoked")
		return
	default:
		f := subscriptionFailures[failure]
		writeError(w, f.status, failure, "access is "+failure, nil)
		return
	}
	links, err := s.keys.NodeLinks(r.Context(), key.ID)
	if err != nil {
		s.log.Error("subscription: load links", "err", err)
		internal(w)
		return
	}
	links = s.visibleLinks(s.subscriptionSettings(r.Context()), links)
	locations := make([]map[string]any, 0, len(links))
	for _, l := range links {
		// tme/tg stay the WEB link every client already reads; links[] carries every kind the node offers, WEB first.
		loc := map[string]any{"name": l.NodeName, "hostname": l.Hostname, "links": l.Links}
		if len(l.Links) > 0 {
			loc["tme"], loc["tg"] = l.Links[0].TMe, l.Links[0].Tg
		}
		locations = append(locations, loc)
	}
	b, err := s.store.Q.GetActiveBranding(r.Context())
	panelName := branding.DefaultPanelName
	if err == nil && b.PanelName != "" {
		panelName = b.PanelName
	}
	writeJSON(w, 200, map[string]any{"panel_name": panelName, "locations": locations})
}

func (s *Server) subscriptionPage(r *http.Request, key db.AccessKey) (subscription.Page, error) {
	ctx := r.Context()
	links, err := s.keys.NodeLinks(ctx, key.ID)
	if err != nil {
		return subscription.Page{}, err
	}
	settings := s.subscriptionSettings(ctx)
	platform := subscription.ParsePlatform(settings.Platform)
	if platform == "" {
		platform = subscription.DetectPlatform(r.UserAgent())
	}
	return subscription.Build(subscription.Input{
		Settings:  settings,
		Lang:      subscription.PageLang(r.URL.Query().Get("lang"), settings.Language, r.Header.Get("Accept-Language")),
		Platform:  platform,
		Locations: links,
		ExpiresAt: key.ExpiresAt,
		Branding:  s.subscriptionBranding(ctx),
		QRSize:    subscriptionQRSize,
	})
}
