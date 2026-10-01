package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
		if p.FaviconPath != "" {
			file := filepath.Join(s.cfg.DataDir, "branding", p.ID.String(), filepath.Base(p.FaviconPath))
			if st, err := os.Stat(file); err == nil && st.Size() <= subscription.MaxFaviconBytes {
				if data, err := os.ReadFile(file); err == nil {
					b.FaviconDataURI = subscription.FaviconDataURI(brandingAssetContentTypes[strings.ToLower(filepath.Ext(file))], data)
				}
			}
		}
	}
	b.PrimaryColor, b.AccentColor = branding.ThemeColors(b.PrimaryColor, b.AccentColor, b.Theme)
	b.PrimaryInk = branding.Foreground(b.PrimaryColor)
	return b
}

// handleSubscriptionPreview renders the page for the servers the panel has, with a secret that opens nothing.
func (s *Server) handleSubscriptionPreview(w http.ResponseWriter, r *http.Request) {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		forbidden(w)
		return
	}
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
	lang := subscription.PageLang(q.Get("lang"), settings.Language, r.Header.Get("Accept-Language"))
	if l := q.Get("language"); q.Get("lang") == "" && (l == "ru" || l == "en") {
		lang = alerttext.Lang(l)
	}
	page, err := subscription.Build(subscription.Input{
		Settings: settings, Lang: lang, Locations: locations,
		Branding: s.subscriptionBranding(ctx), QRSize: subscriptionQRSize,
	})
	if err != nil {
		internal(w)
		return
	}
	page.Preview = true
	var buf bytes.Buffer
	if err := subscription.Render(&buf, page); err != nil {
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) hidesEveryServer(ctx context.Context, hidden []string) bool {
	if len(hidden) == 0 {
		return false
	}
	nodes, err := s.store.Q.ListNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		if !(subscription.Settings{HiddenNodes: hidden}).Hides(n.ID.String()) {
			return false
		}
	}
	return true
}
