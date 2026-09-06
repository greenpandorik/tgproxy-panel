package sitekit

import "embed"

//go:embed presets/*/index.html
var presetFS embed.FS

type Preset struct {
	Name   string
	HTML   string
	Assets map[string][]byte
}

func Presets() []Preset {
	var out []Preset
	for _, name := range []string{"blog", "docs", "portfolio", "product", "studio"} {
		b, _ := presetFS.ReadFile("presets/" + name + "/index.html")
		out = append(out, Preset{Name: name, HTML: string(b), Assets: map[string][]byte{}})
	}
	return out
}
