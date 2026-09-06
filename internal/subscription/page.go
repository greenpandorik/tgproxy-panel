// Package subscription renders the public, per-access-key subscription page:
// a small static HTML document listing the WEB proxy links and QR codes for
// every node a key is bound to. It never sees (and the Page type has no field
// for) the key's label, owner label or note - the caller only ever hands it
// node names and hostnames.
package subscription

import (
	"embed"
	"html/template"
	"io"
)

//go:embed page.tmpl.html error.tmpl.html
var templateFS embed.FS

// safeURL marks a server-built URL as safe to place in an href/src attribute
// unescaped by html/template's scheme allowlist. It is only ever applied to
// the tg:// deep link and the data: QR image this package itself constructs
// (via qrlink, from an admin-set hostname and a generated secret) - never to
// caller-supplied free text - so nothing "unsafe" reaches it.
func safeURL(s string) template.URL { return template.URL(s) } //nolint:gosec // constructed server-side, see comment above

var pageTmpl = template.Must(template.New("page.tmpl.html").Funcs(template.FuncMap{"safeURL": safeURL}).ParseFS(templateFS, "page.tmpl.html"))

var errorTmpl = template.Must(template.New("error.tmpl.html").ParseFS(templateFS, "error.tmpl.html"))

// LocationLink is one connection method for a location: the kind ("web" or
// "tls"), a human label for it, the two link forms Telegram clients understand,
// and a pre-rendered QR code as a data URI (so the page needs no image requests
// of its own).
type LocationLink struct {
	Kind      string
	Label     string
	TMe       string
	Tg        string
	QRDataURI string
}

// Location is one node a key is bound to: its display name, hostname and every
// connection method it offers. A telemt node offers both WEB and Fake-TLS; a
// tproxy node only WEB.
type Location struct {
	Name     string
	Hostname string
	Links    []LocationLink
}

// Page is everything the template needs to render one subscription page. All
// styling comes from the active branding profile at render time; the template
// has no fallback styling of its own.
type Page struct {
	PanelName     string
	PrimaryColor  string
	AccentColor   string
	Theme         string // "dark" or "light"
	SupportLink   string
	FooterText    string
	Locations     []Location
	ClientSupport map[string]string
}

// Render writes the subscription page as self-contained HTML: every style is
// inline in a <style> block and the only script is the small addEventListener
// block that wires up the copy buttons - no external stylesheet, script,
// font or image request ever leaves the client's browser.
func Render(w io.Writer, p Page) error {
	return pageTmpl.Execute(w, p)
}

// ErrorPage is the minimal branded page shown for the HTML /s/{token} route
// when the token is unknown (404) or its key has been revoked (410). It
// carries no key/node data at all - by the time it renders, the lookup has
// already failed, so there is nothing to leak - only enough branding to look
// like part of the panel rather than a bare status code.
type ErrorPage struct {
	PanelName string
	Theme     string // "dark" or "light"
	Message   string
}

// RenderError writes ErrorPage as the same kind of self-contained HTML as
// Render: everything inline, no external resources.
func RenderError(w io.Writer, p ErrorPage) error {
	return errorTmpl.Execute(w, p)
}
