package subscription

import (
	"embed"
	"html/template"
	"io"
)

//go:embed page.tmpl.html error.tmpl.html
var templateFS embed.FS

func safeURL(s string) template.URL { return template.URL(s) } //nolint:gosec // constructed server-side, see comment above

var pageTmpl = template.Must(template.New("page.tmpl.html").Funcs(template.FuncMap{"safeURL": safeURL}).ParseFS(templateFS, "page.tmpl.html"))

var errorTmpl = template.Must(template.New("error.tmpl.html").ParseFS(templateFS, "error.tmpl.html"))

type LocationLink struct {
	Kind      string
	Label     string
	TMe       string
	Tg        string
	QRDataURI string
}

// Location is one node a key is bound to: its display name, hostname and every connection method it offers.
type Location struct {
	Name     string
	Hostname string
	Links    []LocationLink
}

// Page is everything the template needs to render one subscription page.
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

func Render(w io.Writer, p Page) error {
	return pageTmpl.Execute(w, p)
}

type ErrorPage struct {
	PanelName string
	Theme     string // "dark" or "light"
	Message   string
}

func RenderError(w io.Writer, p ErrorPage) error {
	return errorTmpl.Execute(w, p)
}
