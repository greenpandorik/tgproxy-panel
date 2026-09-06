package branding

// Brand defaults: what a fresh install shows before an operator edits the active
// branding profile. They mirror the column defaults in migration 00008 and the
// `--brand-*` fallbacks in web/src/index.css; the palette and the contrast figures
// behind it are documented in docs/design/brand.md.
const (
	// DefaultPanelName is the product name shown in the sidebar, the login page,
	// the browser title and the subscription pages until an operator renames it.
	DefaultPanelName = "TGProxy Panel"
	// DefaultPrimaryColor is the brand magenta: the active nav marker, focus ring,
	// links, the first chart series and the core of the logo mark.
	DefaultPrimaryColor = "#e23c92"
	// DefaultAccentColor is the teal complement: the second chart series.
	DefaultAccentColor = "#12a198"
	// DarkGround is the page background of the dark theme (`--bg` in index.css),
	// the surface the defaults are contrast-checked against.
	DarkGround = "#09090b"
)
