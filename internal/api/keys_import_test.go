package api_test

import (
	"io"
	"strings"
	"testing"
)

func TestImportUsersWithTheirOwnSecrets(t *testing.T) {
	_, c, n := ownerWithNode(t)
	body := map[string]any{
		"type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()},
		"items": []map[string]string{
			{"label": "Anna", "owner_label": "@anna", "secret": "0123456789ABCDEF0123456789ABCDEF"},
			{"label": "Boris", "secret": "fedcba9876543210fedcba9876543210"},
		},
	}
	resp := c.Post("/api/v1/keys/import", body)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 || !strings.Contains(string(raw), "0123456789abcdef0123456789abcdef") || !strings.Contains(string(raw), `"subscription_url":"http`) {
		t.Fatalf("import %d %s", resp.StatusCode, raw)
	}
	body["items"] = []map[string]string{{"label": "Again", "secret": "fedcba9876543210fedcba9876543210"}}
	resp = c.Post("/api/v1/keys/import", body)
	raw, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 422 || !strings.Contains(string(raw), "items.0") {
		t.Fatalf("a secret in use must be refused per line: %d %s", resp.StatusCode, raw)
	}
}
