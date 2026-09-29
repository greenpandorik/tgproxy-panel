package branding

import (
	"math"
	"strconv"
	"strings"
)

// Brand defaults: what a fresh install shows before an operator edits the active branding profile.
const (
	DefaultPanelName    = "TGProxy Panel"
	DefaultPrimaryColor = "#3fc0d6"
	DefaultAccentColor  = "#20c997"
	DarkGround          = "#171b21"
	LightPrimaryColor   = "#0b7285"
	LightAccentColor    = "#099268"
	LightGround         = "#f3f5f8"
)

var defaultPairs = [][2]string{
	{"#e23c92", "#12a198"},
	{"#3b82f6", "#22c55e"},
	{"#c4ed79", "#c0a8ed"},
	{"#365b46", "#a35336"},
	{DefaultPrimaryColor, DefaultAccentColor},
	{LightPrimaryColor, LightAccentColor},
}

// ThemeColors resolves the brand pair for a page in theme: a default pair follows the theme, a custom one is kept.
func ThemeColors(primary, accent, theme string) (string, string) {
	themed := [2]string{DefaultPrimaryColor, DefaultAccentColor}
	if theme == "light" {
		themed = [2]string{LightPrimaryColor, LightAccentColor}
	}
	p, a := strings.ToLower(primary), strings.ToLower(accent)
	if p == "" && a == "" {
		return themed[0], themed[1]
	}
	for _, pair := range defaultPairs {
		if p == pair[0] && a == pair[1] {
			return themed[0], themed[1]
		}
	}
	if primary == "" {
		primary = themed[0]
	}
	if accent == "" {
		accent = themed[1]
	}
	return primary, accent
}

// Foreground picks black or white text for a solid background of color, the same way the panel does.
func Foreground(color string) string {
	hex := strings.TrimPrefix(color, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return "#ffffff"
	}
	var lum float64
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		v, err := strconv.ParseUint(hex[2*i:2*i+2], 16, 8)
		if err != nil {
			return "#ffffff"
		}
		c := float64(v) / 255
		if c <= 0.04045 {
			c /= 12.92
		} else {
			c = math.Pow((c+0.055)/1.055, 2.4)
		}
		lum += weight * c
	}
	if 1.05/(lum+0.05) >= 4.5 {
		return "#ffffff"
	}
	return "#000000"
}
