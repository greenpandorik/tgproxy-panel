// Package sitekit validates operator sites against the relay/CSP rules and packages them.
package sitekit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Bundle struct{ Files map[string][]byte }

type Report struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// inputDigest is a stable fingerprint of a Normalize input: the HTML plus every asset in
// sorted path order. Generated names are derived from it so that normalising the same
// input twice yields a byte-identical bundle — and therefore the same Bundle.Hash().
// Re-assigning the template a node already runs must not look like a change, because a
// changed bundle makes the agent swap the site directory and restart the relay, which
// drops every live carrier session.
func inputDigest(src string, assets map[string][]byte) []byte {
	paths := make([]string, 0, len(assets))
	for p := range assets {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	h.Write([]byte(src))
	h.Write([]byte{0})
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(assets[p])
		h.Write([]byte{0})
	}
	return h.Sum(nil)
}

// derive produces a short, stable, per-item suffix from the input digest. kind separates
// the namespaces (stylesheet / script / generated class) and n is a counter within a
// namespace, so two style attributes with identical content still get distinct classes.
func derive(seed []byte, kind string, n int) string {
	h := sha256.New()
	h.Write(seed)
	h.Write([]byte(kind))
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	h.Write(b[:])
	return hex.EncodeToString(h.Sum(nil)[:4])
}

var (
	reImport      = regexp.MustCompile(`(?i)@import`)
	reURLExternal = regexp.MustCompile(`(?i)url\(\s*['"]?\s*(https?:)?//`)
	reSW          = regexp.MustCompile(`(?i)serviceWorker`)
)

// isLocal reports whether a URL points into the site itself.
func isLocal(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "tel:") {
		return true
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "data:") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == ""
}

// cleanAssetPath is the single normalisation used for both bundle file keys and
// the paths referenced from the HTML. Both sides must agree: when they did not,
// a reference like "./a.css" to an asset stored as "a.css" was reported as a
// missing file. It rejects traversal and empty paths, mirroring the agent's
// safeSitePath guard so the panel never offers an unsafe path to a node.
func cleanAssetPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "..") {
		return "", false
	}
	clean := path.Clean("/" + raw)
	if clean == "/" {
		return "", false
	}
	return strings.TrimPrefix(clean, "/"), true
}

// assetPath maps an href/src attribute onto a bundle file key. An unsafe or
// unparseable reference is returned close to verbatim so it still surfaces as a
// missing-file error rather than being silently dropped.
func assetPath(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if clean, ok := cleanAssetPath(u.Path); ok {
		return clean
	}
	return strings.TrimPrefix(u.Path, "/")
}

// sanitizeAssetPath rejects path-traversal or otherwise unsafe asset keys before they become
// bundle file paths.
func sanitizeAssetPath(raw string) (string, bool) { return cleanAssetPath(raw) }

// Normalize parses html, rejects forbidden constructs, extracts inline CSS/JS into files and
// returns a deployable bundle. Report.Errors non-empty means the site must be rejected.
func Normalize(src string, assets map[string][]byte) (Bundle, Report, error) {
	var rep Report
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Bundle{}, rep, err
	}
	files := map[string][]byte{}
	for p, c := range assets {
		clean, ok := sanitizeAssetPath(p)
		if !ok {
			rep.Errors = append(rep.Errors, "invalid asset path: "+p)
			continue
		}
		files[clean] = c
	}
	seed := inputDigest(src, assets)
	styleN := 0
	var css bytes.Buffer
	var js bytes.Buffer
	var head, body *html.Node
	var remove []*html.Node
	referenced := map[string]bool{}

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Head:
				head = n
			case atom.Body:
				body = n
			case atom.Iframe, atom.Frame, atom.Object, atom.Embed:
				rep.Errors = append(rep.Errors, fmt.Sprintf("<%s> is not allowed (CSP blocks framing and plugins)", n.Data))
			case atom.Form:
				rep.Errors = append(rep.Errors, "<form> is not allowed (form-action 'none')")
			case atom.Base:
				rep.Errors = append(rep.Errors, "<base> is not allowed (base-uri 'none')")
			case atom.Style:
				text := textOf(n)
				if reImport.MatchString(text) || reURLExternal.MatchString(text) {
					rep.Errors = append(rep.Errors, "<style> uses @import or an external url()")
				}
				css.WriteString(text + "\n")
				remove = append(remove, n)
				rep.Warnings = append(rep.Warnings, "inline <style> moved to a stylesheet file")
			case atom.Script:
				if srcAttr := attr(n, "src"); srcAttr != "" {
					if !isLocal(srcAttr) {
						rep.Errors = append(rep.Errors, "external script: "+srcAttr)
					} else {
						referenced[assetPath(srcAttr)] = true
					}
				} else {
					text := textOf(n)
					if reSW.MatchString(text) {
						rep.Errors = append(rep.Errors, "service workers are not allowed")
					}
					js.WriteString(text + "\n")
					remove = append(remove, n)
					rep.Warnings = append(rep.Warnings, "inline <script> moved to a script file")
				}
			case atom.Link:
				rel := strings.ToLower(attr(n, "rel"))
				href := attr(n, "href")
				if strings.Contains(rel, "manifest") {
					rep.Errors = append(rep.Errors, "web app manifest is not allowed")
				}
				if href != "" && !isLocal(href) {
					rep.Errors = append(rep.Errors, "external resource: "+href)
				} else if href != "" && (strings.Contains(rel, "stylesheet") || strings.Contains(rel, "icon")) {
					referenced[assetPath(href)] = true
				}
			case atom.Img, atom.Source, atom.Video, atom.Audio, atom.Track:
				if srcAttr := attr(n, "src"); srcAttr != "" {
					if !isLocal(srcAttr) {
						rep.Errors = append(rep.Errors, "external media: "+srcAttr)
					} else {
						referenced[assetPath(srcAttr)] = true
					}
				}
				if srcset := attr(n, "srcset"); srcset != "" {
					for _, part := range strings.Split(srcset, ",") {
						u := strings.Fields(strings.TrimSpace(part))
						if len(u) > 0 && !isLocal(u[0]) {
							rep.Errors = append(rep.Errors, "external media in srcset: "+u[0])
						}
					}
				}
			case atom.A:
				if href := attr(n, "href"); strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "javascript:") {
					rep.Errors = append(rep.Errors, "javascript: links are not allowed")
				}
			}

			// Rebuild the attribute list once: compute the generated class first (if a
			// style attribute is present), then drop style/on* and replace/add class.
			// Never rely on attribute order in n.Attr.
			styleVal := attr(n, "style")
			var cls string
			hasStyle := styleVal != ""
			if hasStyle {
				cls = "i-" + derive(seed, "class", styleN)
				styleN++
				css.WriteString("." + cls + "{" + styleVal + "}\n")
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("style attribute on <%s> moved to class .%s", n.Data, cls))
			}
			kept := make([]html.Attribute, 0, len(n.Attr))
			classAdded := false
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				switch {
				case key == "style":
					// dropped
				case strings.HasPrefix(key, "on"):
					rep.Errors = append(rep.Errors, fmt.Sprintf("inline handler %s on <%s>", a.Key, n.Data))
				case key == "class" && hasStyle:
					kept = append(kept, html.Attribute{Key: "class", Val: strings.TrimSpace(a.Val + " " + cls)})
					classAdded = true
				default:
					kept = append(kept, a)
				}
			}
			if hasStyle && !classAdded {
				kept = append(kept, html.Attribute{Key: "class", Val: cls})
			}
			n.Attr = kept
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	for _, n := range remove {
		n.Parent.RemoveChild(n)
	}
	if head == nil || body == nil {
		rep.Errors = append(rep.Errors, "document must have <head> and <body>")
		return Bundle{}, rep, nil
	}
	if css.Len() > 0 {
		name := "s-" + derive(seed, "css", 0) + ".css"
		files[name] = css.Bytes()
		head.AppendChild(&html.Node{
			Type: html.ElementNode, Data: "link", DataAtom: atom.Link,
			Attr: []html.Attribute{{Key: "rel", Val: "stylesheet"}, {Key: "href", Val: "/" + name}},
		})
	}
	if js.Len() > 0 {
		name := "j-" + derive(seed, "js", 0) + ".js"
		files[name] = js.Bytes()
		body.AppendChild(&html.Node{
			Type: html.ElementNode, Data: "script", DataAtom: atom.Script,
			Attr: []html.Attribute{{Key: "src", Val: "/" + name}, {Key: "defer", Val: ""}},
		})
	}
	for p := range referenced {
		if _, ok := files[p]; !ok && p != "" {
			rep.Errors = append(rep.Errors, "referenced local file is missing from assets: /"+p)
		}
	}
	for p, c := range files {
		if strings.HasSuffix(p, ".css") && (reImport.Match(c) || reURLExternal.Match(c)) {
			rep.Errors = append(rep.Errors, "stylesheet "+p+" uses @import or an external url()")
		}
	}
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return Bundle{}, rep, err
	}
	files["index.html"] = out.Bytes()
	if len(rep.Errors) > 0 {
		return Bundle{}, rep, nil
	}
	return Bundle{Files: files}, rep, nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func (b Bundle) Hash() string {
	paths := make([]string, 0, len(b.Files))
	for p := range b.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(b.Files[p])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (b Bundle) JSON() []byte {
	m := map[string]string{}
	for p, c := range b.Files {
		m[p] = base64.StdEncoding.EncodeToString(c)
	}
	out, _ := json.Marshal(m)
	return out
}

func BundleFromJSON(raw []byte) (Bundle, error) {
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Bundle{}, err
	}
	b := Bundle{Files: map[string][]byte{}}
	for p, s := range m {
		c, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return Bundle{}, err
		}
		b.Files[p] = c
	}
	return b, nil
}

func (b Bundle) Paths() []string {
	out := make([]string, 0, len(b.Files))
	for p := range b.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
