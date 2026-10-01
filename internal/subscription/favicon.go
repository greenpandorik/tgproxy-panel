package subscription

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// MaxFaviconBytes is the largest custom favicon embedded into the page.
const MaxFaviconBytes = 48 << 10

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var faviconTypes = []string{"data:image/svg+xml;base64,", "data:image/png;base64,", "data:image/x-icon;base64,", "data:image/jpeg;base64,"}

// FaviconDataURI turns an uploaded favicon into the data: link the page embeds, or "" when the
// type is not an image the page accepts or the file is too large.
func FaviconDataURI(contentType string, data []byte) string {
	if len(data) == 0 || len(data) > MaxFaviconBytes {
		return ""
	}
	uri := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	if !embeddable(uri) {
		return ""
	}
	return uri
}

func embeddable(uri string) bool {
	for _, prefix := range faviconTypes {
		if strings.HasPrefix(uri, prefix) {
			return true
		}
	}
	return false
}

func defaultFavicon(primary string) string {
	if !hexColor.MatchString(primary) {
		primary = "#3fc0d6"
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" width="32" height="32"><rect width="32" height="32" rx="7" fill="#171b21"/>` +
		`<g transform="translate(4 4)"><path d="M2.6 9.7c0-1.9 1.4-2.9 3.2-2.9h12.4c1.8 0 3.2 1 3.2 2.9 0 4.2-2.4 7.6-5.5 7.6-1.9 0-3-1.3-3.9-2.8-.9 1.5-2 2.8-3.9 2.8-3.1 0-5.5-3.4-5.5-7.6z" fill="none" stroke="#e9edf1" stroke-width="2" stroke-linejoin="round"/>` +
		`<ellipse cx="8.1" cy="11.5" rx="1.9" ry="1.4" fill="` + primary + `"/><ellipse cx="15.9" cy="11.5" rx="1.9" ry="1.4" fill="` + primary + `"/></g></svg>`
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

// faviconFor is the custom favicon when the branding has a usable one, else the standard mark.
func faviconFor(b Branding) string {
	if b.FaviconDataURI != "" && embeddable(b.FaviconDataURI) && len(b.FaviconDataURI) <= 2*MaxFaviconBytes {
		return b.FaviconDataURI
	}
	return defaultFavicon(b.PrimaryColor)
}
