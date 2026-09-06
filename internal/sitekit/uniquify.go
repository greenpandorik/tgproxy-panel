package sitekit

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/rand"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// reCSSSelectorClass matches a class selector token inside a stylesheet: a leading dot
// followed by a CSS identifier. Only classes that actually appear in a stylesheet this way
// are candidates for renaming — a class used purely as a JS/behavioural hook (never styled)
// is left alone so external semantics never break.
var reCSSSelectorClass = regexp.MustCompile(`\.([A-Za-z_][\w-]*)`)

// maskedSpans reports the byte ranges of a stylesheet that must never be scanned for class
// selectors: url(...) bodies, quoted strings and /* comments */.
//
// The regex above matches any dot followed by a CSS identifier, so without this
// `background: url(/logo.png)` registers `png` as a class and the rename rewrites the
// reference to `url(/logo.c1a2b3c4)`. buildAssetMapping only ever renames .css/.js, so the
// file is still logo.png and the reference now points at nothing — silently, since the
// bundle stays valid. Anchoring on the preceding character instead is not an option:
// `ol.steps` and `a.link` are legitimate class selectors with an identifier right before
// the dot.
//
// Spans are returned in ascending order and never overlap.
func maskedSpans(css []byte) [][2]int {
	var spans [][2]int
	isIdent := func(b byte) bool {
		return b == '-' || b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	}
	// skipQuoted returns the index just past the string literal opening at i.
	skipQuoted := func(i int) int {
		q := css[i]
		j := i + 1
		for j < len(css) && css[j] != q {
			if css[j] == '\\' {
				j++
			}
			j++
		}
		if j < len(css) {
			j++
		}
		return j
	}
	for i := 0; i < len(css); {
		switch {
		case css[i] == '/' && i+1 < len(css) && css[i+1] == '*':
			end := len(css)
			if k := bytes.Index(css[i+2:], []byte("*/")); k >= 0 {
				end = i + 2 + k + 2
			}
			spans = append(spans, [2]int{i, end})
			i = end
		case css[i] == '"' || css[i] == '\'':
			j := skipQuoted(i)
			spans = append(spans, [2]int{i, j})
			i = j
		case (css[i] == 'u' || css[i] == 'U') && i+4 <= len(css) &&
			strings.EqualFold(string(css[i:i+4]), "url(") && (i == 0 || !isIdent(css[i-1])):
			j := i + 4
			for j < len(css) && css[j] != ')' {
				if css[j] == '"' || css[j] == '\'' {
					j = skipQuoted(j)
					continue
				}
				j++
			}
			if j < len(css) {
				j++
			}
			spans = append(spans, [2]int{i, j})
			i = j
		default:
			i++
		}
	}
	return spans
}

// inSpans reports whether byte offset i falls inside one of the (ascending, non-overlapping)
// masked ranges.
func inSpans(spans [][2]int, i int) bool {
	for _, s := range spans {
		if i < s[0] {
			return false
		}
		if i < s[1] {
			return true
		}
	}
	return false
}

// Uniquify rewrites a normalized Bundle so that the same template produces a byte-different,
// but equally valid, site per seed. This defeats fingerprinting of proxy sites by exact byte
// signature (identical markup/asset hashes across every node running the same template) while
// keeping the transformation fully deterministic for a given (bundle, seed) pair — re-running
// it for a node that already has this exact bundle assigned must reproduce the same bytes, or
// every re-assign would look like a change and force a needless relay restart.
//
// Transformations, in order:
//  1. Shuffle the direct children of <main> that carry data-block (elements without the
//     attribute keep their position).
//  2. Rename every CSS class that appears in both the HTML and a bundle stylesheet to a short
//     generated name, consistently across the HTML and every stylesheet.
//  3. For elements with data-variants="a|b|c", replace their text content with one variant.
//  4. Rename .css/.js asset files to <prefix>-<8 hex>.<ext> and rewrite <link href>/<script
//     src> references.
//  5. Strip the now-unneeded data-block and data-variants attributes.
func Uniquify(b Bundle, seed string) (Bundle, error) {
	idxSrc, ok := b.Files["index.html"]
	if !ok {
		return Bundle{}, errors.New("sitekit: bundle has no index.html")
	}
	doc, err := html.Parse(bytes.NewReader(idxSrc))
	if err != nil {
		return Bundle{}, err
	}

	sum := sha256.Sum256([]byte(seed))
	rng := rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(sum[:8])))) //nolint:gosec // deterministic per-seed shuffle, not security-sensitive

	shuffleMainBlocks(doc, rng)

	classMapping := buildClassMapping(b.Files, rng)
	assetMapping := buildAssetMapping(b.Files, rng)
	applyDocTransforms(doc, classMapping, assetMapping, rng)

	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return Bundle{}, err
	}

	files := make(map[string][]byte, len(b.Files))
	for p, c := range b.Files {
		if p == "index.html" {
			continue
		}
		if strings.HasSuffix(p, ".css") {
			c = renameClassesInCSS(c, classMapping)
		}
		newPath := p
		if np, ok := assetMapping[p]; ok {
			newPath = np
		}
		files[newPath] = c
	}
	files["index.html"] = out.Bytes()
	return Bundle{Files: files}, nil
}

// shuffleMainBlocks permutes which movable (data-block) node occupies each movable slot among
// <main>'s direct children, leaving every other child (including whitespace text nodes and
// elements without data-block) exactly where it was.
func shuffleMainBlocks(doc *html.Node, rng *rand.Rand) {
	main := findElement(doc, atom.Main)
	if main == nil {
		return
	}
	var children []*html.Node
	for c := main.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}

	var idxs []int
	var movable []*html.Node
	for i, c := range children {
		if c.Type == html.ElementNode && attr(c, "data-block") != "" {
			idxs = append(idxs, i)
			movable = append(movable, c)
		}
	}
	rng.Shuffle(len(movable), func(i, j int) { movable[i], movable[j] = movable[j], movable[i] })
	for k, i := range idxs {
		children[i] = movable[k]
	}

	for _, c := range children {
		main.RemoveChild(c)
	}
	for _, c := range children {
		main.AppendChild(c)
	}
}

func findElement(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, a); found != nil {
			return found
		}
	}
	return nil
}

// buildClassMapping assigns every class name that appears in any bundle stylesheet a short
// generated name. Names are assigned in sorted order of the original class name so the mapping
// is deterministic for a given (bundle, seed) regardless of Go's randomised map iteration order.
func buildClassMapping(files map[string][]byte, rng *rand.Rand) map[string]string {
	var cssPaths []string
	for p := range files {
		if strings.HasSuffix(p, ".css") {
			cssPaths = append(cssPaths, p)
		}
	}
	sort.Strings(cssPaths)

	set := map[string]bool{}
	for _, p := range cssPaths {
		css := files[p]
		spans := maskedSpans(css)
		for _, loc := range reCSSSelectorClass.FindAllSubmatchIndex(css, -1) {
			if inSpans(spans, loc[0]) {
				continue
			}
			set[string(css[loc[2]:loc[3]])] = true
		}
	}
	names := make([]string, 0, len(set))
	for c := range set {
		names = append(names, c)
	}
	sort.Strings(names)

	used := map[string]bool{}
	mapping := make(map[string]string, len(names))
	for _, c := range names {
		mapping[c] = nextToken(rng, used, "c", 3)
	}
	return mapping
}

// buildAssetMapping renames every .css/.js file (other than index.html) to a random name that
// carries no information about the original template.
func buildAssetMapping(files map[string][]byte, rng *rand.Rand) map[string]string {
	var paths []string
	for p := range files {
		if p == "index.html" {
			continue
		}
		if strings.HasSuffix(p, ".css") || strings.HasSuffix(p, ".js") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	used := map[string]bool{}
	mapping := make(map[string]string, len(paths))
	for _, p := range paths {
		ext := path.Ext(p)
		prefix := "style"
		if ext == ".js" {
			prefix = "script"
		}
		var newName string
		for {
			newName = prefix + "-" + nextToken(rng, nil, "", 4) + ext
			if !used[newName] {
				used[newName] = true
				break
			}
		}
		mapping[p] = newName
	}
	return mapping
}

// nextToken draws nBytes random bytes from rng and returns them hex-encoded with the given
// prefix, redrawing on collision against used (used may be nil to skip collision checking).
func nextToken(rng *rand.Rand, used map[string]bool, prefix string, nBytes int) string {
	buf := make([]byte, nBytes)
	for {
		_, _ = rng.Read(buf)
		tok := prefix + hex.EncodeToString(buf)
		if used == nil || !used[tok] {
			if used != nil {
				used[tok] = true
			}
			return tok
		}
	}
}

// renameClassesInCSS applies mapping to every class selector token in css, skipping the
// url()/string/comment spans maskedSpans reports so a filename extension is never mistaken
// for a class name.
func renameClassesInCSS(css []byte, mapping map[string]string) []byte {
	spans := maskedSpans(css)
	var out bytes.Buffer
	last := 0
	for _, loc := range reCSSSelectorClass.FindAllSubmatchIndex(css, -1) {
		if inSpans(spans, loc[0]) {
			continue
		}
		nn, ok := mapping[string(css[loc[2]:loc[3]])]
		if !ok {
			continue
		}
		out.Write(css[last:loc[0]])
		out.WriteString("." + nn)
		last = loc[1]
	}
	if last == 0 {
		return css
	}
	out.Write(css[last:])
	return out.Bytes()
}

func renameClassAttr(val string, mapping map[string]string) string {
	fields := strings.Fields(val)
	for i, f := range fields {
		if nn, ok := mapping[f]; ok {
			fields[i] = nn
		}
	}
	return strings.Join(fields, " ")
}

// applyDocTransforms walks the whole document once, renaming classes, resolving
// data-variants, rewriting asset references, and stripping the now-unneeded attributes.
func applyDocTransforms(doc *html.Node, classMapping, assetMapping map[string]string, rng *rand.Rand) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			kept := make([]html.Attribute, 0, len(n.Attr))
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "class":
					kept = append(kept, html.Attribute{Key: a.Key, Val: renameClassAttr(a.Val, classMapping)})
				case "data-block":
					// dropped: only used to drive the shuffle above
				case "data-variants":
					parts := strings.Split(a.Val, "|")
					if len(parts) > 0 {
						setText(n, parts[rng.Intn(len(parts))])
					}
					// dropped: the choice has been baked into the element's text
				case "href":
					if np, ok := assetMapping[assetPath(a.Val)]; ok {
						kept = append(kept, html.Attribute{Key: a.Key, Val: "/" + np})
					} else {
						kept = append(kept, a)
					}
				case "src":
					if np, ok := assetMapping[assetPath(a.Val)]; ok {
						kept = append(kept, html.Attribute{Key: a.Key, Val: "/" + np})
					} else {
						kept = append(kept, a)
					}
				default:
					kept = append(kept, a)
				}
			}
			n.Attr = kept
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

// setText replaces n's children with a single text node.
func setText(n *html.Node, text string) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		c = next
	}
	n.AppendChild(&html.Node{Type: html.TextNode, Data: text})
}
