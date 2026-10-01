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
)

//go:embed page.tmpl.html error.tmpl.html
var templateFS embed.FS

func safeURL(s string) template.URL { return template.URL(s) } //nolint:gosec // links are built server-side from stored hostnames and secrets

var funcs = template.FuncMap{
	"safeURL": safeURL,
	"t":       func(string, ...string) string { return "" },
}

var pageTmpl = template.Must(template.New("page.tmpl.html").Funcs(funcs).ParseFS(templateFS, "page.tmpl.html"))

var errorTmpl = template.Must(template.New("error.tmpl.html").Funcs(funcs).ParseFS(templateFS, "error.tmpl.html"))

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

// Button is one way to connect to a server, with the line under it.
type Button struct {
	Kind    string
	Text    string
	Note    string
	Href    string
	Primary bool
}

// Server is one server a key is bound to: its connect buttons and every link it has.
type Server struct {
	Name    string
	Buttons []Button
	Links   []Link
}

// Page is everything the template needs to render one subscription page.
type Page struct {
	Lang         string
	Title        string
	Intro        string
	PrimaryColor string
	AccentColor  string
	PrimaryInk   string
	Theme        string
	FooterText   string
	ShowStatus   bool
	StatusLabel  string
	StatusUntil  string
	ShowQR       bool
	Servers      []Server
	Trouble      string
	SupportURL   string
	SupportLabel string
	Favicon      string
	Preview      bool
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
	// FaviconDataURI is the branding favicon as a data: link, so pages on any domain can show it.
	FaviconDataURI string `json:"favicon_data_uri,omitempty"`
}

type Input struct {
	Settings  Settings
	Lang      alerttext.Lang
	Locations []keys.NodeLinks
	ExpiresAt *time.Time
	Branding  Branding
	QRSize    int
}

// Build turns a key's links into the page, applying the owner's settings.
func Build(in Input) (Page, error) {
	c := alerttext.Default()
	t := func(key string, vars map[string]string) string { return c.T(in.Lang, key, vars) }
	or := func(own, key string) string {
		if own != "" {
			return own
		}
		return t(key, nil)
	}
	s := in.Settings
	tx := s.textsFor(in.Lang)
	page := Page{
		Lang: string(in.Lang), Title: or(tx.title, "subpage.title_default"), Intro: or(tx.intro, "subpage.intro_default"),
		PrimaryColor: in.Branding.PrimaryColor, AccentColor: in.Branding.AccentColor, PrimaryInk: in.Branding.PrimaryInk,
		Theme: in.Branding.Theme, FooterText: in.Branding.FooterText, ShowStatus: s.ShowStatus, ShowQR: s.ShowQR,
		Favicon: faviconFor(in.Branding),
	}
	if in.ExpiresAt != nil {
		page.StatusLabel = t("subpage.status_active", nil)
		page.StatusUntil = t("subpage.status_until", map[string]string{"date": formatDate(in.Lang, in.ExpiresAt.Local())})
	} else {
		page.StatusLabel = t("subpage.status_forever", nil)
	}

	tlsButton := Button{Kind: keys.LinkTLS, Text: or(tx.tlsButton, "subpage.tls_button"), Note: or(tx.tlsNote, "subpage.tls_note")}
	webButton := Button{Kind: keys.LinkWeb, Text: or(tx.webButton, "subpage.web_button"), Note: or(tx.webNote, "subpage.web_note")}
	for _, loc := range in.Locations {
		if s.hidden(loc.NodeID.String()) {
			continue
		}
		server := Server{Name: loc.NodeName}
		var tlsLinks, webLinks []Link
		tlsSeen := 0
		for _, l := range loc.Links {
			var label string
			switch l.Kind {
			case keys.LinkWeb:
				if !s.ShowWeb {
					continue
				}
				label = t("subpage.link_web", nil)
			case keys.LinkTLS:
				tlsSeen++
				if !s.ShowFakeTLS || (tlsSeen > 1 && !s.ShowBackupDomains) {
					continue
				}
				label = t("subpage.link_tls", nil)
				if tlsSeen > 1 {
					label = t("subpage.link_tls_backup", map[string]string{"domain": l.Domain})
				}
			default:
				continue
			}
			if !proxyLink(l.TMe, l.Tg) {
				continue
			}
			link := Link{Kind: l.Kind, Label: label, TMe: l.TMe, Tg: l.Tg}
			if s.ShowQR {
				qr, err := qrFor(l.TMe, in.QRSize)
				if err != nil {
					return Page{}, err
				}
				link.QRDataURI = qr
			}
			if l.Kind == keys.LinkTLS {
				tlsLinks = append(tlsLinks, link)
			} else {
				webLinks = append(webLinks, link)
			}
		}
		if len(tlsLinks) > 0 {
			b := tlsButton
			b.Href = tlsLinks[0].TMe
			server.Buttons = append(server.Buttons, b)
		}
		if len(webLinks) > 0 {
			b := webButton
			b.Href = webLinks[0].TMe
			server.Buttons = append(server.Buttons, b)
		}
		if len(server.Buttons) == 0 {
			continue
		}
		server.Buttons[0].Primary = true
		server.Links = append(tlsLinks, webLinks...)
		page.Servers = append(page.Servers, server)
	}

	if !s.HideSupport {
		if u := SafeSupportURL(s.SupportURL); u != "" {
			page.SupportURL = u
		} else if s.SupportURL == "" {
			page.SupportURL = SafeSupportURL(in.Branding.SupportLink)
		}
		if page.SupportURL != "" {
			page.SupportLabel = or(tx.supportLabel, "subpage.support")
		}
	}
	trouble := "subpage.trouble_one"
	if len(page.Servers) > 1 {
		trouble = "subpage.trouble_many"
	}
	if page.SupportURL != "" {
		trouble += "_support"
	}
	page.Trouble = t(trouble, nil)
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
	Lang    string
	Theme   string
	Message string
	Favicon string
}

func RenderError(w io.Writer, p ErrorPage) error {
	tmpl, err := errorTmpl.Clone()
	if err != nil {
		return err
	}
	return tmpl.Funcs(template.FuncMap{"t": translator(alerttext.ParseLang(p.Lang))}).Execute(w, p)
}
