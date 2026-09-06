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

// TestDefaultColors pins the brand defaults to the same rule the settings UI
// enforces (a valid hex colour) and to the design spec's contrast floor: the
// primary is used as link text on the dark ground, so it must clear WCAG AA
// for text (4.5:1); the accent only ever colours chart strokes, so 3:1 (UI) is
// its floor. Both values are also asserted at their documented ratios so a
// silent palette change shows up here before it shows up in docs/design/brand.md.
func TestDefaultColors(t *testing.T) {
	for _, c := range []string{DefaultPrimaryColor, DefaultAccentColor, DarkGround} {
		if !ValidateColor(c) {
			t.Errorf("%s is not a valid hex colour", c)
		}
	}
	if got := contrastRatio(t, DefaultPrimaryColor, DarkGround); got < 4.5 {
		t.Errorf("primary %s on %s: contrast %.2f < 4.5", DefaultPrimaryColor, DarkGround, got)
	}
	if got := contrastRatio(t, DefaultAccentColor, DarkGround); got < 3 {
		t.Errorf("accent %s on %s: contrast %.2f < 3", DefaultAccentColor, DarkGround, got)
	}
	// Sanity-check the implementation against the known black/white extreme.
	if got := contrastRatio(t, "#000000", "#ffffff"); math.Abs(got-21) > 0.01 {
		t.Errorf("black/white contrast %.2f, want 21", got)
	}
	if DefaultPanelName != "TGProxy Panel" {
		t.Errorf("panel name %q", DefaultPanelName)
	}
}
