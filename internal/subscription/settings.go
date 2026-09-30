package subscription

import (
	"strings"
	"unicode/utf8"

	"tgwebproxy/internal/alerttext"
)

// Settings is what the owner chooses for every subscription page.
type Settings struct {
	Language          string   `json:"language"`
	Platform          string   `json:"platform"`
	TitleRU           string   `json:"title_ru"`
	TitleEN           string   `json:"title_en"`
	IntroRU           string   `json:"intro_ru"`
	IntroEN           string   `json:"intro_en"`
	ShowFakeTLS       bool     `json:"show_fake_tls"`
	ShowWeb           bool     `json:"show_web"`
	ShowBackupDomains bool     `json:"show_backup_domains"`
	ShowGuide         bool     `json:"show_guide"`
	ShowStatus        bool     `json:"show_status"`
	ShowQR            bool     `json:"show_qr"`
	HiddenNodes       []string `json:"hidden_nodes"`
}

// DefaultSettings shows everything, in the visitor's language and on the tab of their device.
func DefaultSettings() Settings {
	return Settings{
		Language: "auto", Platform: "auto", ShowFakeTLS: true, ShowWeb: true, ShowBackupDomains: true,
		ShowGuide: true, ShowStatus: true, ShowQR: true, HiddenNodes: []string{},
	}
}

const (
	maxTitle = 80
	maxIntro = 500
)

// Validate returns field errors keyed like the JSON body.
func (s Settings) Validate() map[string]string {
	f := map[string]string{}
	switch s.Language {
	case "auto", "ru", "en":
	default:
		f["subscription_page.language"] = "must be auto, ru or en"
	}
	switch s.Platform {
	case "auto", string(Android), string(IOS), string(Desktop):
	default:
		f["subscription_page.platform"] = "must be auto, android, ios or desktop"
	}
	for key, v := range map[string]string{"title_ru": s.TitleRU, "title_en": s.TitleEN} {
		if utf8.RuneCountInString(v) > maxTitle {
			f["subscription_page."+key] = "at most 80 characters"
		}
	}
	for key, v := range map[string]string{"intro_ru": s.IntroRU, "intro_en": s.IntroEN} {
		if utf8.RuneCountInString(v) > maxIntro {
			f["subscription_page."+key] = "at most 500 characters"
		}
	}
	if !s.ShowFakeTLS && !s.ShowWeb {
		f["subscription_page.show_fake_tls"] = "show at least one kind of link"
	}
	return f
}

// Normalized trims the texts and never returns a nil list.
func (s Settings) Normalized() Settings {
	s.TitleRU = strings.TrimSpace(s.TitleRU)
	s.TitleEN = strings.TrimSpace(s.TitleEN)
	s.IntroRU = strings.TrimSpace(s.IntroRU)
	s.IntroEN = strings.TrimSpace(s.IntroEN)
	if s.HiddenNodes == nil {
		s.HiddenNodes = []string{}
	}
	return s
}

func (s Settings) texts(l alerttext.Lang) (title, intro string) {
	if l == alerttext.EN {
		return s.TitleEN, s.IntroEN
	}
	return s.TitleRU, s.IntroRU
}

func (s Settings) hidden(nodeID string) bool {
	for _, id := range s.HiddenNodes {
		if id == nodeID {
			return true
		}
	}
	return false
}
