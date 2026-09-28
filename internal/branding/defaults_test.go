package branding

import (
	"math"
	"strconv"
	"testing"
)

// relativeLuminance implements the WCAG 2.x formula for a #rrggbb colour.
func relativeLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("not a #rrggbb colour: %q", hex)
	}
	var ch [3]float64
	for i := range ch {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("bad hex %q: %v", hex, err)
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			ch[i] = c / 12.92
		} else {
			ch[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*ch[0] + 0.7152*ch[1] + 0.0722*ch[2]
}

func contrastRatio(t *testing.T, a, b string) float64 {
	t.Helper()
	la, lb := relativeLuminance(t, a), relativeLuminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestDefaultColors(t *testing.T) {
	for _, c := range []string{DefaultPrimaryColor, DefaultAccentColor, DarkGround, LightPrimaryColor, LightAccentColor, LightGround} {
		if !ValidateColor(c) {
			t.Errorf("%s is not a valid hex colour", c)
		}
	}
	for _, pair := range [][3]string{{DefaultPrimaryColor, DefaultAccentColor, DarkGround}, {LightPrimaryColor, LightAccentColor, LightGround}} {
		if got := contrastRatio(t, pair[0], pair[2]); got < 4.5 {
			t.Errorf("primary %s on %s: contrast %.2f < 4.5", pair[0], pair[2], got)
		}
		if got := contrastRatio(t, pair[1], pair[2]); got < 3 {
			t.Errorf("accent %s on %s: contrast %.2f < 3", pair[1], pair[2], got)
		}
	}
	// Sanity-check the implementation against the known black/white extreme.
	if got := contrastRatio(t, "#000000", "#ffffff"); math.Abs(got-21) > 0.01 {
		t.Errorf("black/white contrast %.2f, want 21", got)
	}
	if DefaultPanelName != "TGProxy Panel" {
		t.Errorf("panel name %q", DefaultPanelName)
	}
}

func TestThemeColors(t *testing.T) {
	for _, tc := range []struct {
		primary, accent, theme, wantP, wantA string
	}{
		{"#e23c92", "#12a198", "dark", DefaultPrimaryColor, DefaultAccentColor},
		{"#E23C92", "#12A198", "light", LightPrimaryColor, LightAccentColor},
		{"#3b82f6", "#22c55e", "light", LightPrimaryColor, LightAccentColor},
		{DefaultPrimaryColor, DefaultAccentColor, "light", LightPrimaryColor, LightAccentColor},
		{LightPrimaryColor, LightAccentColor, "dark", DefaultPrimaryColor, DefaultAccentColor},
		{"", "", "dark", DefaultPrimaryColor, DefaultAccentColor},
		{"#112233", "#abcdef", "light", "#112233", "#abcdef"},
		{"#112233", "", "dark", "#112233", DefaultAccentColor},
	} {
		p, a := ThemeColors(tc.primary, tc.accent, tc.theme)
		if p != tc.wantP || a != tc.wantA {
			t.Errorf("ThemeColors(%q, %q, %q) = %s, %s; want %s, %s", tc.primary, tc.accent, tc.theme, p, a, tc.wantP, tc.wantA)
		}
	}
}

func TestForeground(t *testing.T) {
	for color, want := range map[string]string{
		DefaultPrimaryColor: "#000000",
		LightPrimaryColor:   "#ffffff",
		"#1d4ed8":           "#ffffff",
		"#fff":              "#000000",
		"#000":              "#ffffff",
		"not-a-colour":      "#ffffff",
	} {
		if got := Foreground(color); got != want {
			t.Errorf("Foreground(%s) = %s, want %s", color, got, want)
		}
	}
}
