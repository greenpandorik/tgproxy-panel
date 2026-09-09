package store

import (
	"context"
	"encoding/json"

	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
)

// SeedPresets makes the built-in catalogue authoritative: every shipped preset gets a row,
// and preset rows for sites no longer shipped are removed so they stop being offered. A node
// that was using a retired one keeps serving it - node_sites holds the node's own copy of the
// bundle, and its template reference is dropped rather than its site. Custom templates are
// never touched.
func SeedPresets(ctx context.Context, s *Store, presets []sitekit.Preset) error {
	names := make([]string, 0, len(presets))
	for _, p := range presets {
		names = append(names, p.Name)
		if _, err := s.Q.GetPresetByName(ctx, p.Name); err == nil {
			continue
		}
		raw, _ := json.Marshal(map[string]string{})
		if _, err := s.Q.CreateSiteTemplate(ctx, db.CreateSiteTemplateParams{Name: p.Name, Html: p.HTML, Assets: raw, IsPreset: true}); err != nil {
			return err
		}
	}
	if len(names) == 0 {
		return nil
	}
	_, err := s.Q.DeleteRetiredPresets(ctx, names)
	return err
}
