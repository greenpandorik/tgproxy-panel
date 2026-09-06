// Package branding validates and sanitises operator-provided branding.
package branding

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
)

var (
	reImport     = regexp.MustCompile(`(?i)@import[^;]*;?`)
	reExtURL     = regexp.MustCompile(`(?i)url\(\s*['"]?\s*(?:https?:)?//[^)]*\)`)
	reExpression = regexp.MustCompile(`(?i)expression\([^)]*\)`)
	reBehavior   = regexp.MustCompile(`(?i)behavior\s*:[^;}]*;?`)
	reColor      = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

// SanitizeCSS strips dangerous constructs from operator-supplied custom CSS and
// returns the cleaned CSS along with a human-readable list of what was removed.
func SanitizeCSS(css string) (string, []string) {
	var removed []string
	for name, re := range map[string]*regexp.Regexp{"@import": reImport, "external url()": reExtURL, "expression()": reExpression, "behavior": reBehavior} {
		for _, m := range re.FindAllString(css, -1) {
			removed = append(removed, name+": "+strings.TrimSpace(m))
		}
		css = re.ReplaceAllString(css, "")
	}
	css = strings.ReplaceAll(css, "</style", "")
	return css, removed
}

// ValidateColor reports whether s is a #rgb or #rrggbb hex color.
func ValidateColor(s string) bool { return reColor.MatchString(s) }

// forbiddenSVGElements are elements that can execute script or load a nested browsing
// context when the file is fetched as image/svg+xml.
var forbiddenSVGElements = map[string]bool{"script": true, "foreignobject": true, "iframe": true}

// animationSVGElements can retarget another element's attribute at run time; pointing one
// at href/xlink:href smuggles in a javascript: URL that no static attribute check sees.
var animationSVGElements = map[string]bool{"set": true, "animate": true, "animatetransform": true}

// SanitizeSVG rejects SVG markup containing script execution vectors: <script>,
// <foreignObject>, <iframe>, event handler attributes, non-fragment hrefs, and
// <set>/<animate>/<animateTransform> retargeting href.
//
// It parses with encoding/xml, not an HTML tokenizer, because the browser fetches this file
// as image/svg+xml and parses it as XML. An HTML tokenizer turns `<![CDATA[…]]>` into a bogus
// comment and walks straight past a <script> hidden inside it, while the browser sees real
// CDATA and runs it. Anything the XML decoder cannot parse is rejected rather than passed
// through: the browser's XML parser is at least as strict, so a file we cannot read is a file
// we cannot vouch for.
func SanitizeSVG(svg []byte) ([]byte, error) {
	if !bytes.Contains(bytes.ToLower(svg), []byte("<svg")) {
		return nil, errors.New("not an svg")
	}
	dec := xml.NewDecoder(bytes.NewReader(svg))
	// Tolerate the named entities and unresolved prefixes real-world SVG exporters emit;
	// every token is still inspected below.
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("svg is not well-formed XML: " + err.Error())
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if forbiddenSVGElements[name] {
				return nil, errors.New("svg contains <" + name + ">")
			}
			isAnimation := animationSVGElements[name]
			for _, a := range t.Attr {
				// Namespace prefixes are resolved by the decoder, so xlink:href arrives
				// as Local="href" whether or not the prefix was declared.
				k := strings.ToLower(a.Name.Local)
				if strings.HasPrefix(k, "on") {
					return nil, errors.New("svg contains event handler " + a.Name.Local)
				}
				if k == "href" && !strings.HasPrefix(strings.TrimSpace(a.Value), "#") {
					return nil, errors.New("svg references an external href")
				}
				if isAnimation && k == "attributename" {
					target := strings.ToLower(strings.TrimSpace(a.Value))
					if target == "href" || target == "xlink:href" {
						return nil, errors.New("svg animates <" + name + "> onto href")
					}
				}
			}
		case xml.CharData:
			// CDATA sections reach us here with their markup intact.
			if containsScriptTag(t) {
				return nil, errors.New("svg contains <script> inside character data")
			}
		case xml.Comment:
			if containsScriptTag(t) {
				return nil, errors.New("svg contains <script> inside a comment")
			}
		case xml.Directive:
			if containsScriptTag(t) {
				return nil, errors.New("svg contains <script> inside a directive")
			}
		case xml.ProcInst:
			if containsScriptTag(t.Inst) {
				return nil, errors.New("svg contains <script> inside a processing instruction")
			}
		}
	}
	return svg, nil
}

func containsScriptTag(b []byte) bool { return bytes.Contains(bytes.ToLower(b), []byte("<script")) }
