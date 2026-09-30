package subscription

import (
	"encoding/json"
	"net/http"
	"time"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/keys"
)

// QRSize is the edge, in pixels, of the QR codes on the page.
const QRSize = 256

// PageData is everything one subscription request needs, whether the panel answers it
// or a separate page service does. State is empty for a link that opens, otherwise
// not_found, revoked, disabled or expired.
type PageData struct {
	State     string           `json:"state"`
	Settings  Settings         `json:"settings"`
	Branding  Branding         `json:"branding"`
	ExpiresAt *time.Time       `json:"expires_at,omitempty"`
	Locations []keys.NodeLinks `json:"locations,omitempty"`
}

type failure struct {
	status  int
	message string
}

var failures = map[string]failure{
	"not_found": {http.StatusNotFound, "subpage.error_not_found"},
	"revoked":   {http.StatusGone, "subpage.error_gone"},
	"disabled":  {http.StatusForbidden, "subpage.error_disabled"},
	"expired":   {http.StatusGone, "subpage.error_expired"},
}

func failureOf(state string) failure {
	if f, ok := failures[state]; ok {
		return f
	}
	return failures["not_found"]
}

// SecurityHeaders are the headers every subscription response carries.
func SecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'none'")
}

// ServePage answers a request for the human-facing page.
func ServePage(w http.ResponseWriter, r *http.Request, d PageData) error {
	lang := PageLang(r.URL.Query().Get("lang"), d.Settings.Language, r.Header.Get("Accept-Language"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if d.State != "" {
		f := failureOf(d.State)
		w.WriteHeader(f.status)
		return RenderError(w, ErrorPage{
			Lang: string(lang), PanelName: d.Branding.PanelName, Theme: d.Branding.Theme,
			Message: alerttext.Default().T(lang, f.message, nil),
		})
	}
	platform := ParsePlatform(d.Settings.Platform)
	if platform == "" {
		platform = DetectPlatform(r.UserAgent())
	}
	page, err := Build(Input{
		Settings: d.Settings, Lang: lang, Platform: platform, Locations: d.Locations,
		ExpiresAt: d.ExpiresAt, Branding: d.Branding, QRSize: QRSize, PageURL: pageURL(r),
	})
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		return err
	}
	return Render(w, page)
}

// pageURL is the address the visitor opened, without the query.
func pageURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto != "https" && proto != "http" {
		proto = "http"
		if r.TLS != nil {
			proto = "https"
		}
	}
	return proto + "://" + r.Host + r.URL.EscapedPath()
}

// ServeJSON answers a request for the machine-readable view of the same links.
func ServeJSON(w http.ResponseWriter, d PageData) error {
	w.Header().Set("Content-Type", "application/json")
	if d.State != "" {
		f := failureOf(d.State)
		code, msg := d.State, "access is "+d.State
		switch d.State {
		case "not_found":
			msg = "not found"
		case "revoked":
			code, msg = "gone", "key revoked"
		}
		w.WriteHeader(f.status)
		return json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": msg}})
	}
	links := VisibleLinks(d.Settings, d.Locations)
	locations := make([]map[string]any, 0, len(links))
	for _, l := range links {
		loc := map[string]any{"name": l.NodeName, "hostname": l.Hostname, "links": l.Links}
		if len(l.Links) > 0 {
			loc["tme"], loc["tg"] = l.Links[0].TMe, l.Links[0].Tg
		}
		locations = append(locations, loc)
	}
	return json.NewEncoder(w).Encode(map[string]any{"panel_name": d.Branding.PanelName, "locations": locations})
}

// VisibleLinks keeps the servers and link kinds the settings show.
func VisibleLinks(settings Settings, links []keys.NodeLinks) []keys.NodeLinks {
	out := make([]keys.NodeLinks, 0, len(links))
	for _, l := range links {
		if settings.hidden(l.NodeID.String()) {
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

// Hides reports whether the settings keep the server with this id off the page.
func (s Settings) Hides(nodeID string) bool { return s.hidden(nodeID) }
