package agent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tgwebproxy/internal/domain"
)

func telemtParams(t *testing.T) TelemtInitParams {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "bundle")
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<html>site</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "assets", "a.css"), []byte("p{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return TelemtInitParams{
		Hostname: "n1.example.com", PublicIP: "203.0.113.7", TLSDomain: "n1.example.com",
		ClassicPort: 8443, WebUser: "kabc123456789", WebSecret: "0123456789abcdef0123456789abcdef",
		SiteSrc:    src,
		ConfigPath: filepath.Join(dir, "etc", "telemt.toml"),
		TokenPath:  filepath.Join(dir, "etc", "api.token"),
		UnitPath:   filepath.Join(dir, "systemd", "telemt.service"),
		SiteDir:    filepath.Join(dir, "var", "public"),
		DataDir:    filepath.Join(dir, "var"),
		StateDir:   filepath.Join(dir, "state"),
		Binary:     "/usr/local/bin/telemt",
	}
}

func TestInitTelemtNodeRendersConfigTokenUnitAndSite(t *testing.T) {
	p := telemtParams(t)
	ex := &fakeExec{}
	tokenPath, err := InitTelemtNode(context.Background(), ex, p)
	if err != nil {
		t.Fatal(err)
	}
	if tokenPath != p.TokenPath {
		t.Fatalf("token path %q", tokenPath)
	}

	raw, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	toml := string(raw)
	for _, want := range []string{
		"[general]\nuse_middle_proxy = false\nlog_level = \"normal\"\n",
		"[general.modes]\nclassic = false\nsecure = false\ntls = true\n",
		"[general.links]\nshow = []\npublic_host = \"n1.example.com\"\npublic_port = 8443\n",
		"[server]\nport = 8443\nmetrics_listen = \"127.0.0.1:9090\"\nmetrics_whitelist = [\"127.0.0.1/32\"]\n",
		"[server.api]\nenabled = true\nlisten = \"127.0.0.1:9091\"\nwhitelist = [\"127.0.0.1/32\"]\n",
		"runtime_edge_enabled = true\n",
		"[[server.listeners]]\nip = \"0.0.0.0\"\nport = 8443\nsynlimit = \"nftables\"\n",
		"[[server.listeners]]\nip = \"127.0.0.1\"\nport = 18080\ntransport = \"web\"\nproxy_protocol = false\nweb_client_ip_source = \"x_forwarded_for\"\nweb_trusted_proxy_cidrs = [\"127.0.0.1/32\"]\n",
		"[censorship]\ntls_domain = \"n1.example.com\"\nmask = true\nunknown_sni_action = \"mask\"\n",
		"[access.users]\n\"kabc123456789\" = \"0123456789abcdef0123456789abcdef\"\n",
		"[web]\nenabled = true\ncarrier = \"https\"\ncarriers = [\"websocket-lanes\", \"websocket\", \"https-lanes\"]\n",
		"[[web.vhosts]]\nhost = \"n1.example.com\"\npublic_addr = \"203.0.113.7:443\"\n",
		"[web.vhosts.decoy]\nmode = \"static_directory\"\n",
		"index = \"index.html\"\n",
		"[[web.vhosts.profiles]]\nuser = \"kabc123456789\"\nsecret_mode = \"plain\"\n",
	} {
		if !strings.Contains(toml, want) {
			t.Fatalf("telemt.toml missing %q\n---\n%s", want, toml)
		}
	}
	if !strings.Contains(toml, "directory = "+strconv.Quote(p.SiteDir)) {
		t.Fatalf("decoy directory not set to the site dir:\n%s", toml)
	}
	if st, _ := os.Stat(p.ConfigPath); st.Mode().Perm() != 0o600 {
		t.Fatalf("telemt.toml mode %o", st.Mode().Perm())
	}

	token, err := os.ReadFile(p.TokenPath)
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSpace(string(token))
	if len(tok) != 64 {
		t.Fatalf("token length %d: %q", len(tok), tok)
	}
	if !strings.Contains(toml, "auth_header = "+strconv.Quote(tok)) {
		t.Fatalf("config auth_header does not match the token file")
	}
	if st, _ := os.Stat(p.TokenPath); st.Mode().Perm() != 0o600 {
		t.Fatalf("token mode %o", st.Mode().Perm())
	}

	unit, err := os.ReadFile(p.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=/usr/local/bin/telemt " + p.ConfigPath,
		"Restart=on-failure",
		// The capabilities are only effective because the service is unprivileged.
		"User=telemt\nGroup=telemt\n",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE",
		"CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=" + p.DataDir + " " + filepath.Dir(p.ConfigPath),
		"ProtectHome=true",
		"PrivateTmp=true",
		"WorkingDirectory=" + p.DataDir,
		"LimitNOFILE=1048576",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(string(unit), want) {
			t.Fatalf("unit missing %q\n---\n%s", want, unit)
		}
	}
	if strings.Contains(string(unit), "User=root") {
		t.Fatalf("telemt must not run as root:\n%s", unit)
	}

	for _, path := range []string{filepath.Dir(p.ConfigPath), p.DataDir, p.SiteDir} {
		if !ex.has("chown -R telemt:telemt " + path) {
			t.Fatalf("missing chown for %s: %v", path, ex.calls)
		}
	}

	// The node's own user must be recorded so the first apply does not re-set its secret.
	if readTelemtState(p.StateDir).SecretHashes[p.WebUser] != secretFingerprint(p.WebSecret) {
		t.Fatalf("agent state not seeded: %v", readTelemtState(p.StateDir))
	}

	idx, err := os.ReadFile(filepath.Join(p.SiteDir, "index.html"))
	if err != nil || string(idx) != "<html>site</html>" {
		t.Fatalf("site index: %q %v", idx, err)
	}
	if css, err := os.ReadFile(filepath.Join(p.SiteDir, "assets", "a.css")); err != nil || string(css) != "p{}" {
		t.Fatalf("site asset: %q %v", css, err)
	}
}

func TestInitTelemtNodeWithoutBundleStillHasAnIndex(t *testing.T) {
	p := telemtParams(t)
	p.SiteSrc = ""
	if _, err := InitTelemtNode(context.Background(), &fakeExec{}, p); err != nil {
		t.Fatal(err)
	}
	// telemt refuses to start when the static decoy directory has no index file.
	if _, err := os.Stat(filepath.Join(p.SiteDir, "index.html")); err != nil {
		t.Fatalf("placeholder index missing: %v", err)
	}
}

func TestInitTelemtNodeIsIdempotentButKeepsTheToken(t *testing.T) {
	p := telemtParams(t)
	if _, err := InitTelemtNode(context.Background(), &fakeExec{}, p); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p.TokenPath)
	if _, err := InitTelemtNode(context.Background(), &fakeExec{}, p); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p.TokenPath)
	if string(first) != string(second) {
		t.Fatal("re-running init-node rotated the API token, which would lock the agent out")
	}
}

func TestInitTelemtNodeValidatesInput(t *testing.T) {
	cases := map[string]func(*TelemtInitParams){
		"empty hostname": func(p *TelemtInitParams) { p.Hostname = "" },
		"hostname injection": func(p *TelemtInitParams) {
			p.Hostname = "n1\"\nmask = false\nx = \""
		},
		"bad tls domain": func(p *TelemtInitParams) { p.TLSDomain = "-bad-.example.com" },
		"bad ip":         func(p *TelemtInitParams) { p.PublicIP = "not-an-ip" },
		"port zero":      func(p *TelemtInitParams) { p.ClassicPort = 0 },
		"port too big":   func(p *TelemtInitParams) { p.ClassicPort = 70000 },
		"short secret":   func(p *TelemtInitParams) { p.WebSecret = "abcd" },
		"non hex secret": func(p *TelemtInitParams) { p.WebSecret = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" },
		"bad user":       func(p *TelemtInitParams) { p.WebUser = "bad user!" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := telemtParams(t)
			mutate(&p)
			if _, err := InitTelemtNode(context.Background(), &fakeExec{}, p); err == nil {
				t.Fatalf("expected a validation error for %s", name)
			}
			if _, err := os.Stat(p.ConfigPath); err == nil {
				t.Fatal("invalid input still wrote telemt.toml")
			}
		})
	}
}

func TestInitTelemtNodeFailsWhenTheServiceAccountIsMissing(t *testing.T) {
	p := telemtParams(t)
	ex := &fakeExec{failOn: "chown", failMsg: "chown: invalid user: 'telemt:telemt'"}
	_, err := InitTelemtNode(context.Background(), ex, p)
	if err == nil {
		t.Fatal("a missing telemt account must fail init-node loudly")
	}
	if !strings.Contains(err.Error(), "useradd") {
		t.Fatalf("the error must name the fix: %v", err)
	}
}

func TestInitTelemtNodeIPv6PublicAddr(t *testing.T) {
	p := telemtParams(t)
	p.PublicIP = "2001:db8::7"
	if _, err := InitTelemtNode(context.Background(), &fakeExec{}, p); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p.ConfigPath)
	if !strings.Contains(string(raw), `public_addr = "[2001:db8::7]:443"`) {
		t.Fatalf("ipv6 public_addr not bracketed:\n%s", raw)
	}
}

func TestRenderTelemtConfigNoSynlimit(t *testing.T) {
	p := telemtParams(t)
	p.NoSynlimit = true
	raw, err := RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	toml := string(raw)
	if strings.Contains(toml, "synlimit") {
		t.Fatalf("--no-synlimit still rendered a synlimit line:\n%s", toml)
	}
	// The listener itself must survive intact, with the WEB listener still after it.
	if !strings.Contains(toml, "[[server.listeners]]\nip = \"0.0.0.0\"\nport = 8443\n[[server.listeners]]\nip = \"127.0.0.1\"\n") {
		t.Fatalf("listener block malformed without synlimit:\n%s", toml)
	}

	p.NoSynlimit = false
	raw, err = RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "port = 8443\nsynlimit = \"nftables\"\n") {
		t.Fatalf("default config lost its synlimit line:\n%s", raw)
	}
}

func TestRenderTelemtConfigNoTLSEmulation(t *testing.T) {
	p := telemtParams(t)
	p.NoTLSEmulation = true
	raw, err := RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "unknown_sni_action = \"mask\"\ntls_emulation = false\n[access.users]\n") {
		t.Fatalf("censorship block malformed with --no-tls-emulation:\n%s", raw)
	}

	p.NoTLSEmulation = false
	raw, err = RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tls_emulation") {
		t.Fatalf("default config disabled tls emulation:\n%s", raw)
	}
}

// M4: `[access.users]` is a TOML table whose keys are usernames.
func TestRenderTelemtConfigQuotesTheAccessUserKey(t *testing.T) {
	p := telemtParams(t)
	p.WebUser = "k1.2"
	raw, err := RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	toml := string(raw)
	if !strings.Contains(toml, "[access.users]\n\"k1.2\" = \"") {
		t.Fatalf("a dotted username must be a quoted key, not a nested table:\n%s", toml)
	}
	if strings.Contains(toml, "\nk1.2 = ") {
		t.Fatalf("bare dotted key rendered:\n%s", toml)
	}
}

func TestRenderTelemtConfigCarriesTheWebPolicy(t *testing.T) {
	p := telemtParams(t)
	cfg, err := RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"carrier = \"https\"",
		"carriers = [\"websocket-lanes\", \"websocket\", \"https-lanes\"]",
		"carrier_learning = true",
		"carrier_negotiation_aggressiveness = \"conservative\"",
		"[web.limits]\nmax_http_handlers = 4",
		"[web.timeouts]",
		"carrier_negotiation_deadlines_secs = [3, 5, 8, 12]",
		"carrier_health_secs = 30",
		"carrier_learning_secs = 600",
		"bridge_request_secs = 10",
		"bridge_retry_secs = 90",
		"carrier_probe_coalesce_ms = 0",
	} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("config is missing %q:\n%s", want, cfg)
		}
	}
	// The negotiation order ships https-lanes, which telemt refuses below four HTTP handlers.
	if !strings.Contains(string(cfg), "max_http_handlers = 4") {
		t.Fatalf("https-lanes needs max_http_handlers >= 4:\n%s", cfg)
	}
}

func TestRenderTelemtConfigTakesTheOperatorsWebPolicy(t *testing.T) {
	p := telemtParams(t)
	p.WebPolicy = domain.DefaultWebPolicy()
	p.WebPolicy.Carriers = []domain.Carrier{domain.CarrierWebSocket}
	p.WebPolicy.CarrierLearning = false
	p.WebPolicy.Timeouts.CarrierHealthSecs = 45
	cfg, err := RenderTelemtConfig(p, "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"carriers = [\"websocket\"]", "carrier_learning = false", "carrier_health_secs = 45"} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("config is missing %q:\n%s", want, cfg)
		}
	}
}

func TestRenderTelemtConfigRefusesAPolicyTelemtWouldRefuse(t *testing.T) {
	p := telemtParams(t)
	p.WebPolicy = domain.DefaultWebPolicy()
	p.WebPolicy.Timeouts.NegotiationDeadlinesSecs = []int{3, 5, 8}
	if _, err := RenderTelemtConfig(p, "tok"); err == nil {
		t.Fatal("a config telemt refuses must not be written to the node")
	}
}
