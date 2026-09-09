package store

import (
	"context"
	"encoding/json"

	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
)

// SeedPresets ensures every built-in site preset has a row in site_templates, creating any that are missing.
func SeedPresets(ctx context.Context, s *Store, presets []sitekit.Preset) error {
	for _, p := range presets {
		if _, err := s.Q.GetPresetByName(ctx, p.Name); err == nil {
			continue
		}
		raw, _ := json.Marshal(map[string]string{})
		if _, err := s.Q.CreateSiteTemplate(ctx, db.CreateSiteTemplateParams{Name: p.Name, Html: p.HTML, Assets: raw, IsPreset: true}); err != nil {
			return err
		}
	}
	return nil
}
