// Package agent runs on a node and executes panel commands.
package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"tgwebproxy/internal/telemt"
)

// Version is stamped from the release tag at link time.
var Version = "0.1.0"

// DefaultReloadWait is the budget telemtReload polls a reload for: the whole drain window plus
// 30s of slack for preparation, activation and the poll interval itself.
const DefaultReloadWait = telemt.ReloadDrainSecs*time.Second + 30*time.Second

// Node engines. The engine is fixed when the node is created and selects the agent code path:
// tproxy is the tproxy-server + official MTProxy stack, telemt is a single telemt process
// driven through its control API.
const (
	EngineTProxy = "tproxy"
	EngineTelemt = "telemt"
)

type Config struct {
	PanelURL, Token, StateDir, TProxyBin, ConfigPath, ProfilesPath, MTProxyEnvPath, SiteDir string
	RelayAdminURL, MTProxyStatsURL, TProxyVersion                                           string
	HealthWait                                                                              time.Duration

	// ReloadWait bounds a telemt runtime reload. It is deliberately *not* HealthWait: a
	// draining reload is non-terminal for up to telemt.ReloadDrainSecs while old sessions
	// finish, so reusing the (much shorter) health budget would make every apply on a node
	// with live traffic time out and roll a successful change back.
	ReloadWait time.Duration

	// Engine is EngineTProxy or EngineTelemt.
	Engine string
	// TelemtAPIToken is the exact Authorization value telemt's control API expects. It is
	// read from TGWP_TELEMT_API_TOKEN or the token file and must never be logged.
	TelemtAPI, TelemtAPIToken, TelemtConfigPath, TelemtSiteDir, TelemtBin, TelemtMetricsURL string
}

func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	cfg := Config{
		PanelURL: strings.TrimRight(get("TGWP_PANEL_URL", ""), "/"), Token: get("TGWP_TOKEN", ""),
		StateDir: get("TGWP_STATE_DIR", DefaultStateDir), TProxyBin: get("TGWP_TPROXY_BIN", "/usr/local/bin/tproxy-server"),
		ConfigPath: get("TGWP_CONFIG", "/etc/tproxy-server/config.json"), ProfilesPath: get("TGWP_PROFILES", "/etc/tproxy-server/profiles.json"),
		MTProxyEnvPath: get("TGWP_MTPROXY_ENV", "/etc/mtproxy/mtproxy.env"), SiteDir: get("TGWP_SITE_DIR", "/srv/tproxy-site"),
		RelayAdminURL: get("TGWP_RELAY_ADMIN", "http://127.0.0.1:8081"), MTProxyStatsURL: get("TGWP_MTPROXY_STATS", "http://127.0.0.1:8888/stats"),
		TProxyVersion: get("TGWP_TPROXY_VERSION", "unknown"), HealthWait: 20 * time.Second,
		ReloadWait:       DefaultReloadWait,
		Engine:           get("TGWP_ENGINE", EngineTProxy),
		TelemtAPI:        strings.TrimRight(get("TGWP_TELEMT_API", DefaultTelemtAPI), "/"),
		TelemtAPIToken:   get("TGWP_TELEMT_API_TOKEN", ""),
		TelemtConfigPath: get("TGWP_TELEMT_CONFIG", DefaultTelemtConfigPath),
		TelemtSiteDir:    get("TGWP_TELEMT_SITE_DIR", DefaultTelemtSiteDir),
		TelemtBin:        get("TGWP_TELEMT_BIN", DefaultTelemtBin),
		TelemtMetricsURL: get("TGWP_TELEMT_METRICS", telemt.DefaultMetricsURL),
	}
	if cfg.Engine != EngineTProxy && cfg.Engine != EngineTelemt {
		return cfg, fmt.Errorf("unknown TGWP_ENGINE %q (want %s or %s)", cfg.Engine, EngineTProxy, EngineTelemt)
	}
	if cfg.Engine == EngineTelemt && cfg.TelemtAPIToken == "" {
		f := get("TGWP_TELEMT_API_TOKEN_FILE", DefaultTelemtTokenPath)
		b, err := os.ReadFile(f)
		if err != nil {
			return cfg, fmt.Errorf("read telemt api token: %w", err)
		}
		cfg.TelemtAPIToken = strings.TrimSpace(string(b))
	}
	if cfg.Token == "" {
		if f := getenv("TGWP_TOKEN_FILE"); f != "" {
			b, err := os.ReadFile(f)
			if err != nil {
				return cfg, err
			}
			cfg.Token = strings.TrimSpace(string(b))
		}
	}
	if cfg.PanelURL == "" || cfg.Token == "" {
		return cfg, errors.New("TGWP_PANEL_URL and TGWP_TOKEN are required")
	}
	return cfg, nil
}
