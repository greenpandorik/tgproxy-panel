package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

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
	url := s.subscriptionURL(r.Context(), token)
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
		url = s.subscriptionURL(r.Context(), *k.SubSlug)
	} else if token, ok := s.keys.SubscriptionToken(sub); ok {
		url = s.subscriptionURL(r.Context(), token)
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

// subscriptionData finds the key behind a token or short address and gathers what its
// page shows. A key that cannot be served comes back with its state and nothing else.
func (s *Server) subscriptionData(ctx context.Context, token string) (subscription.PageData, error) {
	d := subscription.PageData{Settings: s.subscriptionSettings(ctx), Branding: s.subscriptionBranding(ctx)}
	if !subscription.PlausibleToken(token) {
		d.State = "not_found"
		return d, nil
	}
	var key db.AccessKey
	sub, err := s.store.Q.GetSubscriptionByHash(ctx, crypto.HashToken(token))
	switch {
	case err == nil:
		if sub.RevokedAt != nil {
			d.State = "not_found"
			return d, nil
		}
		if key, err = s.store.Q.GetKey(ctx, sub.AccessKeyID); err != nil {
			d.State = "not_found"
			return d, nil
		}
	case keys.ValidSlug(token):
		if key, err = s.store.Q.GetKeyBySlug(ctx, &token); err != nil {
			d.State = "not_found"
			return d, nil
		}
		if _, err := s.store.Q.GetKeySubscription(ctx, key.ID); err != nil {
			d.State = "not_found"
			return d, nil
		}
	default:
		d.State = "not_found"
		return d, nil
	}
	switch state := keyState(key, time.Now()); state {
	case "revoked", "disabled", "expired":
		d.State = state
		return d, nil
	}
	links, err := s.keys.NodeLinks(ctx, key.ID)
	if err != nil {
		return d, err
	}
	d.ExpiresAt, d.Locations = key.ExpiresAt, subscription.VisibleLinks(d.Settings, links)
	return d, nil
}

func (s *Server) subscriptionAllowed(w http.ResponseWriter, r *http.Request, asJSON bool) bool {
	subscription.SecurityHeaders(w)
	ip := ipFrom(r.Context())
	if !s.subMisses.Blocked(subscription.VisitorKey(ip)) && s.subLimiter.Allow(ip) {
		return true
	}
	if asJSON {
		writeError(w, 429, "rate_limited", "too many requests", nil)
		return false
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte("too many requests\n"))
	return false
}

// handleSubscriptionPage serves the public, human-facing subscription page.
func (s *Server) handleSubscriptionPage(w http.ResponseWriter, r *http.Request) {
	if s.movedSubscription(w, r, false) || !s.subscriptionAllowed(w, r, false) {
		return
	}
	d, err := s.subscriptionData(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		s.log.Error("subscription: build page", "err", err)
		internal(w)
		return
	}
	s.countMiss(r, d)
	if err := subscription.ServePage(w, r, d); err != nil {
		s.log.Error("subscription: render page", "err", err)
	}
}

func (s *Server) handleSubscriptionJSON(w http.ResponseWriter, r *http.Request) {
	if s.movedSubscription(w, r, true) || !s.subscriptionAllowed(w, r, true) {
		return
	}
	d, err := s.subscriptionData(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		s.log.Error("subscription: load links", "err", err)
		internal(w)
		return
	}
	s.countMiss(r, d)
	if err := subscription.ServeJSON(w, d); err != nil {
		s.log.Error("subscription: write json", "err", err)
	}
}

// countMiss remembers a link that does not exist, so guessing short addresses soon stops working.
func (s *Server) countMiss(r *http.Request, d subscription.PageData) {
	if d.State == "not_found" {
		s.subMisses.Allow(subscription.VisitorKey(ipFrom(r.Context())))
	}
}
