package sitekit

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// presetByName returns the named built-in preset or fails the test.
func presetByName(t *testing.T, name string) Preset {
	t.Helper()
	for _, p := range Presets() {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no preset named %q", name)
	return Preset{}
}

func assetsOf(b Bundle) map[string][]byte {
	out := map[string][]byte{}
	for p, c := range b.Files {
		if p == "index.html" {
			continue
		}
		out[p] = c
	}
	return out
}

func TestUniquifyDeterministicAndSeedSensitive(t *testing.T) {
	p := presetByName(t, "product")
	b, rep, _ := Normalize(p.HTML, p.Assets)
	if len(rep.Errors) > 0 {
		t.Fatal(rep.Errors)
	}
	u1, err := Uniquify(b, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	u2, _ := Uniquify(b, "node-a")
	u3, _ := Uniquify(b, "node-b")
	if u1.Hash() != u2.Hash() {
		t.Fatal("not deterministic")
	}
	if u1.Hash() == u3.Hash() {
		t.Fatal("seed ignored")
	}
	idx := string(u1.Files["index.html"])
	if strings.Contains(idx, "data-block") || strings.Contains(idx, "data-variants") {
		t.Fatal("markers left")
	}
	if _, again, _ := Normalize(idx, assetsOf(u1)); len(again.Errors) > 0 {
		t.Fatalf("uniquified site invalid: %v", again.Errors)
	}
}

func TestUniquifyRenamesClassesConsistently(t *testing.T) {
	b := Bundle{Files: map[string][]byte{
		"index.html": []byte(`<html><head><link rel="stylesheet" href="/s.css"></head><body><main><section data-block="a" class="hero big">x</section><section data-block="b" class="cta">y</section></main></body></html>`),
		"s.css":      []byte(`.hero{color:red}.big{font-size:2em}.cta{color:blue}`),
	}}
	u, _ := Uniquify(b, "seed")
	idx := string(u.Files["index.html"])
	var css string
	for p, c := range u.Files {
		if strings.HasSuffix(p, ".css") {
			css = string(c)
		}
	}
	for _, old := range []string{"hero", "big", "cta"} {
		if strings.Contains(idx, `"`+old) || strings.Contains(css, "."+old+"{") {
			t.Fatalf("class %s not renamed", old)
		}
	}
	re := regexp.MustCompile(`class="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(idx, -1) {
		for _, cls := range strings.Fields(m[1]) {
			if !strings.Contains(css, "."+cls+"{") {
				t.Fatalf("class %s missing in css", cls)
			}
		}
	}
}

var reDataBlockAttr = regexp.MustCompile(`<[a-zA-Z][^>]*\sdata-block=`)

// reDataVariantsAttr matches a data-variants attribute on an element.
var reDataVariantsAttr = regexp.MustCompile(`\sdata-variants=`)

func TestAllPresetsNormalizeAndUniquify(t *testing.T) {
	for _, p := range Presets() {
		b, rep, err := Normalize(p.HTML, p.Assets)
		if err != nil || len(rep.Errors) > 0 {
			t.Fatalf("%s: %v %v", p.Name, err, rep.Errors)
		}
		if _, err := Uniquify(b, "x"); err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		mainStart := strings.Index(p.HTML, "<main")
		mainEnd := strings.Index(p.HTML, "</main>")
		if mainStart < 0 || mainEnd < 0 || mainEnd < mainStart {
			t.Fatalf("%s: no <main>...</main> found", p.Name)
		}
		if n := len(reDataBlockAttr.FindAllString(p.HTML[mainStart:mainEnd], -1)); n < 4 || n > 6 {
			t.Fatalf("%s: expected 4-6 data-block sections inside <main>, got %d", p.Name, n)
		}
		if n := len(reDataVariantsAttr.FindAllString(p.HTML, -1)); n < 3 {
			t.Fatalf("%s: expected at least 3 data-variants attributes, got %d", p.Name, n)
		}
	}
	if len(Presets()) != 5 {
		t.Fatalf("expected 5 presets, got %d", len(Presets()))
	}
}

func mainElementIDs(t *testing.T, doc []byte) []string {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	main := findElement(root, atom.Main)
	if main == nil {
		t.Fatal("no <main> in rendered document")
	}
	var ids []string
	for c := main.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		ids = append(ids, attr(c, "id"))
	}
	return ids
}

func TestUniquifyKeepsNonBlockChildrenInPlace(t *testing.T) {
	b := Bundle{Files: map[string][]byte{
		"index.html": []byte(`<html><body><main>` +
			`<h2 id="k1">head</h2>` +
			`<section data-block="a" id="a">a</section>` +
			`<p id="k2">between</p>` +
			`<section data-block="b" id="b">b</section>` +
			`<section data-block="c" id="c">c</section>` +
			`<div id="k3">tail</div>` +
			`</main></body></html>`),
	}}
	fixed := map[int]string{0: "k1", 2: "k2", 5: "k3"}
	movableSlots := []int{1, 3, 4}
	var everPermuted bool
	for _, seed := range []string{"n1", "n2", "n3", "n4", "n5", "n6", "n7", "n8"} {
		u, err := Uniquify(b, seed)
		if err != nil {
			t.Fatal(err)
		}
		ids := mainElementIDs(t, u.Files["index.html"])
		if len(ids) != 6 {
			t.Fatalf("seed %s: %d element children in <main>, want 6 (%v)", seed, len(ids), ids)
		}
		for i, want := range fixed {
			if ids[i] != want {
				t.Fatalf("seed %s: non-data-block child moved: position %d is %q, want %q (%v)", seed, i, ids[i], want, ids)
			}
		}
		got := make([]string, 0, 3)
		for _, i := range movableSlots {
			got = append(got, ids[i])
		}
		sorted := append([]string(nil), got...)
		sort.Strings(sorted)
		if strings.Join(sorted, "") != "abc" {
			t.Fatalf("seed %s: movable slots hold %v, want a permutation of a,b,c", seed, got)
		}
		if strings.Join(got, "") != "abc" {
			everPermuted = true
		}
	}
	if !everPermuted {
		t.Fatal("no seed permuted the data-block children; the shuffle is not running")
	}
}

// goldenProductHash pins Uniquify's output for (product preset, seed "golden-seed").
const goldenProductHash = "e82647f80d5659e529f6103e3291fd970b67196580a123aa61439200abf45448"

func TestUniquifyGoldenHashForFixedBundleAndSeed(t *testing.T) {
	p := presetByName(t, "product")
	b, rep, err := Normalize(p.HTML, p.Assets)
	if err != nil || len(rep.Errors) > 0 {
		t.Fatalf("normalize: %v %v", err, rep.Errors)
	}
	u, err := Uniquify(b, "golden-seed")
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Hash(); got != goldenProductHash {
		t.Fatalf("Uniquify(product, %q).Hash() = %s, want %s\n"+
			"Output changed: every node running this preset would be re-deployed and its relay "+
			"restarted on the next assign. Confirm the change is intended, then update goldenProductHash.",
			"golden-seed", got, goldenProductHash)
	}
}

func TestUniquifyLeavesCSSURLsAlone(t *testing.T) {
	css := `.hero{background:url(/logo.png)}` +
		`@font-face{font-family:x;src:url("/inter.woff2") format("woff2")}` +
		`.mark{list-style-image:URL( /bullet.svg )}` +
		`.quoted::after{content:".notaclass"}`
	b := Bundle{Files: map[string][]byte{
		"index.html": []byte(`<html><head><link rel="stylesheet" href="/s.css"></head><body><main>` +
			`<section data-block="a" class="hero">x</section><section data-block="b" class="mark quoted">y</section>` +
			`</main></body></html>`),
		"s.css": []byte(css),
	}}
	u, err := Uniquify(b, "seed")
	if err != nil {
		t.Fatal(err)
	}
	var out string
	for p, c := range u.Files {
		if strings.HasSuffix(p, ".css") {
			out = string(c)
		}
	}
	for _, want := range []string{`url(/logo.png)`, `url("/inter.woff2")`, `format("woff2")`, `URL( /bullet.svg )`, `content:".notaclass"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s was rewritten; css = %s", want, out)
		}
	}
	for _, old := range []string{".hero{", ".mark{", ".quoted:"} {
		if strings.Contains(out, old) {
			t.Fatalf("class selector %s not renamed; css = %s", old, out)
		}
	}
	// Every class left on an element must still select something in the stylesheet.
	idx := string(u.Files["index.html"])
	re := regexp.MustCompile(`class="([^"]+)"`)
	matches := re.FindAllStringSubmatch(idx, -1)
	if len(matches) != 2 {
		t.Fatalf("expected 2 class attributes in %s", idx)
	}
	for _, m := range matches {
		for _, cls := range strings.Fields(m[1]) {
			if !strings.Contains(out, "."+cls) {
				t.Fatalf("class %s missing from css %s", cls, out)
			}
		}
	}
}
