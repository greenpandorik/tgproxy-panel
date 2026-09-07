// Package config loads panel configuration from environment variables.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	MasterKey        []byte
	MasterKeyVersion int
	OldMasterKeys    map[int][]byte // MASTER_KEY_V<n>=base64 for decrypting older versions
	SessionSecret    []byte
	PublicURL        string // e.g. https://panel.example.com, no trailing slash
	DataDir          string
	NodeDriver       string // gateway | mock
	MetricsToken     string
	TProxyCommit     string
	// TelemtVersion and TelemtSHA256 pin the telemt release that telemt nodes download in
	// their install script. The checksum is the only thing standing between a compromised
	// release host and a root shell on every new node, so it is not optional in production.
	TelemtVersion string
	TelemtSHA256  string
	FeatureTOTP   bool
	LogLevel      string
	ApplyInterval int // seconds
	OfflineAfter  int // seconds
	// GitHubRepo is the "owner/name" slug the update check reads release and
	// star metadata from; GitHubToken (optional) raises the API rate limit and
	// is sent as a bearer token, never logged. UpdateCheck=false disables the
	// outbound call entirely.
	GitHubRepo  string
	GitHubToken string
	UpdateCheck bool
}

const (
	DefaultTProxyCommit  = "52a5feb7fac38f68da5afef9cedd9b3bfc8473ca"
	DefaultTelemtVersion = "3.5.6"
	DefaultGitHubRepo    = "greenpandorik/tgproxy-panel"
)

func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	cfg := Config{
		HTTPAddr:      get("PANEL_HTTP_ADDR", ":8080"),
		DatabaseURL:   get("DATABASE_URL", ""),
		PublicURL:     strings.TrimRight(get("PANEL_PUBLIC_URL", "http://localhost:8080"), "/"),
		DataDir:       get("DATA_DIR", "./data"),
		NodeDriver:    get("NODE_DRIVER", "gateway"),
		MetricsToken:  get("METRICS_TOKEN", ""),
		TProxyCommit:  get("TPROXY_COMMIT", DefaultTProxyCommit),
		TelemtVersion: get("TELEMT_VERSION", DefaultTelemtVersion),
		TelemtSHA256:  strings.ToLower(get("TELEMT_SHA256_X86_64", "")),
		FeatureTOTP:   get("FEATURE_TOTP", "false") == "true",
		LogLevel:      get("LOG_LEVEL", "info"),
		OldMasterKeys: map[int][]byte{},
		GitHubRepo:    get("GITHUB_REPO", DefaultGitHubRepo),
		GitHubToken:   get("GITHUB_TOKEN", ""),
		// Opt-out is the literal word: anything else (including a typo) keeps the
		// default on, which is the safe direction for a feature that only reads.
		UpdateCheck: get("UPDATE_CHECK", "true") != "false",
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	var err error
	if cfg.MasterKey, err = key32(get("MASTER_KEY", "")); err != nil {
		return cfg, fmt.Errorf("MASTER_KEY: %w", err)
	}
	if cfg.SessionSecret, err = key32(get("SESSION_SECRET", "")); err != nil {
		return cfg, fmt.Errorf("SESSION_SECRET: %w", err)
	}
	if cfg.MasterKeyVersion, err = strconv.Atoi(get("MASTER_KEY_VERSION", "1")); err != nil || cfg.MasterKeyVersion < 1 {
		return cfg, errors.New("MASTER_KEY_VERSION must be a positive integer")
	}
	for v := 1; v < cfg.MasterKeyVersion; v++ {
		if raw := getenv(fmt.Sprintf("MASTER_KEY_V%d", v)); raw != "" {
			k, err := key32(raw)
			if err != nil {
				return cfg, fmt.Errorf("MASTER_KEY_V%d: %w", v, err)
			}
			cfg.OldMasterKeys[v] = k
		}
	}
	if cfg.ApplyInterval, err = strconv.Atoi(get("APPLY_INTERVAL", "45")); err != nil {
		return cfg, errors.New("APPLY_INTERVAL must be integer seconds")
	}
	if cfg.OfflineAfter, err = strconv.Atoi(get("OFFLINE_AFTER", "90")); err != nil {
		return cfg, errors.New("OFFLINE_AFTER must be integer seconds")
	}
	if cfg.NodeDriver != "gateway" && cfg.NodeDriver != "mock" {
		return cfg, errors.New("NODE_DRIVER must be gateway or mock")
	}
	// /metrics is mounted at the panel root, outside the auth group, and the reverse proxy
	// publishes it on the operator's domain. Fail closed rather than exposing node UUIDs and
	// key counts to anyone who can reach the panel. NODE_DRIVER=mock is test/dev only.
	if cfg.NodeDriver == "gateway" && cfg.MetricsToken == "" {
		return cfg, errors.New("METRICS_TOKEN is required when NODE_DRIVER=gateway (set it to a random secret; /metrics is served on the public domain)")
	}
	if !reCommit.MatchString(cfg.TProxyCommit) {
		return cfg, errors.New("TPROXY_COMMIT must be a git commit sha (7-40 lowercase hex characters)")
	}
	if !reSemver.MatchString(cfg.TelemtVersion) {
		return cfg, errors.New("TELEMT_VERSION must be a release version like 3.5.6")
	}
	if !reRepo.MatchString(cfg.GitHubRepo) {
		return cfg, errors.New("GITHUB_REPO must be an owner/name slug like greenpandorik/tgproxy-panel")
	}
	if cfg.TelemtSHA256 != "" && !reSHA256.MatchString(cfg.TelemtSHA256) {
		return cfg, errors.New("TELEMT_SHA256_X86_64 must be the 64 hex characters of the sha256 of telemt-x86_64-linux-gnu.tar.gz")
	}
	// Same reasoning as METRICS_TOKEN: a real deployment installs real nodes, and an
	// unverified download running as root on each of them is not something to discover later.
	if cfg.NodeDriver == "gateway" && cfg.TelemtSHA256 == "" {
		return cfg, errors.New("TELEMT_SHA256_X86_64 is required when NODE_DRIVER=gateway (sha256 of the telemt " + cfg.TelemtVersion + " release asset telemt-x86_64-linux-gnu.tar.gz; the node install script verifies the download against it)")
	}
	return cfg, nil
}

// reCommit constrains TPROXY_COMMIT: it is interpolated into the root-run installer script,
// so nothing but a hex commit id may ever reach it.
var reCommit = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// reSemver and reSHA256 constrain the telemt pin, which is interpolated into the same
// root-run installer script.
var (
	reSemver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// reRepo constrains GITHUB_REPO, which is interpolated into the URL the update
// check requests: one owner and one name, nothing that could add a path
// segment or a query.
var reRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func key32(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("required (base64 of 32 random bytes)")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("must be standard base64")
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("must decode to 32 bytes, got %d", len(raw))
	}
	return raw, nil
}
