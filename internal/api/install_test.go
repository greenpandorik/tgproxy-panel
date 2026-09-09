package api_test

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/config"
)

func TestInstallScriptAndRegister(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_ = os.MkdirAll(filepath.Join(h.Deps.Cfg.DataDir, "agent"), 0o755)
	_ = os.WriteFile(filepath.Join(h.Deps.Cfg.DataDir, "agent", "tgwp-agent-linux-amd64"), []byte("binary"), 0o755)
	_ = os.WriteFile(filepath.Join(h.Deps.Cfg.DataDir, "agent", "tgwp-agent-linux-amd64.sha256"), []byte("abc123\n"), 0o644)

	n, cmd := createNode(t, c, "n1.test")
	token := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(cmd)[1]

	anon := h.Anonymous()
	resp := anon.Get("/api/v1/install/" + token + ".sh")
	body, _ := io.ReadAll(resp.Body)
	// Every substituted value is single-quoted by nodeinstall's sq template func.
	if resp.StatusCode != 200 || !strings.Contains(string(body), "NODE_HOSTNAME='n1.test'") || !strings.Contains(string(body), "AGENT_SHA256='abc123'") {
		t.Fatalf("script %d: %.200s", resp.StatusCode, body)
	}
	if resp := anon.Get("/api/v1/install/agent/linux-amd64"); resp.StatusCode != 200 {
		t.Fatalf("agent download %d", resp.StatusCode)
	}

	var reg struct {
		NodeID string `json:"node_id"`
		Token  string `json:"token"`
	}
	resp = anon.Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test", "tproxy_version": "52a5feb", "agent_version": "0.1.0", "public_ip": "203.0.113.4"})
	if resp.StatusCode != 200 {
		t.Fatalf("register %d", resp.StatusCode)
	}
	anon.JSON(resp, &reg)
	if reg.NodeID != n.ID.String() || len(reg.Token) < 40 {
		t.Fatalf("reg %+v", reg)
	}
	// token is single-use
	if resp := anon.Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test"}); resp.StatusCode != 404 {
		t.Fatalf("second register expected 404, got %d", resp.StatusCode)
	}
	// presence auth accepts the issued token
	id, err := h.Presence.NodeByToken(t.Context(), reg.Token)
	if err != nil || id != n.ID {
		t.Fatalf("presence auth %v %v", id, err)
	}
	var got nodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.Status != "offline" {
		t.Fatalf("status after register = %s", got.Status)
	}
}

func TestInstallUnknownToken(t *testing.T) {
	h := apitest.New(t)
	if resp := h.Anonymous().Get("/api/v1/install/nope.sh"); resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestInstallScriptWithEmptyChecksumFile(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_ = os.MkdirAll(filepath.Join(h.Deps.Cfg.DataDir, "agent"), 0o755)
	_ = os.WriteFile(filepath.Join(h.Deps.Cfg.DataDir, "agent", "tgwp-agent-linux-amd64.sha256"), []byte("  \n"), 0o644)

	_, cmd := createNode(t, c, "n2.test")
	token := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(cmd)[1]
	resp := h.Anonymous().Get("/api/v1/install/" + token + ".sh")
	if resp.StatusCode != 200 {
		t.Fatalf("empty checksum file must not 500: got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "AGENT_SHA256=''") {
		t.Fatalf("expected an empty quoted checksum: %.200s", body)
	}
}

func TestInstallScriptPerEngine(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_ = os.MkdirAll(filepath.Join(h.Deps.Cfg.DataDir, "agent"), 0o755)
	_ = os.WriteFile(filepath.Join(h.Deps.Cfg.DataDir, "agent", "tgwp-agent-linux-amd64.sha256"), []byte("abc123\n"), 0o644)

	script := func(body map[string]any) string {
		t.Helper()
		var out struct {
			InstallCommand string `json:"install_command"`
		}
		resp := c.Post("/api/v1/nodes", body)
		if resp.StatusCode != 201 {
			t.Fatalf("create node %d", resp.StatusCode)
		}
		c.JSON(resp, &out)
		token := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(out.InstallCommand)[1]
		r := h.Anonymous().Get("/api/v1/install/" + token + ".sh")
		if r.StatusCode != 200 {
			t.Fatalf("install script %d", r.StatusCode)
		}
		b, _ := io.ReadAll(r.Body)
		return string(b)
	}

	tel := script(map[string]any{
		"name": "t1", "hostname": "t1.test", "acme_email": "a@b.co",
		"engine": "telemt", "tls_domain": "sni.test", "classic_port": 9443,
	})
	for _, want := range []string{
		"init-node --engine telemt", "TGWP_ENGINE=telemt", "TLS_DOMAIN='sni.test'", "CLASSIC_PORT='9443'",
		// The node's own profile is the first telemt user.
		"WEB_USER='default'",
		"TELEMT_VERSION='" + config.DefaultTelemtVersion + "'", "TELEMT_SHA256='" + h.Deps.Cfg.TelemtSHA256 + "'",
		"reverse_proxy 127.0.0.1:18080", `\"public_ip\":\"$PUBLIC_IP\"`,
	} {
		if !strings.Contains(tel, want) {
			t.Errorf("telemt script missing %q", want)
		}
	}
	if strings.Contains(tel, "tproxy-server") {
		t.Error("telemt script must not install the tproxy stack")
	}

	tp := script(map[string]any{"name": "t2", "hostname": "t2.test", "acme_email": "a@b.co", "engine": "tproxy"})
	for _, want := range []string{"install.sh --hostname 't2.test'", "TPROXY_COMMIT='" + config.DefaultTProxyCommit + "'"} {
		if !strings.Contains(tp, want) {
			t.Errorf("tproxy script missing %q", want)
		}
	}
	if strings.Contains(tp, "telemt") {
		t.Error("tproxy script must not mention telemt")
	}
}

// logCapture is a race-safe io.Writer for a slog handler the harness installs.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func TestInstallScriptUnpinnedTelemtIsLogged(t *testing.T) {
	logs := &logCapture{}
	h := apitest.New(t, func(d *api.Deps) {
		d.Cfg.TelemtSHA256 = ""
		d.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelError}))
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	n, cmd := createNode(t, c, "unpinned.test") // engine defaults to telemt
	token := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(cmd)[1]
	if resp := h.Anonymous().Get("/api/v1/install/" + token + ".sh"); resp.StatusCode != 500 {
		t.Fatalf("unpinned telemt build must not produce a script: got %d", resp.StatusCode)
	}
	got := logs.String()
	if !strings.Contains(got, "install script render") || !strings.Contains(got, n.ID.String()) {
		t.Fatalf("render failure not logged with the node id:\n%s", got)
	}
}
