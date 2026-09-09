package api_test

import (
	"io"
	"strings"
	"testing"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/version"
)

type publicStatusResp struct {
	Version     string `json:"version"`
	NodesTotal  int    `json:"nodes_total"`
	NodesOnline int    `json:"nodes_online"`
	RelayCommit string `json:"relay_commit"`
}

func TestPublicStatusIsAnonymous(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")
	createNode(t, owner, "ams1.example.test")
	createNode(t, owner, "fra1.example.test")

	anon := h.Anonymous()
	resp := anon.Get("/api/v1/status/public")
	if resp.StatusCode != 200 {
		t.Fatalf("anonymous status: %d", resp.StatusCode)
	}
	var got publicStatusResp
	anon.JSON(resp, &got)

	if got.Version != version.Version {
		t.Fatalf("version = %q, want %q", got.Version, version.Version)
	}
	if got.NodesTotal != 2 {
		t.Fatalf("nodes_total = %d, want 2", got.NodesTotal)
	}
	// Both nodes are still "pending" (no heartbeat has arrived in a test).
	if got.NodesOnline != 0 {
		t.Fatalf("nodes_online = %d, want 0", got.NodesOnline)
	}
	if want := config.DefaultTProxyCommit[:7]; got.RelayCommit != want {
		t.Fatalf("relay_commit = %q, want the 7-char short form %q", got.RelayCommit, want)
	}
}

func TestPublicStatusLeaksNoHostnames(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")
	node, _ := createNode(t, owner, "secret-host.example.test")

	resp := h.Anonymous().Get("/api/v1/status/public")
	if resp.StatusCode != 200 {
		t.Fatalf("anonymous status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, secret := range []string{node.Hostname, node.Name, node.ID.String()} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("public status leaked %q: %s", secret, body)
		}
	}
}
