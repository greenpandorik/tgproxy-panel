package sitekit

import "testing"

func TestPresetsAreTheShippedCatalogue(t *testing.T) {
	ps := Presets()
	if len(ps) != 15 {
		t.Fatalf("presets: %d, want the 15 built-in sites", len(ps))
	}
	for _, p := range ps {
		if p.HTML == "" {
			t.Errorf("%s: no html", p.Name)
		}
		if p.Manifest.Category == "" || p.Manifest.Name == "" {
			t.Errorf("%s: manifest is missing name or category", p.Name)
		}
		if len(p.Manifest.Variables) == 0 {
			t.Errorf("%s: manifest declares no editable variables", p.Name)
		}
	}
	for _, gone := range []string{"blog", "docs", "portfolio", "product", "studio"} {
		if PresetNames()[gone] {
			t.Errorf("%s is a retired preset and must not be shipped", gone)
		}
	}
}
