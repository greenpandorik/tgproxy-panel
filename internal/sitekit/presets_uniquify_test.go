package sitekit

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func mainOrderSignature(t *testing.T, doc []byte) string {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	main := findElement(root, atom.Main)
	if main == nil {
		t.Fatal("no <main> in rendered document")
	}
	var parts []string
	for c := main.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		parts = append(parts, strings.Join(strings.Fields(deepTextOf(c)), " "))
	}
	return strings.Join(parts, "\n")
}

func deepTextOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	s := strings.Join(strings.Fields(b.String()), " ")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

func TestEveryPresetDiffersBetweenNodes(t *testing.T) {
	nodeIDs := []string{
		"7c2f1a44-6f0e-4a3d-9b21-0e5c8a1d4f60",
		"1b93de07-2c85-41af-8f6a-77c0b9e2a318",
		"e40a5c6b-9d17-4c02-a5e8-3b6f1027dd94",
		"a8f30512-4e6b-49d7-8c11-52ba7d9e0c6f",
		"36d7b8e1-0a4f-4d59-9e73-c81a6f452b07",
		"c51e2740-8b6d-4f18-a09c-2d47e93b6a15",
		"9ad46f83-1c50-4b27-b6e4-08f35d71c9a2",
		"52bc907e-3f41-4ea6-95d0-6a1c48b7f2e3",
	}

	for _, p := range Presets() {
		b, rep, err := Normalize(p.HTML, p.Assets)
		if err != nil || len(rep.Errors) > 0 {
			t.Fatalf("%s: normalize: %v %v", p.Name, err, rep.Errors)
		}

		hashes := map[string]bool{}
		orders := map[string]bool{}
		for _, id := range nodeIDs {
			u, err := Uniquify(b, id)
			if err != nil {
				t.Fatalf("%s/%s: %v", p.Name, id, err)
			}
			idx := u.Files["index.html"]
			if bytes.Contains(idx, []byte("data-block")) || bytes.Contains(idx, []byte("data-variants")) {
				t.Fatalf("%s/%s: markers survived into the served page", p.Name, id)
			}
			if _, again, err := Normalize(string(idx), assetsOf(u)); err != nil || len(again.Errors) > 0 {
				t.Fatalf("%s/%s: uniquified page no longer valid: %v %v", p.Name, id, err, again.Errors)
			}
			if again, err := Uniquify(b, id); err != nil || again.Hash() != u.Hash() {
				t.Fatalf("%s/%s: not deterministic", p.Name, id)
			}
			hashes[u.Hash()] = true
			orders[mainOrderSignature(t, idx)] = true
		}

		if len(hashes) < len(nodeIDs) {
			t.Errorf("%s: %d node ids produced only %d distinct pages", p.Name, len(nodeIDs), len(hashes))
		}
		if len(orders) < 2 {
			t.Errorf("%s: section order never varied across %d node ids", p.Name, len(nodeIDs))
		}
	}
}

func shapeOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		b.WriteString(n.Data)
		b.WriteByte('(')
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		b.WriteByte(')')
	}
	walk(n)
	return b.String()
}

func mainShapes(t *testing.T, doc []byte) []string {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	main := findElement(root, atom.Main)
	if main == nil {
		t.Fatal("no <main> in rendered document")
	}
	var out []string
	for c := main.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, shapeOf(c))
		}
	}
	return out
}

func TestPresetPagesSurviveShuffleStructurally(t *testing.T) {
	for _, p := range Presets() {
		b, rep, err := Normalize(p.HTML, p.Assets)
		if err != nil || len(rep.Errors) > 0 {
			t.Fatalf("%s: normalize: %v %v", p.Name, err, rep.Errors)
		}
		base := mainShapes(t, b.Files["index.html"])
		for _, id := range []string{"node-alpha", "node-beta", "node-gamma"} {
			u, err := Uniquify(b, id)
			if err != nil {
				t.Fatal(err)
			}
			got := mainShapes(t, u.Files["index.html"])
			if len(got) != len(base) {
				t.Fatalf("%s/%s: <main> has %d element children, want %d", p.Name, id, len(got), len(base))
			}
			seen := map[string]int{}
			for _, s := range base {
				seen[s]++
			}
			for _, s := range got {
				seen[s]--
			}
			for s, n := range seen {
				if n != 0 {
					t.Fatalf("%s/%s: a section was lost or duplicated by the shuffle (%d of %.40q)", p.Name, id, n, s)
				}
			}
		}
	}
}
