// Package alerttext renders Telegram notifications from the interface translations.
package alerttext

import (
	"encoding/json"
	"html"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"tgwebproxy/web/src/i18n"
)

type Lang string

const (
	RU Lang = "ru"
	EN Lang = "en"
)

// ParseLang maps a stored setting to a language; anything but "en" is Russian.
func ParseLang(s string) Lang {
	if s == string(EN) {
		return EN
	}
	return RU
}

type Catalog struct {
	strings map[Lang]map[string]string
}

var (
	defaultOnce    sync.Once
	defaultCatalog *Catalog
)

// Default is the catalog built from the embedded interface translations.
func Default() *Catalog {
	defaultOnce.Do(func() {
		c, err := Load(i18n.Files)
		if err != nil {
			c = &Catalog{strings: map[Lang]map[string]string{}}
		}
		defaultCatalog = c
	})
	return defaultCatalog
}

// Load reads ru.json and en.json from fsys.
func Load(fsys fs.FS) (*Catalog, error) {
	c := &Catalog{strings: map[Lang]map[string]string{}}
	for _, l := range []Lang{RU, EN} {
		raw, err := fs.ReadFile(fsys, string(l)+".json")
		if err != nil {
			return nil, err
		}
		var tree map[string]any
		if err := json.Unmarshal(raw, &tree); err != nil {
			return nil, err
		}
		flat := map[string]string{}
		flatten("", tree, flat)
		c.strings[l] = flat
	}
	return c, nil
}

func flatten(prefix string, node map[string]any, out map[string]string) {
	for k, v := range node {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch v := v.(type) {
		case string:
			out[key] = v
		case map[string]any:
			flatten(key, v, out)
		}
	}
}

func (c *Catalog) has(l Lang, key string) bool {
	_, ok := c.strings[l][key]
	return ok
}

var placeholder = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

// text returns the plain translation with vars filled in; it falls back to English, then to the key.
func (c *Catalog) text(l Lang, key string, vars map[string]string) string {
	s, ok := c.strings[l][key]
	if !ok {
		if s, ok = c.strings[EN][key]; !ok {
			return key
		}
	}
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		return vars[placeholder.FindStringSubmatch(m)[1]]
	})
}

// htmlText is text with the translation and every var escaped for Telegram's HTML mode.
func (c *Catalog) htmlText(l Lang, key string, vars map[string]string) string {
	escaped := make(map[string]string, len(vars))
	for k, v := range vars {
		escaped[k] = html.EscapeString(v)
	}
	s, ok := c.strings[l][key]
	if !ok {
		if s, ok = c.strings[EN][key]; !ok {
			return html.EscapeString(key)
		}
	}
	return placeholder.ReplaceAllStringFunc(html.EscapeString(s), func(m string) string {
		return escaped[placeholder.FindStringSubmatch(m)[1]]
	})
}

// Node is what a message needs to know about a server.
type Node struct {
	ID       string
	Name     string
	Hostname string
}

// Message is one notification: HTML for Telegram and plain text for the webhook.
type Message struct {
	HTML  string
	Plain string
}

var (
	tag    = regexp.MustCompile(`<[^>]+>`)
	anchor = regexp.MustCompile(`<a href="([^"]*)">([^<]*)</a>`)
)

func message(lines []string) Message {
	body := strings.Join(nonEmpty(lines), "\n")
	plain := anchor.ReplaceAllString(body, "$2: $1")
	return Message{HTML: body, Plain: html.UnescapeString(tag.ReplaceAllString(plain, ""))}
}

func nonEmpty(lines []string) []string {
	out := lines[:0:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func (c *Catalog) link(l Lang, panelURL, nodeID, section string) string {
	if !strings.HasPrefix(panelURL, "http") || nodeID == "" {
		return ""
	}
	href := strings.TrimRight(panelURL, "/") + "/nodes/" + nodeID
	if section != "" {
		href += "?section=" + section
	}
	return `<a href="` + html.EscapeString(href) + `">` + c.htmlText(l, "notify.open_server", nil) + `</a>`
}

func bold(s string) string { return "<b>" + html.EscapeString(s) + "</b>" }

// Offline says a server stopped reporting.
func (c *Catalog) Offline(l Lang, n Node, panelURL string) Message {
	return message([]string{
		"🔴 " + c.htmlText(l, "notify.offline_title", map[string]string{"name": "\x00"}),
		c.htmlText(l, "notify.offline_body", map[string]string{"host": n.Hostname}),
		c.link(l, panelURL, n.ID, ""),
	}).withName(n.Name)
}

// Online says a server is reporting again.
func (c *Catalog) Online(l Lang, n Node) Message {
	return message([]string{"🟢 " + c.htmlText(l, "notify.online_title", map[string]string{"name": "\x00"})}).withName(n.Name)
}

// ApplyFailed says an apply job failed; only the first line of the agent's error is shown.
func (c *Catalog) ApplyFailed(l Lang, n Node, jobErr, panelURL string) Message {
	first, _, _ := strings.Cut(jobErr, "\n")
	lines := []string{
		"❌ " + c.htmlText(l, "notify.apply_failed_title", map[string]string{"name": "\x00"}),
		c.htmlText(l, "notify.apply_failed_body", nil),
	}
	if strings.TrimSpace(first) != "" {
		lines = append(lines, "<code>"+html.EscapeString(strings.TrimSpace(first))+"</code>")
	}
	lines = append(lines, c.link(l, panelURL, n.ID, ""))
	return message(lines).withName(n.Name)
}

// Test is the message the settings page sends to check the chat.
func (c *Catalog) Test(l Lang, panelName string) Message {
	return message([]string{"✅ " + c.htmlText(l, "notify.test", map[string]string{"panel": panelName})})
}

// withName puts the bold server name where the "\x00" marker stands, so the name is escaped once.
func (m Message) withName(name string) Message {
	return Message{
		HTML:  strings.Replace(m.HTML, "\x00", bold(name), 1),
		Plain: strings.Replace(m.Plain, "\x00", name, 1),
	}
}
