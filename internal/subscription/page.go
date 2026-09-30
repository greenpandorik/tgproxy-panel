package subscription

import (
	"embed"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/qrlink"
)

//go:embed page.tmpl.html error.tmpl.html
var templateFS embed.FS

func safeURL(s string) template.URL { return template.URL(s) } //nolint:gosec // links are built server-side from stored hostnames and secrets

var storeURLs = map[Platform]string{
	Android: "https://play.google.com/store/apps/details?id=org.telegram.messenger",
	IOS:     "https://apps.apple.com/app/telegram-messenger/id686449807",
	Desktop: "https://desktop.telegram.org/",
}

func storeURL(p Platform) template.URL { return template.URL(storeURLs[p]) } //nolint:gosec // fixed official addresses

var funcs = template.FuncMap{
	"safeURL":  safeURL,
	"storeURL": storeURL,
	"t":        func(string, ...string) string { return "" },
}

var pageTmpl = template.Must(template.New("page.tmpl.html").Funcs(funcs).ParseFS(templateFS, "page.tmpl.html"))

var errorTmpl = template.Must(template.New("error.tmpl.html").Funcs(funcs).ParseFS(templateFS, "error.tmpl.html"))

type Platform string

const (
	Android Platform = "android"
	IOS     Platform = "ios"
	Desktop Platform = "desktop"
)

var platforms = []Platform{Android, IOS, Desktop}

// DetectPlatform guesses the visitor's device from the User-Agent.
func DetectPlatform(userAgent string) Platform {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"), strings.Contains(ua, "ipod"):
		return IOS
	case strings.Contains(ua, "android"):
		return Android
	default:
		return Desktop
	}
}

// ParsePlatform accepts a platform name from a query string; anything else is empty.
func ParsePlatform(s string) Platform {
	for _, p := range platforms {
		if string(p) == s {
			return p
		}
	}
	return ""
}

// PageLang is the visitor's own ?lang= choice when present, else the owner's setting.
func PageLang(query, setting, acceptLanguage string) alerttext.Lang {
	if query == "ru" || query == "en" {
		return alerttext.Lang(query)
	}
	return DetectLang(setting, acceptLanguage)
}

// DetectLang picks the page language: the owner's choice, or the browser's first language.
func DetectLang(setting, acceptLanguage string) alerttext.Lang {
	if setting == "ru" || setting == "en" {
		return alerttext.Lang(setting)
	}
	first, _, _ := strings.Cut(acceptLanguage, ",")
	first = strings.ToLower(strings.TrimSpace(first))
	for _, prefix := range []string{"ru", "uk", "be", "kk"} {
		if strings.HasPrefix(first, prefix) {
			return alerttext.RU
		}
	}
	if first == "" {
		return alerttext.RU
	}
	return alerttext.EN
}

var monthsRU = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

func formatDate(l alerttext.Lang, t time.Time) string {
	if l == alerttext.RU {
		return strconv.Itoa(t.Day()) + " " + monthsRU[t.Month()-1] + " " + strconv.Itoa(t.Year())
	}
	return t.Format("2 January 2006")
}

type Link struct {
	Kind      string
	Label     string
	TMe       string
	Tg        string
	QRDataURI string
}

// Server is one server a key is bound to, with the links the page shows for it.
type Server struct {
	Name     string
	Hostname string
	Links    []Link
	WebOnly  bool
}

// Action is what the "Connect" row offers for one server on one device.
type Action struct {
	Name     string
	Hostname string
	Primary  Link
	Alt      *Link
}

type PlatformView struct {
	ID       Platform
	Selected bool
	Actions  []Action
	HasAlt   bool
}

// Page is everything the template needs to render one subscription page.
type Page struct {
	Lang         string
	Title        string
	Intro        string
	PanelName    string
	PrimaryColor string
	AccentColor  string
	PrimaryInk   string
	Theme        string
	SupportLink  string
	FooterText   string
	ShowStatus   bool
	StatusText   string
	ShowGuide    bool
	ShowQR       bool
	Platform     Platform
	Platforms    []PlatformView
	Servers      []Server
	Preview      bool
	// PageURL is this page's own address; PageQR draws it for opening the page on a phone.
	PageURL string
	PageQR  string
}

// Branding is the part of the active branding profile the page uses.
type Branding struct {
	PanelName    string `json:"panel_name"`
	PrimaryColor string `json:"primary_color"`
	AccentColor  string `json:"accent_color"`
	PrimaryInk   string `json:"primary_ink"`
	Theme        string `json:"theme"`
	SupportLink  string `json:"support_link"`
	FooterText   string `json:"footer_text"`
}

type Input struct {
	Settings  Settings
	Lang      alerttext.Lang
	Platform  Platform
	Locations []keys.NodeLinks
	ExpiresAt *time.Time
	Branding  Branding
	QRSize    int
	PageURL   string
}

// Build turns a key's links into the page, applying the owner's settings.
func Build(in Input) (Page, error) {
	c := alerttext.Default()
	t := func(key string, vars map[string]string) string { return c.T(in.Lang, key, vars) }
	s := in.Settings
	page := Page{
		Lang: string(in.Lang), PanelName: in.Branding.PanelName,
		PrimaryColor: in.Branding.PrimaryColor, AccentColor: in.Branding.AccentColor, PrimaryInk: in.Branding.PrimaryInk,
		Theme: in.Branding.Theme, SupportLink: in.Branding.SupportLink, FooterText: in.Branding.FooterText,
		ShowStatus: s.ShowStatus, ShowGuide: s.ShowGuide, ShowQR: s.ShowQR, Platform: in.Platform,
	}
	page.Title, page.Intro = s.texts(in.Lang)
	if in.PageURL != "" {
		page.PageURL = in.PageURL
		if s.ShowQR {
			qr, err := qrlink.DataURI(in.PageURL, 200)
			if err != nil {
				return Page{}, err
			}
			page.PageQR = qr
		}
	}
	if page.Title == "" {
		page.Title = t("subpage.title_default", nil)
	}
	if page.Intro == "" {
		page.Intro = t("subpage.intro_default", nil)
	}
	if in.ExpiresAt != nil {
		page.StatusText = t("subpage.status_until", map[string]string{"date": formatDate(in.Lang, in.ExpiresAt.Local())})
	} else {
		page.StatusText = t("subpage.status_forever", nil)
	}

	type choice struct {
		web, tls, backup int
	}
	var choices []choice
	for _, loc := range in.Locations {
		if s.hidden(loc.NodeID.String()) {
			continue
		}
		server := Server{Name: loc.NodeName, Hostname: loc.Hostname}
		ch := choice{web: -1, tls: -1, backup: -1}
		tlsSeen := 0
		for _, l := range loc.Links {
			var label string
			switch l.Kind {
			case keys.LinkWeb:
				if !s.ShowWeb {
					continue
				}
				label = t("subpage.label_web", nil)
			case keys.LinkTLS:
				tlsSeen++
				if !s.ShowFakeTLS || (tlsSeen > 1 && !s.ShowBackupDomains) {
					continue
				}
				label = t("subpage.label_tls", nil)
				if tlsSeen > 1 {
					label = t("subpage.label_tls_domain", map[string]string{"domain": l.Domain})
				}
			default:
				continue
			}
			link := Link{Kind: l.Kind, Label: label, TMe: l.TMe, Tg: l.Tg}
			if s.ShowQR {
				qr, err := qrlink.DataURI(l.TMe, in.QRSize)
				if err != nil {
					return Page{}, err
				}
				link.QRDataURI = qr
			}
			if l.Kind == keys.LinkWeb && ch.web < 0 {
				ch.web = len(server.Links)
			}
			if l.Kind == keys.LinkTLS && tlsSeen == 2 {
				ch.backup = len(server.Links)
			}
			if l.Kind == keys.LinkTLS && tlsSeen == 1 {
				ch.tls = len(server.Links)
			}
			server.Links = append(server.Links, link)
		}
		if len(server.Links) == 0 {
			continue
		}
		server.WebOnly = ch.tls < 0
		page.Servers = append(page.Servers, server)
		choices = append(choices, ch)
	}

	for _, p := range platforms {
		view := PlatformView{ID: p, Selected: p == in.Platform}
		for i, ch := range choices {
			srv := page.Servers[i]
			primary, alt := -1, -1
			switch p {
			case IOS:
				primary, alt = ch.tls, ch.backup
			case Android:
				primary, alt = ch.tls, ch.web
				if alt < 0 {
					alt = ch.backup
				}
			case Desktop:
				primary, alt = ch.web, ch.tls
			}
			if primary < 0 && p != IOS {
				primary, alt = alt, -1
			}
			if primary < 0 {
				continue
			}
			a := Action{Name: srv.Name, Hostname: srv.Hostname, Primary: srv.Links[primary]}
			if alt >= 0 {
				fallback := srv.Links[alt]
				a.Alt = &fallback
				view.HasAlt = true
			}
			view.Actions = append(view.Actions, a)
		}
		page.Platforms = append(page.Platforms, view)
	}
	return page, nil
}

func translator(l alerttext.Lang) func(string, ...string) string {
	c := alerttext.Default()
	return func(key string, kv ...string) string {
		vars := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			vars[kv[i]] = kv[i+1]
		}
		return c.T(l, key, vars)
	}
}

func Render(w io.Writer, p Page) error {
	tmpl, err := pageTmpl.Clone()
	if err != nil {
		return err
	}
	return tmpl.Funcs(template.FuncMap{"t": translator(alerttext.ParseLang(p.Lang))}).Execute(w, p)
}

type ErrorPage struct {
	Lang      string
	PanelName string
	Theme     string
	Message   string
}

func RenderError(w io.Writer, p ErrorPage) error {
	tmpl, err := errorTmpl.Clone()
	if err != nil {
		return err
	}
	return tmpl.Funcs(template.FuncMap{"t": translator(alerttext.ParseLang(p.Lang))}).Execute(w, p)
}
