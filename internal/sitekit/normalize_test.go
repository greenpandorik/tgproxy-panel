package sitekit

import (
	"strings"
	"testing"
)

func TestNormalizeExtractsInlineStyleAndScript(t *testing.T) {
	html := `<!doctype html><html><head><title>x</title><style>body{color:red}</style></head>
<body><h1 style="margin:0">Hi</h1><button id="b">Go</button><script>document.getElementById("b").addEventListener("click",()=>alert(1))</script></body></html>`
	b, rep, err := Normalize(html, nil)
	if err != nil || len(rep.Errors) != 0 {
		t.Fatalf("err %v report %+v", err, rep)
	}
	idx := string(b.Files["index.html"])
	if strings.Contains(idx, "<style") || strings.Contains(idx, "style=") || strings.Contains(idx, "alert(1)") {
		t.Fatalf("inline content left in index: %s", idx)
	}
	var css, js int
	for p := range b.Files {
		switch {
		case strings.HasSuffix(p, ".css"):
			css++
		case strings.HasSuffix(p, ".js"):
			js++
		}
	}
	if css != 1 || js != 1 {
		t.Fatalf("expected 1 css and 1 js, got %d/%d: %v", css, js, keys(b.Files))
	}
	if !strings.Contains(idx, `<link rel="stylesheet" href="/`) || !strings.Contains(idx, `<script src="/`) {
		t.Fatalf("no references inserted: %s", idx)
	}
	if len(rep.Warnings) == 0 {
		t.Fatal("expected warnings about extraction")
	}
}

func TestNormalizeRejectsForbidden(t *testing.T) {
	cases := map[string]string{
		"external script": `<html><body><script src="https://cdn.x/y.js"></script></body></html>`,
		"external css":    `<html><head><link rel="stylesheet" href="//fonts.googleapis.com/x.css"></head></html>`,
		"inline handler":  `<html><body><a onclick="x()">a</a></body></html>`,
		"javascript href": `<html><body><a href="javascript:void(0)">a</a></body></html>`,
		"iframe":          `<html><body><iframe src="/x"></iframe></body></html>`,
		"form":            `<html><body><form action="/x"></form></body></html>`,
		"external img":    `<html><body><img src="http://x.y/a.png"></body></html>`,
		"css import":      `<html><head><style>@import url("https://x/y.css");</style></head></html>`,
		"service worker":  `<html><body><script>navigator.serviceWorker.register("/sw.js")</script></body></html>`,
	}
	for name, html := range cases {
		_, rep, err := Normalize(html, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rep.Errors) == 0 {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeKeepsLocalAssets(t *testing.T) {
	html := `<html><head><link rel="stylesheet" href="/a.css"></head><body><img src="/i.png"><a href="mailto:a@b.c">m</a><a href="/about">about</a></body></html>`
	b, rep, _ := Normalize(html, map[string][]byte{"a.css": []byte("p{}"), "i.png": {1, 2}})
	if len(rep.Errors) != 0 || len(b.Files) != 3 {
		t.Fatalf("report %+v files %v", rep, keys(b.Files))
	}
}

func TestNormalizeMissingAssetIsError(t *testing.T) {
	_, rep, _ := Normalize(`<html><head><link rel="stylesheet" href="/missing.css"></head></html>`, nil)
	if len(rep.Errors) == 0 {
		t.Fatal("missing local asset must be an error")
	}
}

func TestBundleHashAndJSON(t *testing.T) {
	b := Bundle{Files: map[string][]byte{"index.html": []byte("a"), "s.css": []byte("b")}}
	h1 := b.Hash()
	b2, err := BundleFromJSON(b.JSON())
	if err != nil || b2.Hash() != h1 {
		t.Fatalf("json round trip: %v", err)
	}
	b.Files["s.css"] = []byte("c")
	if b.Hash() == h1 {
		t.Fatal("hash must change")
	}
}

func TestPresetsNormalizeClean(t *testing.T) {
	for _, p := range Presets() {
		_, rep, err := Normalize(p.HTML, p.Assets)
		if err != nil || len(rep.Errors) > 0 {
			t.Fatalf("preset %s: %v %+v", p.Name, err, rep.Errors)
		}
	}
}

func TestNormalizeRejectsAssetPathTraversal(t *testing.T) {
	cases := []string{"../../etc/passwd", "a/../../b.css", "/../x.css"}
	for _, p := range cases {
		_, rep, err := Normalize(`<html><head></head><body></body></html>`, map[string][]byte{p: []byte("x")})
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if len(rep.Errors) == 0 {
			t.Errorf("%s: expected a path-traversal error", p)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNormalizeIsDeterministic(t *testing.T) {
	html := `<!doctype html><html><head><title>x</title><style>body{color:red}</style></head>
<body><h1 style="margin:0">Hi</h1><p style="margin:0">two</p><script>console.log(1)</script></body></html>`
	assets := map[string][]byte{"a.css": []byte("p{}"), "b.png": {1, 2, 3}}
	first, _, err := Normalize(html, assets)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, _, err := Normalize(html, assets)
		if err != nil {
			t.Fatal(err)
		}
		if again.Hash() != first.Hash() {
			t.Fatalf("Normalize is not deterministic: %s != %s (files %v vs %v)", again.Hash(), first.Hash(), keys(again.Files), keys(first.Files))
		}
	}
	// Two style attributes with identical content must still get distinct classes.
	idx := string(first.Files["index.html"])
	classes := map[string]bool{}
	for _, part := range strings.Split(idx, `class="`)[1:] {
		classes[strings.SplitN(part, `"`, 2)[0]] = true
	}
	if len(classes) != 2 {
		t.Fatalf("expected 2 distinct generated classes, got %v in %s", classes, idx)
	}
	// A different input must produce a different bundle.
	other, _, _ := Normalize(strings.Replace(html, "color:red", "color:blue", 1), assets)
	if other.Hash() == first.Hash() {
		t.Fatal("different input produced the same hash")
	}
}

func TestAssetPathAndSanitizeAgree(t *testing.T) {
	for _, raw := range []string{"a.css", "./a.css", "/a.css", "css/../a.css.map", "sub/./b.js", "/sub//b.js"} {
		clean, ok := sanitizeAssetPath(raw)
		if !ok {
			continue // traversal is rejected on both sides
		}
		if got := assetPath(raw); got != clean {
			t.Errorf("%q: assetPath = %q, sanitizeAssetPath = %q", raw, got, clean)
		}
	}
	for _, bad := range []string{"", " ", "/", "../secret", "a/../../b"} {
		if _, ok := sanitizeAssetPath(bad); ok {
			t.Errorf("%q accepted as an asset key", bad)
		}
	}
}

func TestNormalizeAcceptsDotSlashReference(t *testing.T) {
	html := `<html><head><link rel="stylesheet" href="./a.css"></head><body><main><p>x</p></main></body></html>`
	_, rep, err := Normalize(html, map[string][]byte{"./a.css": []byte(".x{color:red}")})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rep.Errors {
		if strings.Contains(e, "missing from assets") {
			t.Fatalf("./a.css reported missing though it is in the bundle: %v", rep.Errors)
		}
	}
}

func TestNormalizeStillReportsTrulyMissingReference(t *testing.T) {
	html := `<html><head><link rel="stylesheet" href="/nope.css"></head><body><main><p>x</p></main></body></html>`
	_, rep, err := Normalize(html, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range rep.Errors {
		if strings.Contains(e, "missing from assets") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a genuinely missing reference was not reported: %v", rep.Errors)
	}
}
