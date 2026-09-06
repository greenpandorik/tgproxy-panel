package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// Fixed loopback endpoints of a telemt node (spec §5). They are not configurable per node:
// Caddy, the firewall rules and the agent all assume these ports.
const (
	telemtWebListenIP   = "127.0.0.1"
	telemtWebListenPort = 18080
	telemtAPIListen     = "127.0.0.1:9091"
	telemtMetricsListen = "127.0.0.1:9090"
	telemtLoopbackCIDR  = "127.0.0.1/32"
	// telemtSynlimitBackend is the per-listener SYN rate limiter telemt installs itself
	// (spec §5, the MEKO fix). Only the Fake-TLS listener carries it: telemt rejects the
	// option on a WEB listener.
	telemtSynlimitBackend = "nftables"
)

// Default node paths, overridable for tests.
const (
	DefaultTelemtConfigPath = "/etc/telemt/telemt.toml"
	DefaultTelemtTokenPath  = "/etc/telemt/api.token"
	DefaultTelemtUnitPath   = "/etc/systemd/system/telemt.service"
	DefaultTelemtSiteDir    = "/var/lib/telemt/public"
	DefaultTelemtBin        = "/usr/local/bin/telemt"
	DefaultTelemtAPI        = "http://127.0.0.1:9091"
	DefaultTelemtDataDir    = "/var/lib/telemt"
	DefaultStateDir         = "/var/lib/tgwp-agent"
)

// TelemtServiceAccount is the unprivileged account the telemt unit runs as. The installer
// (Task 41) must create it before `init-node --engine telemt` runs:
//
//	useradd --system --home /var/lib/telemt --shell /usr/sbin/nologin telemt
//
// init-node chowns /etc/telemt and /var/lib/telemt to it and fails loudly if it is missing,
// because a telemt that cannot read its own config or write its config back (the control API
// mutates telemt.toml) would only fail later and less clearly.
const TelemtServiceAccount = "telemt"

// TelemtInitParams describes one telemt node as `agent init-node --engine telemt` sees it.
type TelemtInitParams struct {
	Hostname    string // public FQDN served by Caddy and used as the WEB vhost
	PublicIP    string // concrete public IP; telemt needs it for web.vhosts.public_addr
	TLSDomain   string // Fake-TLS SNI, normally the node's own hostname
	ClassicPort int    // Fake-TLS listener port
	WebUser     string // initial telemt user (the node's own service key)
	WebSecret   string // 32 hex chars
	SiteSrc     string // optional directory whose contents become the decoy site
	// NoSynlimit omits the Fake-TLS listener's `synlimit = "nftables"` line. The synlimit
	// rules are installed by telemt itself at startup and need CAP_NET_ADMIN plus a writable
	// netfilter namespace; where that is unavailable (an unprivileged container such as the
	// e2e fakenode) telemt refuses to start at all rather than degrade. Real nodes never set
	// this: the installer runs telemt with CAP_NET_ADMIN and the MEKO fix depends on it.
	NoSynlimit bool
	// NoTLSEmulation sets `censorship.tls_emulation = false`. telemt normally learns the
	// TLS fingerprint of the real tls_domain by connecting to it on 443, and a runtime
	// reload refuses to activate a generation whose TLS-front profiles are still the
	// built-in fallback ("TLS-front profiles are not ready for domains: ..."). On a test
	// bench the domain is fictional and resolves nowhere, so every apply would roll back.
	// Real nodes never set this: the emulation is what makes the Fake-TLS listener look
	// like the site it masks behind.
	NoTLSEmulation bool

	ConfigPath string
	TokenPath  string
	UnitPath   string
	SiteDir    string
	// DataDir is telemt's state directory (the unit's WorkingDirectory); SiteDir normally
	// lives inside it.
	DataDir string
	Binary  string
	// StateDir is the agent's state directory. init-node seeds the user fingerprint file
	// there so the first apply recognises the node's own user as already up to date.
	StateDir string
}

func (p *TelemtInitParams) withDefaults() {
	if p.ConfigPath == "" {
		p.ConfigPath = DefaultTelemtConfigPath
	}
	if p.TokenPath == "" {
		p.TokenPath = DefaultTelemtTokenPath
	}
	if p.UnitPath == "" {
		p.UnitPath = DefaultTelemtUnitPath
	}
	if p.SiteDir == "" {
		p.SiteDir = DefaultTelemtSiteDir
	}
	if p.DataDir == "" {
		p.DataDir = DefaultTelemtDataDir
	}
	if p.Binary == "" {
		p.Binary = DefaultTelemtBin
	}
	if p.StateDir == "" {
		p.StateDir = DefaultStateDir
	}
	if p.TLSDomain == "" {
		p.TLSDomain = p.Hostname
	}
	// ClassicPort is deliberately not defaulted: the CLI flag carries the default, so a zero
	// here means the caller passed one and must be told rather than silently corrected.
}

var (
	hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	userRe     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	hexRe      = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func validHostname(h string) bool {
	return len(h) <= 253 && hostnameRe.MatchString(strings.ToLower(h)) && h == strings.ToLower(h)
}

// validate rejects anything that could break out of a TOML string or produce a config telemt
// would refuse at startup. Everything that reaches the template is checked here.
func (p TelemtInitParams) validate() error {
	if !validHostname(p.Hostname) {
		return fmt.Errorf("invalid hostname %q", p.Hostname)
	}
	if !validHostname(p.TLSDomain) {
		return fmt.Errorf("invalid tls domain %q", p.TLSDomain)
	}
	if p.ClassicPort < 1 || p.ClassicPort > 65535 {
		return fmt.Errorf("classic port %d out of range", p.ClassicPort)
	}
	if p.ClassicPort == telemtWebListenPort {
		return fmt.Errorf("classic port %d collides with the WEB listener", p.ClassicPort)
	}
	if net.ParseIP(p.PublicIP) == nil {
		return fmt.Errorf("invalid public ip %q", p.PublicIP)
	}
	if !userRe.MatchString(p.WebUser) {
		return fmt.Errorf("invalid web user %q", p.WebUser)
	}
	if !hexRe.MatchString(p.WebSecret) {
		return errors.New("web secret must be 32 lowercase hex characters")
	}
	return nil
}

// publicAddr is the vhost's public socket address; IPv6 needs brackets.
func (p TelemtInitParams) publicAddr() string {
	ip := net.ParseIP(p.PublicIP)
	if ip.To4() == nil {
		return "[" + p.PublicIP + "]:443"
	}
	return p.PublicIP + ":443"
}

// telemtConfigTemplate mirrors spec §5. Every interpolated string goes through `q`
// (strconv.Quote), so the rendered file is valid TOML even if validation is ever loosened.
const telemtConfigTemplate = `[general]
use_middle_proxy = false
log_level = "normal"
[general.modes]
classic = false
secure = false
tls = true
[general.links]
show = []
public_host = {{q .Hostname}}
public_port = {{.ClassicPort}}
[server]
port = {{.ClassicPort}}
metrics_listen = {{q .MetricsListen}}
metrics_whitelist = [{{q .LoopbackCIDR}}]
[server.api]
enabled = true
listen = {{q .APIListen}}
whitelist = [{{q .LoopbackCIDR}}]
auth_header = {{q .APIToken}}
runtime_edge_enabled = true
[[server.listeners]]
ip = "0.0.0.0"
port = {{.ClassicPort}}
{{if .Synlimit}}synlimit = {{q .Synlimit}}
{{end}}[[server.listeners]]
ip = {{q .WebListenIP}}
port = {{.WebListenPort}}
transport = "web"
proxy_protocol = false
web_client_ip_source = "x_forwarded_for"
web_trusted_proxy_cidrs = [{{q .LoopbackCIDR}}]
[censorship]
tls_domain = {{q .TLSDomain}}
mask = true
unknown_sni_action = "mask"
{{if .NoTLSEmulation}}tls_emulation = false
{{end}}[access.users]
{{q .WebUser}} = {{q .WebSecret}}
[web]
enabled = true
carrier = "https"
carriers = ["websocket-lanes", "websocket", "https-lanes"]
[[web.vhosts]]
host = {{q .Hostname}}
public_addr = {{q .PublicAddr}}
[web.vhosts.decoy]
mode = "static_directory"
directory = {{q .SiteDir}}
index = "index.html"
[[web.vhosts.profiles]]
user = {{q .WebUser}}
secret_mode = "plain"
`

var telemtConfigTmpl = template.Must(template.New("telemt.toml").
	Funcs(template.FuncMap{"q": strconv.Quote}).Parse(telemtConfigTemplate))

// telemtUnitTemplate runs telemt unprivileged. The capability lines are what make that
// possible: CAP_NET_ADMIN for the nftables synlimit, CAP_NET_BIND_SERVICE so a node may use a
// privileged classic port. They are only meaningful because the service is not root — an
// ambient capability set on a root service is inert.
const telemtUnitTemplate = `[Unit]
Description=telemt proxy
Wants=network-online.target
After=network.target network-online.target

[Service]
Type=simple
User={{.Account}}
Group={{.Account}}
WorkingDirectory={{.DataDir}}
ExecStart={{.Binary}} {{.ConfigPath}}
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths={{.DataDir}} {{.ConfigDir}}
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
`

var telemtUnitTmpl = template.Must(template.New("telemt.service").Parse(telemtUnitTemplate))

const telemtPlaceholderIndex = "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><title>It works</title></head>\n<body><h1>It works</h1></body></html>\n"

// RenderTelemtConfig renders telemt.toml for the given node and API token.
func RenderTelemtConfig(p TelemtInitParams, apiToken string) ([]byte, error) {
	p.withDefaults()
	if err := p.validate(); err != nil {
		return nil, err
	}
	synlimit := telemtSynlimitBackend
	if p.NoSynlimit {
		synlimit = ""
	}
	data := map[string]any{
		"Synlimit": synlimit, "NoTLSEmulation": p.NoTLSEmulation,
		"Hostname": p.Hostname, "ClassicPort": p.ClassicPort, "TLSDomain": p.TLSDomain,
		"WebUser": p.WebUser, "WebSecret": p.WebSecret, "PublicAddr": p.publicAddr(),
		"SiteDir": p.SiteDir, "APIToken": apiToken,
		"MetricsListen": telemtMetricsListen, "APIListen": telemtAPIListen,
		"LoopbackCIDR": telemtLoopbackCIDR,
		"WebListenIP":  telemtWebListenIP, "WebListenPort": telemtWebListenPort,
	}
	var b strings.Builder
	if err := telemtConfigTmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// InitTelemtNode renders telemt.toml, ensures the API token file, installs the systemd unit and
// materialises the decoy site. It is idempotent: an existing token is reused so a re-run does
// not lock the agent out of the API. It returns the token file path; the token itself is never
// printed or logged.
func InitTelemtNode(ctx context.Context, ex Exec, p TelemtInitParams) (string, error) {
	p.withDefaults()
	if err := p.validate(); err != nil {
		return "", err
	}
	token, err := ensureAPIToken(p.TokenPath)
	if err != nil {
		return "", err
	}
	cfg, err := RenderTelemtConfig(p, token)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p.ConfigPath), 0o750); err != nil {
		return "", err
	}
	if err := writeAtomic(p.ConfigPath, cfg, 0o600); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p.UnitPath), 0o755); err != nil {
		return "", err
	}
	var unit strings.Builder
	if err := telemtUnitTmpl.Execute(&unit, map[string]any{
		"Account": TelemtServiceAccount, "Binary": p.Binary, "ConfigPath": p.ConfigPath,
		"DataDir": p.DataDir, "ConfigDir": filepath.Dir(p.ConfigPath),
	}); err != nil {
		return "", err
	}
	if err := writeAtomic(p.UnitPath, []byte(unit.String()), 0o644); err != nil {
		return "", err
	}
	if err := installTelemtSite(p.SiteSrc, p.SiteDir); err != nil {
		return "", err
	}
	if err := chownTelemtPaths(ctx, ex, p); err != nil {
		return "", err
	}
	// Record the secret we just wrote into telemt.toml. Without it the first apply would
	// re-PATCH this user's secret for no reason, because the API never reveals secrets.
	state := readTelemtState(p.StateDir)
	state.SecretHashes[p.WebUser] = secretFingerprint(p.WebSecret)
	if err := writeTelemtState(p.StateDir, state); err != nil {
		return "", err
	}
	return p.TokenPath, nil
}

// chownTelemtPaths hands telemt's config and state to its service account. The token file keeps
// mode 0600 and becomes telemt-owned; the agent reads it as root. A failure here is fatal: it
// almost always means the installer did not create the account (see TelemtServiceAccount).
func chownTelemtPaths(ctx context.Context, ex Exec, p TelemtInitParams) error {
	owner := TelemtServiceAccount + ":" + TelemtServiceAccount
	seen := map[string]bool{}
	for _, path := range []string{filepath.Dir(p.ConfigPath), p.DataDir, p.SiteDir} {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out, err := ex.Run(ctx, "chown", "-R", owner, path)
		if err != nil {
			return fmt.Errorf("chown -R %s %s: %s: %w (create the account first: useradd --system --home %s --shell /usr/sbin/nologin %s)",
				owner, path, strings.TrimSpace(string(out)), err, DefaultTelemtDataDir, TelemtServiceAccount)
		}
	}
	return nil
}

// ensureAPIToken returns the existing token or generates a fresh 32-byte one (0600).
func ensureAPIToken(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(raw)); tok != "" {
			return tok, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	if err := writeAtomic(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// installTelemtSite copies the bundle into the decoy directory. telemt validates the decoy at
// startup and requires the index file to exist, so an empty bundle still gets a placeholder.
func installTelemtSite(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if src != "" {
		if err := copyDir(src, dst); err != nil {
			return err
		}
	}
	index := filepath.Join(dst, "index.html")
	if _, err := os.Stat(index); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(index, []byte(telemtPlaceholderIndex), 0o644)
}
