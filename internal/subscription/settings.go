package subscription

import (
	"strings"
	"unicode/utf8"
)

// Settings is what the owner chooses for every subscription page.
type Settings struct {
	Language          string   `json:"language"`
	Platform          string   `json:"platform"`
	Title             string   `json:"title"`
	Intro             string   `json:"intro"`
	ShowFakeTLS       bool     `json:"show_fake_tls"`
	ShowWeb           bool     `json:"show_web"`
	ShowBackupDomains bool     `json:"show_backup_domains"`
	ShowGuide         bool     `json:"show_guide"`
	ShowStatus        bool     `json:"show_status"`
	ShowQR            bool     `json:"show_qr"`
	HiddenNodes       []string `json:"hidden_nodes"`
}

// DefaultSettings shows everything in Russian with the Android tab open; nothing is guessed from the visitor.
func DefaultSettings() Settings {
	return Settings{
		Language: "ru", Platform: string(Android), ShowFakeTLS: true, ShowWeb: true, ShowBackupDomains: true,
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
	if utf8.RuneCountInString(s.Title) > maxTitle {
		f["subscription_page.title"] = "at most 80 characters"
	}
	if utf8.RuneCountInString(s.Intro) > maxIntro {
		f["subscription_page.intro"] = "at most 500 characters"
	}
	if !s.ShowFakeTLS && !s.ShowWeb {
		f["subscription_page.show_fake_tls"] = "show at least one kind of link"
	}
	return f
}

// Normalized trims the texts and never returns a nil list.
func (s Settings) Normalized() Settings {
	s.Title = strings.TrimSpace(s.Title)
	s.Intro = strings.TrimSpace(s.Intro)
	if s.HiddenNodes == nil {
		s.HiddenNodes = []string{}
	}
	return s
}

func (s Settings) hidden(nodeID string) bool {
	for _, id := range s.HiddenNodes {
		if id == nodeID {
			return true
		}
	}
	return false
}
