package api

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestManagementOpenAPICatalog(t *testing.T) {
	doc := ManagementOpenAPI()
	if doc["openapi"] != "3.0.3" {
		t.Fatal("expected OpenAPI 3.0.3")
	}
	paths := doc["paths"].(map[string]any)
	for _, route := range ManagementAPIRoutes() {
		item, ok := paths[route.Path].(map[string]any)
		if !ok {
			t.Fatalf("missing route %s", route.Path)
		}
		op := item[strings.ToLower(route.Method)].(map[string]any)
		if op["x-required-scope"] != route.Scope || op["x-owner-only"] != route.OwnerOnly {
			t.Fatalf("permission contract differs for %s %s", route.Method, route.Path)
		}
	}
	for _, path := range []string{"/api/v1/admins", "/api/v1/me/password", "/api/v1/backups", "/api/v1/subscription-service/token"} {
		if _, exists := paths[path]; exists {
			t.Fatalf("session-only privileged operation advertised as automation: %s", path)
		}
	}
}

func TestManagementOpenAPIRequestContracts(t *testing.T) {
	paths := ManagementOpenAPI()["paths"].(map[string]any)
	post := paths["/api/v1/keys"].(map[string]any)["post"].(map[string]any)
	body := post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	props := body["properties"].(map[string]any)
	types := props["type"].(map[string]any)["enum"].([]string)
	if len(types) != 2 || types[0] != "PERSONAL" || types[1] != "SHARED" {
		t.Fatalf("wrong key type enum: %v", types)
	}
	if props["node_ids"].(map[string]any)["minItems"] != 1 {
		t.Fatal("creation must require a node")
	}
	if props["expires_at"].(map[string]any)["format"] != "date-time" {
		t.Fatal("expiration must use RFC3339")
	}
	apply := paths["/api/v1/nodes/{id}/apply"].(map[string]any)["post"].(map[string]any)
	if _, ok := apply["responses"].(map[string]any)["202"]; !ok {
		t.Fatal("apply queues asynchronously and must describe 202")
	}
}

func TestManagementOpenAPICommittedArtifact(t *testing.T) {
	want, err := json.MarshalIndent(ManagementOpenAPI(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	got, err := os.ReadFile("../../docs/api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("API contract is stale: run go run ./cmd/api-spec")
	}
}
