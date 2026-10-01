package subscription

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"tgwebproxy/internal/alerttext"
)

// Settings is what the owner chooses for every subscription page. An empty text means the
// standard one in the page's language.
type Settings struct {
	Language          string   `json:"language"`
	TitleRU           string   `json:"title_ru"`
	TitleEN           string   `json:"title_en"`
	IntroRU           string   `json:"intro_ru"`
	IntroEN           string   `json:"intro_en"`
	TLSButtonRU       string   `json:"tls_button_ru"`
	TLSButtonEN       string   `json:"tls_button_en"`
	TLSNoteRU         string   `json:"tls_note_ru"`
	TLSNoteEN         string   `json:"tls_note_en"`
	WebButtonRU       string   `json:"web_button_ru"`
	WebButtonEN       string   `json:"web_button_en"`
	WebNoteRU         string   `json:"web_note_ru"`
	WebNoteEN         string   `json:"web_note_en"`
	SupportLabelRU    string   `json:"support_label_ru"`
	SupportLabelEN    string   `json:"support_label_en"`
	SupportURL        string   `json:"support_url"`
	HideSupport       bool     `json:"hide_support"`
	ShowFakeTLS       bool     `json:"show_fake_tls"`
	ShowWeb           bool     `json:"show_web"`
	ShowBackupDomains bool     `json:"show_backup_domains"`
	ShowStatus        bool     `json:"show_status"`
	ShowQR            bool     `json:"show_qr"`
	HiddenNodes       []string `json:"hidden_nodes"`
}

// DefaultSettings shows every kind of link, the access term and QR codes, in the visitor's language.
func DefaultSettings() Settings {
	return Settings{
		Language: "auto", ShowFakeTLS: true, ShowWeb: true, ShowBackupDomains: true,
		ShowStatus: true, ShowQR: true, HiddenNodes: []string{},
	}
}

const (
	maxTitle      = 80
	maxIntro      = 500
	maxButton     = 40
	maxNote       = 140
	maxSupportURL = 300
)

var supportSchemes = map[string]bool{"https": true, "http": true, "tg": true, "mailto": true}

// SafeSupportURL returns the link when it is one a visitor may be sent to, else "".
func SafeSupportURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxSupportURL {
		return ""
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '"' || r == '<' || r == '>' || r == '\\' || r == '`' {
			return ""
		}
	}
	u, err := url.Parse(raw)
	if err != nil || !supportSchemes[strings.ToLower(u.Scheme)] {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		if u.Host == "" {
			return ""
		}
	case "tg", "mailto":
		if u.Opaque == "" && u.Host == "" {
			return ""
		}
	}
	return raw
}

// Validate returns field errors keyed like the JSON body.
func (s Settings) Validate() map[string]string {
	f := map[string]string{}
	switch s.Language {
	case "auto", "ru", "en":
	default:
		f["subscription_page.language"] = "must be auto, ru or en"
	}
	limits := []struct {
		key   string
		value string
		max   int
		msg   string
	}{
		{"title_ru", s.TitleRU, maxTitle, "at most 80 characters"},
		{"title_en", s.TitleEN, maxTitle, "at most 80 characters"},
		{"intro_ru", s.IntroRU, maxIntro, "at most 500 characters"},
		{"intro_en", s.IntroEN, maxIntro, "at most 500 characters"},
		{"tls_button_ru", s.TLSButtonRU, maxButton, "at most 40 characters"},
		{"tls_button_en", s.TLSButtonEN, maxButton, "at most 40 characters"},
		{"web_button_ru", s.WebButtonRU, maxButton, "at most 40 characters"},
		{"web_button_en", s.WebButtonEN, maxButton, "at most 40 characters"},
		{"support_label_ru", s.SupportLabelRU, maxButton, "at most 40 characters"},
		{"support_label_en", s.SupportLabelEN, maxButton, "at most 40 characters"},
		{"tls_note_ru", s.TLSNoteRU, maxNote, "at most 140 characters"},
		{"tls_note_en", s.TLSNoteEN, maxNote, "at most 140 characters"},
		{"web_note_ru", s.WebNoteRU, maxNote, "at most 140 characters"},
		{"web_note_en", s.WebNoteEN, maxNote, "at most 140 characters"},
	}
	for _, l := range limits {
		if utf8.RuneCountInString(l.value) > l.max {
			f["subscription_page."+l.key] = l.msg
		}
	}
	if u := strings.TrimSpace(s.SupportURL); !s.HideSupport && u != "" && SafeSupportURL(u) == "" {
		f["subscription_page.support_url"] = "must be an https://, http://, tg:// or mailto: link"
	}
	if !s.ShowFakeTLS && !s.ShowWeb {
		f["subscription_page.show_fake_tls"] = "show at least one kind of link"
	}
	return f
}

// Normalized trims the texts and never returns a nil list.
func (s Settings) Normalized() Settings {
	for _, p := range []*string{
		&s.TitleRU, &s.TitleEN, &s.IntroRU, &s.IntroEN, &s.TLSButtonRU, &s.TLSButtonEN, &s.TLSNoteRU, &s.TLSNoteEN,
		&s.WebButtonRU, &s.WebButtonEN, &s.WebNoteRU, &s.WebNoteEN, &s.SupportLabelRU, &s.SupportLabelEN, &s.SupportURL,
	} {
		*p = strings.TrimSpace(*p)
	}
	if s.HiddenNodes == nil {
		s.HiddenNodes = []string{}
	}
	return s
}

type texts struct {
	title, intro, tlsButton, tlsNote, webButton, webNote, supportLabel string
}

func (s Settings) textsFor(l alerttext.Lang) texts {
	if l == alerttext.EN {
		return texts{s.TitleEN, s.IntroEN, s.TLSButtonEN, s.TLSNoteEN, s.WebButtonEN, s.WebNoteEN, s.SupportLabelEN}
	}
	return texts{s.TitleRU, s.IntroRU, s.TLSButtonRU, s.TLSNoteRU, s.WebButtonRU, s.WebNoteRU, s.SupportLabelRU}
}

func (s Settings) hidden(nodeID string) bool {
	for _, id := range s.HiddenNodes {
		if id == nodeID {
			return true
		}
	}
	return false
}
