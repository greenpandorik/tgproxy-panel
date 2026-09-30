package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/branding"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/subscription"
)

const settingSubscriptionPage = "subscription_page"

// previewSecret stands in for a key's secret in the settings preview; it opens nothing.
const previewSecret = "00000000000000000000000000000000"

func (s *Server) subscriptionSettings(ctx context.Context) subscription.Settings {
	out := subscription.DefaultSettings()
	if raw, err := s.store.Q.GetSetting(ctx, settingSubscriptionPage); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out.Normalized()
}

func (s *Server) subscriptionBranding(ctx context.Context) subscription.Branding {
	b := subscription.Branding{
		PanelName: branding.DefaultPanelName, PrimaryColor: branding.DefaultPrimaryColor,
		AccentColor: branding.DefaultAccentColor, Theme: "dark",
	}
	if p, err := s.store.Q.GetActiveBranding(ctx); err == nil {
		if p.PanelName != "" {
			b.PanelName = p.PanelName
		}
		b.PrimaryColor, b.AccentColor = p.PrimaryColor, p.AccentColor
		if p.ThemeDefault != "" {
			b.Theme = p.ThemeDefault
		}
		b.SupportLink, b.FooterText = p.SupportLink, p.FooterText
	}
	b.PrimaryColor, b.AccentColor = branding.ThemeColors(b.PrimaryColor, b.AccentColor, b.Theme)
	b.PrimaryInk = branding.Foreground(b.PrimaryColor)
	return b
}

// handleSubscriptionPreview renders the page for the servers the panel has, with a secret that opens nothing.
func (s *Server) handleSubscriptionPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	settings := s.subscriptionSettings(ctx)
	if raw := q.Get("settings"); raw != "" {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			badRequest(w, "settings must be base64 JSON")
			return
		}
		parsed := subscription.DefaultSettings()
		if err := json.Unmarshal(decoded, &parsed); err != nil {
			badRequest(w, "settings must be base64 JSON")
			return
		}
		settings = parsed.Normalized()
		if f := settings.Validate(); len(f) > 0 {
			validation(w, f)
			return
		}
	}
	nodes, err := s.store.Q.ListNodes(ctx)
	if err != nil {
		internal(w)
		return
	}
	locations := make([]keys.NodeLinks, 0, len(nodes))
	for _, n := range nodes {
		locations = append(locations, keys.LinksFor(keys.LinkTarget{
			NodeID: n.ID, NodeName: n.Name, Hostname: n.Hostname, Engine: n.Engine,
			TLSDomain: n.TlsDomain, TLSDomains: n.TlsDomains, ClassicPort: int(n.ClassicPort),
		}, previewSecret))
	}
	platform := subscription.ParsePlatform(q.Get("platform"))
	if platform == "" {
		platform = subscription.Android
	}
	lang := subscription.DetectLang(settings.Language, r.Header.Get("Accept-Language"))
	if l := q.Get("language"); l == "ru" || l == "en" {
		lang = alerttext.Lang(l)
	}
	page, err := subscription.Build(subscription.Input{
		Settings: settings, Lang: lang, Platform: platform, Locations: locations,
		Branding: s.subscriptionBranding(ctx), QRSize: subscriptionQRSize,
	})
	if err != nil {
		internal(w)
		return
	}
	var buf bytes.Buffer
	if err := subscription.Render(&buf, page); err != nil {
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'self'")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) visibleLinks(settings subscription.Settings, links []keys.NodeLinks) []keys.NodeLinks {
	out := make([]keys.NodeLinks, 0, len(links))
	for _, l := range links {
		if settingsHidesNode(settings, l.NodeID) {
			continue
		}
		kept := l
		kept.Links = nil
		tlsSeen := 0
		for _, k := range l.Links {
			switch k.Kind {
			case keys.LinkWeb:
				if !settings.ShowWeb {
					continue
				}
			case keys.LinkTLS:
				tlsSeen++
				if !settings.ShowFakeTLS || (tlsSeen > 1 && !settings.ShowBackupDomains) {
					continue
				}
			}
			kept.Links = append(kept.Links, k)
		}
		if len(kept.Links) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

func settingsHidesNode(settings subscription.Settings, id uuid.UUID) bool {
	for _, h := range settings.HiddenNodes {
		if h == id.String() {
			return true
		}
	}
	return false
}
