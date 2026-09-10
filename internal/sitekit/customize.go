package sitekit

import (
	"bytes"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/net/html"
)

// Customize edits text nodes and a mailto address, never interpolating HTML or script.
func Customize(source string, defaults, values map[string]string) (string, error) {
	for key, v := range values {
		if _, ok := defaults[key]; !ok {
			return "", fmt.Errorf("unknown variable %s", key)
		}
		if len(v) > 1000 {
			return "", fmt.Errorf("%s exceeds 1000 bytes", key)
		}
		if key == "contact_email" && v != "" {
			address, err := mail.ParseAddress(v)
			if err != nil || address.Address != v {
				return "", fmt.Errorf("invalid contact_email")
			}
		}
	}
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	var heading, description *html.Node
	setText := func(n *html.Node, v string) {
		for n.FirstChild != nil {
			n.RemoveChild(n.FirstChild)
		}
		n.AppendChild(&html.Node{Type: html.TextNode, Data: v})
		attrs := n.Attr[:0]
		for _, a := range n.Attr {
			if a.Key != "data-variants" {
				attrs = append(attrs, a)
			}
		}
		n.Attr = attrs
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.ElementNode {
			if n.Data == "h1" && heading == nil {
				heading = n
			}
			if n.Data == "p" && description == nil && heading != nil {
				description = n
			}
			if n.Data == "meta" && attr(n, "name") == "description" {
				if v, ok := values["description"]; ok {
					for i := range n.Attr {
						if n.Attr[i].Key == "content" {
							n.Attr[i].Val = v
						}
					}
				}
			}
			if n.Data == "a" && strings.HasPrefix(attr(n, "href"), "mailto:") {
				if v, ok := values["contact_email"]; ok {
					for i := range n.Attr {
						if n.Attr[i].Key == "href" {
							n.Attr[i].Val = "mailto:" + v
						}
					}
				}
			}
		}
		if n.Type == html.TextNode {
			for _, key := range []string{"company_name", "contact_email"} {
				if v, ok := values[key]; ok && defaults[key] != "" {
					n.Data = strings.ReplaceAll(n.Data, defaults[key], v)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if v, ok := values["headline"]; ok && heading != nil {
		setText(heading, v)
	}
	if v, ok := values["description"]; ok && description != nil {
		setText(description, v)
	}
	// Every built-in footer is customizable even when its displayed text differs from its manifest.
	if v, ok := values["footer_text"]; ok {
		var footer *html.Node
		var find func(*html.Node)
		find = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "footer" {
				footer = n
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				find(c)
			}
		}
		find(doc)
		if footer != nil {
			var last *html.Node
			var paragraph func(*html.Node)
			paragraph = func(n *html.Node) {
				if n.Type == html.ElementNode && n.Data == "p" {
					last = n
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					paragraph(c)
				}
			}
			paragraph(footer)
			if last != nil {
				setText(last, v)
			} else {
				p := &html.Node{Type: html.ElementNode, Data: "p"}
				setText(p, v)
				footer.AppendChild(p)
			}
		}
	}
	var out bytes.Buffer
	err = html.Render(&out, doc)
	return out.String(), err
}
