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

// subscriptionQRSize is the edge, in pixels, of the QR code embedded in the
// create response - a browser-sized code the operator can screenshot and hand
// to whoever owns the key, without a second round trip to fetch it.
const subscriptionQRSize = 256

// hasActiveSubscription reports whether keyID currently has a live (unrevoked)
// subscription token, for the key JSON's subscription_active flag.
func (s *Server) hasActiveSubscription(ctx context.Context, keyID uuid.UUID) bool {
	_, err := s.store.Q.GetKeySubscription(ctx, keyID)
	return err == nil
}

// handleCreateSubscription mints a fresh subscription token for the key, revoking
// any token the key already had (rotate). The plaintext token exists only in this
// response's URL - the database only ever sees its sha256 hash - so this is the
// one and only time the URL can be produced; there is no "show it again" path.
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
		// Re-read the key inside the transaction: the loadKey check above ran before
		// this transaction started, and a concurrent revoke in between must not be
		// able to mint a token for a key that turns out to already be dead.
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

// handleRevokeSubscription revokes every live token for the key. Idempotent: a key
// with no active token still returns 204.
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

// subscriptionLookup is the shared 404/410 gate for both public /s/ routes: an
// unknown or already-revoked token is indistinguishable from the outside (404),
// and a token that is still valid but whose key has since been revoked is 410 -
// the link existed, it just does not work anymore. status is 0 on success; on
// failure it is http.StatusNotFound or http.StatusGone and the caller is
// responsible for writing the response in whatever form suits its route (JSON
// for .json, a branded HTML page for the human-facing one) - this function
// never writes to w itself.
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

// subscriptionSecurityHeaders locks the public page down to exactly what it needs:
// never cached or indexed, never framed, and (CSP) unable to load or run anything
// beyond its own inline style/script - it never reveals label/owner/note, so there
// is nothing here worth an external request being able to reach.
//
// frame-ancestors 'none' is what stops the page being embedded in someone else's
// site: the connection details and the copy buttons are the whole content, and a
// page that can be framed can be framed invisibly under something else's UI.
// X-Frame-Options is set globally (server.go) for browsers that predate it.
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

// writeSubscriptionErrorPage renders the minimal branded HTML page for the
// human-facing /s/{token} route's 404 (unknown/revoked token) and 410 (token
// still valid, key revoked) cases - a bare JSON error body reads like a
// broken link to someone who followed a QR code, so the HTML route gets a
// small page instead. The .json twin keeps returning JSON (see
// handleSubscriptionJSON) for API clients that parse it.
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

// handleSubscriptionJSON serves the machine-readable twin of the public page, for
// clients that would rather parse than scrape HTML.
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
		// tme/tg stay the WEB link every client already reads; links[] carries
		// every kind the node offers, WEB first.
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

// linkKindLabel names a link kind for the public page. Unknown kinds fall back
// to the raw value rather than rendering an empty heading.
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

// subscriptionPage assembles the subscription.Page for key from the active branding
// profile and the key's node links/QR codes. Branding lookup failures degrade to
// sane defaults rather than failing the whole page - a subscription link must keep
// working even if branding is misconfigured.
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
