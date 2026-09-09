package sitekit

import (
	"embed"
	"encoding/json"
	"sort"
)

//go:embed presets/*/index.html presets/*/manifest.json
var presetFS embed.FS

type Preset struct {
	Name     string
	HTML     string
	Assets   map[string][]byte
	Manifest PresetManifest
}

// PresetManifest is the editable description shipped beside a built-in site.
type PresetManifest struct {
	Name      string            `json:"name"`
	Category  string            `json:"category"`
	Variables map[string]string `json:"variables"`
}

// Presets reads every built-in site out of the embedded set. The list is the catalogue: a
// name that is not here is not offered, and SeedPresets removes its row.
func Presets() []Preset {
	dirs, err := presetFS.ReadDir("presets")
	if err != nil {
		return nil
	}
	out := make([]Preset, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		html, err := presetFS.ReadFile("presets/" + d.Name() + "/index.html")
		if err != nil {
			continue
		}
		p := Preset{Name: d.Name(), HTML: string(html), Assets: map[string][]byte{}}
		if raw, err := presetFS.ReadFile("presets/" + d.Name() + "/manifest.json"); err == nil {
			_ = json.Unmarshal(raw, &p.Manifest)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PresetNames is the catalogue as a set, for callers pruning what is no longer shipped.
func PresetNames() map[string]bool {
	names := map[string]bool{}
	for _, p := range Presets() {
		names[p.Name] = true
	}
	return names
}
