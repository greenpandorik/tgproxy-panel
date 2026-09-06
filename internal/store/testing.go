package store

import (
	"context"
	"os"
	"testing"
)

// OpenTest connects to TEST_DATABASE_URL, migrates and truncates every table. Skips when unset.
func OpenTest(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := Migrate(ctx, s.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err = s.Pool.Exec(ctx, `TRUNCATE backups, settings, subscription_tokens, alerts, node_stats_snapshots, key_stats_snapshots,
		audit_log, apply_jobs, node_sites, site_templates, key_bindings, profiles, access_keys, nodes,
		sessions, recovery_codes, admin_users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	_, _ = s.Pool.Exec(ctx, `DELETE FROM branding_profiles; INSERT INTO branding_profiles (name, is_active) VALUES ('default', true)`)
	t.Cleanup(s.Close)
	return s
}
