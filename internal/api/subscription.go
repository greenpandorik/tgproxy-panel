package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/branding"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/qrlink"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/subscription"
)

// subscriptionQRSize is the edge, in pixels, of the QR code embedded in the create response.
const subscriptionQRSize = 256

func (s *Server) hasActiveSubscription(ctx context.Context, keyID uuid.UUID) bool {
	_, err := s.store.Q.GetKeySubscription(ctx, keyID)
	return err == nil
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	token, err := crypto.NewToken(32)
	if err != nil {
		s.log.Error("subscription: generate token", "err", err)
		internal(w)
		return
	}
	hash := crypto.HashToken(token)
	revoked := false
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		current, err := q.GetKey(r.Context(), k.ID)
		if err != nil {
			return err
		}
		if current.Status == db.KeyStatusRevoked {
			revoked = true
			return nil
		}
		if err := q.RevokeSubscriptionTokensForKey(r.Context(), k.ID); err != nil {
			return err
		}
		_, err = q.CreateSubscriptionToken(r.Context(), db.CreateSubscriptionTokenParams{TokenHash: hash, AccessKeyID: k.ID})
		return err
	})
	if err != nil {
		s.log.Error("subscription: create token", "err", err)
		internal(w)
		return
	}
	if revoked {
		conflict(w, "key is revoked")
		return
	}
	url := s.cfg.PublicURL + "/s/" + token
	qr, err := qrlink.DataURI(url, subscriptionQRSize)
	if err != nil {
		s.log.Error("subscription: render qr", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "key.subscription_create", "key", k.ID.String(), map[string]any{"label": k.Label})
	writeJSON(w, 200, map[string]any{"url": url, "qr_data_uri": qr})
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

func (s *Server) subscriptionLookup(r *http.Request, token string) (db.AccessKey, int) {
	sub, err := s.store.Q.GetSubscriptionByHash(r.Context(), crypto.HashToken(token))
	if err != nil || sub.RevokedAt != nil {
		return db.AccessKey{}, http.StatusNotFound
	}
	key, err := s.store.Q.GetKey(r.Context(), sub.AccessKeyID)
	if err != nil {
		return db.AccessKey{}, http.StatusNotFound
	}
	if key.Status == db.KeyStatusRevoked {
		return db.AccessKey{}, http.StatusGone
	}
	return key, 0
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
	key, status := s.subscriptionLookup(r, chi.URLParam(r, "token"))
	if status != 0 {
		s.writeSubscriptionErrorPage(w, r, status)
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

func (s *Server) writeSubscriptionErrorPage(w http.ResponseWriter, r *http.Request, status int) {
	panelName, theme := "TGWebProxy", "dark"
	if b, err := s.store.Q.GetActiveBranding(r.Context()); err == nil {
		if b.PanelName != "" {
			panelName = b.PanelName
		}
		if b.ThemeDefault != "" {
			theme = b.ThemeDefault
		}
	}
	message := "Ссылка не найдена."
	if status == http.StatusGone {
		message = "Эта ссылка больше не действительна."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := subscription.RenderError(w, subscription.ErrorPage{PanelName: panelName, Theme: theme, Message: message}); err != nil {
		s.log.Error("subscription: render error page", "err", err)
	}
}

func (s *Server) handleSubscriptionJSON(w http.ResponseWriter, r *http.Request) {
	subscriptionSecurityHeaders(w)
	if !s.subLimiter.Allow(ipFrom(r.Context())) {
		writeError(w, 429, "rate_limited", "too many requests", nil)
		return
	}
	key, status := s.subscriptionLookup(r, chi.URLParam(r, "token"))
	switch status {
	case http.StatusNotFound:
		notFound(w)
		return
	case http.StatusGone:
		gone(w, "key revoked")
		return
	}
	links, err := s.keys.NodeLinks(r.Context(), key.ID)
	if err != nil {
		s.log.Error("subscription: load links", "err", err)
		internal(w)
		return
	}
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
	panelName := "TGWebProxy"
	if err == nil && b.PanelName != "" {
		panelName = b.PanelName
	}
	writeJSON(w, 200, map[string]any{"panel_name": panelName, "locations": locations})
}

// linkKindLabel names a link kind for the public page.
func linkKindLabel(kind string) string {
	switch kind {
	case keys.LinkWeb:
		return "WEB"
	case keys.LinkTLS:
		return "Fake-TLS"
	default:
		return kind
	}
}

func (s *Server) subscriptionPage(r *http.Request, key db.AccessKey) (subscription.Page, error) {
	links, err := s.keys.NodeLinks(r.Context(), key.ID)
	if err != nil {
		return subscription.Page{}, err
	}
	locations := make([]subscription.Location, 0, len(links))
	for _, l := range links {
		methods := make([]subscription.LocationLink, 0, len(l.Links))
		for _, m := range l.Links {
			qr, err := qrlink.DataURI(m.TMe, subscriptionQRSize)
			if err != nil {
				return subscription.Page{}, err
			}
			methods = append(methods, subscription.LocationLink{Kind: m.Kind, Label: linkKindLabel(m.Kind), TMe: m.TMe, Tg: m.Tg, QRDataURI: qr})
		}
		locations = append(locations, subscription.Location{Name: l.NodeName, Hostname: l.Hostname, Links: methods})
	}
	b, err := s.store.Q.GetActiveBranding(r.Context())
	page := subscription.Page{
		PanelName: branding.DefaultPanelName, PrimaryColor: branding.DefaultPrimaryColor, AccentColor: branding.DefaultAccentColor, Theme: "dark",
		Locations: locations, ClientSupport: clientSupport,
	}
	if err == nil {
		page.PanelName = b.PanelName
		page.PrimaryColor = b.PrimaryColor
		page.AccentColor = b.AccentColor
		page.Theme = b.ThemeDefault
		page.SupportLink = b.SupportLink
		page.FooterText = b.FooterText
	}
	return page, nil
}
