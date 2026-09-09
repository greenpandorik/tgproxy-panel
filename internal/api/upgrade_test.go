package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tgwebproxy/internal/agent"
	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/config"
)

type upgradeManifestResp struct {
	Engine string `json:"engine"`
	Telemt *struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
		URL     string `json:"url"`
	} `json:"telemt"`
	TProxy *struct {
		Commit string `json:"commit"`
		Repo   string `json:"repo"`
	} `json:"tproxy"`
	Agent struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
		URL     string `json:"url"`
	} `json:"agent"`
}

func stageAgentBinary(t *testing.T, h *apitest.Harness, sum string) {
	t.Helper()
	dir := filepath.Join(h.Deps.Cfg.DataDir, "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tgwp-agent-linux-amd64"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tgwp-agent-linux-amd64.sha256"), []byte(sum+"  tgwp-agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func registerNode(t *testing.T, h *apitest.Harness, c *apitest.Client, host, engine string) (string, string) {
	t.Helper()
	var created struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
		InstallCommand string `json:"install_command"`
	}
	resp := c.Post("/api/v1/nodes", map[string]any{"name": host, "hostname": host, "acme_email": "a@b.co", "engine": engine})
	if resp.StatusCode != 201 {
		t.Fatalf("create %s node: %d", engine, resp.StatusCode)
	}
	c.JSON(resp, &created)
	return created.Node.ID, registerWithCommand(t, h, created.InstallCommand, host)
}

func registerWithCommand(t *testing.T, h *apitest.Harness, cmd, host string) string {
	t.Helper()
	token := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(cmd)[1]
	var reg struct {
		Token string `json:"token"`
	}
	anon := h.Anonymous()
	resp := anon.Post("/api/v1/install/"+token+"/register", map[string]string{
		"hostname": host, "public_ip": "203.0.113.7", "tproxy_version": "x", "agent_version": agent.Version,
	})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("register: %d %s", resp.StatusCode, b)
	}
	anon.JSON(resp, &reg)
	if reg.Token == "" {
		t.Fatal("register returned no node token")
	}
	return reg.Token
}

// getUpgrade issues the node's own request: no cookie, just the bearer token.
func getUpgrade(t *testing.T, h *apitest.Harness, token string) *http.Response {
	t.Helper()
	c := h.Anonymous()
	if token != "" {
		c.SetHeader("Authorization", "Bearer "+token)
	}
	return c.Get("/api/v1/node/upgrade")
}

func TestNodeUpgradeManifestTelemt(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	stageAgentBinary(t, h, "deadbeef")

	_, token := registerNode(t, h, c, "up1.test", "telemt")
	resp := getUpgrade(t, h, token)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upgrade manifest: %d %s", resp.StatusCode, b)
	}
	var got upgradeManifestResp
	h.Anonymous().JSON(resp, &got)

	if got.Engine != "telemt" || got.Telemt == nil || got.TProxy != nil {
		t.Fatalf("manifest %+v", got)
	}
	if got.Telemt.Version != config.DefaultTelemtVersion || got.Telemt.SHA256 != h.Deps.Cfg.TelemtSHA256 {
		t.Errorf("telemt pin %+v, want %s/%s", *got.Telemt, config.DefaultTelemtVersion, h.Deps.Cfg.TelemtSHA256)
	}
	if want := "https://github.com/telemt/telemt/releases/download/" + config.DefaultTelemtVersion + "/telemt-x86_64-linux-gnu.tar.gz"; got.Telemt.URL != want {
		t.Errorf("telemt url = %q, want %q", got.Telemt.URL, want)
	}
	if got.Agent.Version != agent.Version || got.Agent.SHA256 != "deadbeef" {
		t.Errorf("agent pin %+v", got.Agent)
	}
	// The agent binary comes from the install route, not from a second download path.
	if got.Agent.URL != "http://panel.test/api/v1/install/agent/linux-amd64" {
		t.Errorf("agent url = %q", got.Agent.URL)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
}

func TestNodeUpgradeManifestTProxy(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	stageAgentBinary(t, h, "abc123")

	_, token := registerNode(t, h, c, "up2.test", "tproxy")
	resp := getUpgrade(t, h, token)
	if resp.StatusCode != 200 {
		t.Fatalf("upgrade manifest: %d", resp.StatusCode)
	}
	var got upgradeManifestResp
	h.Anonymous().JSON(resp, &got)
	if got.Engine != "tproxy" || got.TProxy == nil || got.Telemt != nil {
		t.Fatalf("a tproxy node must get its own pins, not telemt's: %+v", got)
	}
	if got.TProxy.Commit != config.DefaultTProxyCommit {
		t.Errorf("tproxy commit = %q", got.TProxy.Commit)
	}
	if got.Agent.SHA256 != "abc123" {
		t.Errorf("agent sha = %q", got.Agent.SHA256)
	}
}

// TestNodeUpgradeRejectsBadTokens: a missing, unknown or superseded token is the same 401, byte for byte.
func TestNodeUpgradeRejectsBadTokens(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	stageAgentBinary(t, h, "abc123")

	id, old := registerNode(t, h, c, "up3.test", "telemt")
	if resp := getUpgrade(t, h, old); resp.StatusCode != 200 {
		t.Fatalf("the freshly issued token must work: %d", resp.StatusCode)
	}

	// Re-issue an install command and register again: the node now holds a different token.
	var cmd struct {
		Command string `json:"command"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+id+"/install-command"), &cmd)
	fresh := registerWithCommand(t, h, cmd.Command, "up3.test")
	if fresh == old {
		t.Fatal("re-registration must issue a new token")
	}
	if resp := getUpgrade(t, h, fresh); resp.StatusCode != 200 {
		t.Fatalf("the new token must work: %d", resp.StatusCode)
	}

	bodies := map[string]string{}
	for name, tok := range map[string]string{
		"revoked": old,
		"unknown": strings.Repeat("f", 64),
		"missing": "",
		"garbage": "not a token",
	} {
		resp := getUpgrade(t, h, tok)
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s token: got %d, want 401", name, resp.StatusCode)
		}
		bodies[name] = string(b)
	}
	for name, body := range bodies {
		if body != bodies["unknown"] {
			t.Errorf("%s token: body %q differs from the unknown-token body %q; the answer must not say which", name, body, bodies["unknown"])
		}
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		_ = json.Unmarshal([]byte(body), &e)
		if e.Error.Code != "unauthorized" {
			t.Errorf("%s token: error code %q", name, e.Error.Code)
		}
	}
}

func TestNodeUpgradeUnpinnedTelemtRefuses(t *testing.T) {
	h := apitest.New(t, func(d *api.Deps) { d.Cfg.TelemtSHA256 = "" })
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	stageAgentBinary(t, h, "abc123")

	_, token := registerNode(t, h, c, "up4.test", "telemt")
	resp := getUpgrade(t, h, token)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 500 {
		t.Fatalf("unpinned telemt must not produce a manifest: %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "unpinned_telemt") {
		t.Fatalf("body %q", b)
	}
}
