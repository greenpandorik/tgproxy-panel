package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

// telemtDefaultMaxConnections is telemt's own server.max_connections.
const telemtDefaultMaxConnections = 10000

// telemtConfigBackup is telemt.toml as it was before an apply rewrote [web.limits] or [server].
type telemtConfigBackup struct {
	data []byte
	mode os.FileMode
}

var (
	tomlServerHeaderRe = regexp.MustCompile(`^\s*\[\s*server\s*\]\s*(#.*)?$`)
	tomlTableHeaderRe  = regexp.MustCompile(`^\s*\[`)
	tomlMaxConnRe      = regexp.MustCompile(`^(\s*)max_connections\s*=\s*([^#]*?)\s*(#.*)?$`)
)

// tomlServerSection returns the line index of the [server] header and the end of its body.
func tomlServerSection(lines []string) (header, end int, err error) {
	header = -1
	for i, line := range lines {
		if header < 0 {
			if tomlServerHeaderRe.MatchString(line) {
				header = i
			}
			continue
		}
		if tomlTableHeaderRe.MatchString(line) {
			return header, i, nil
		}
	}
	if header < 0 {
		return -1, -1, errors.New("telemt config has no [server] table")
	}
	return header, len(lines), nil
}

// tomlServerMaxConnections reads server.max_connections, telemt's default when the key is absent.
func tomlServerMaxConnections(raw []byte) (int64, error) {
	lines := strings.Split(string(raw), "\n")
	header, end, err := tomlServerSection(lines)
	if err != nil {
		return 0, err
	}
	for _, line := range lines[header+1 : end] {
		if m := tomlMaxConnRe.FindStringSubmatch(line); m != nil {
			v, err := strconv.ParseInt(strings.ReplaceAll(m[2], "_", ""), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("server.max_connections %q is not an integer", m[2])
			}
			return v, nil
		}
	}
	return telemtDefaultMaxConnections, nil
}

// setTomlServerMaxConnections rewrites server.max_connections and leaves every other line as it was.
func setTomlServerMaxConnections(raw []byte, v int64) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	header, end, err := tomlServerSection(lines)
	if err != nil {
		return nil, err
	}
	value := strconv.FormatInt(v, 10)
	for i := header + 1; i < end; i++ {
		if m := tomlMaxConnRe.FindStringSubmatch(lines[i]); m != nil {
			line := m[1] + "max_connections = " + value
			if m[3] != "" {
				line += " " + m[3]
			}
			lines[i] = line
			return []byte(strings.Join(lines, "\n")), nil
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:header+1]...)
	out = append(out, "max_connections = "+value)
	out = append(out, lines[header+1:]...)
	return []byte(strings.Join(out, "\n")), nil
}

func telemtLimitInt(limits map[string]any, key string, fallback int64) int64 {
	switch v := limits[key].(type) {
	case float64:
		return int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	}
	return fallback
}

// telemtWebLimits returns [web.limits] from a config the control API returned.
func telemtWebLimits(cfg map[string]any) map[string]any {
	web, _ := cfg["web"].(map[string]any)
	limits, _ := web["limits"].(map[string]any)
	return limits
}

// readTelemtConfigFile returns telemt.toml and its permission bits.
func (h *Handler) readTelemtConfigFile() ([]byte, os.FileMode, error) {
	info, err := os.Stat(h.cfg.TelemtConfigPath)
	if err != nil {
		return nil, 0, err
	}
	raw, err := os.ReadFile(h.cfg.TelemtConfigPath)
	if err != nil {
		return nil, 0, err
	}
	return raw, info.Mode().Perm(), nil
}

// reconcileTelemtCapacity brings [web.limits] and server.max_connections to the capacity preset
// and keeps web.limits.max_profiles above the node's key cap. Every change here is process-owned,
// so a true result means telemt must restart.
func (h *Handler) reconcileTelemtCapacity(ctx context.Context, lg *applyLog, req *agentv1.ApplyRequest, rb *telemtRollback) (bool, error) {
	capacity := int(req.GetWebCapacity())
	var preset domain.WebCapacity
	if capacity != 0 {
		p, ok := domain.WebCapacityPreset(capacity)
		if !ok {
			return false, fmt.Errorf("WEB capacity %d is not one of the presets", capacity)
		}
		preset = p
	}
	wantProfiles := 0
	if n := int(req.GetMaxProfiles()); n > 0 {
		wantProfiles = max(domain.WebMaxProfiles(n), len(req.GetProfiles()))
	}
	if capacity == 0 && wantProfiles == 0 {
		return false, nil
	}

	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	current := telemtWebLimits(cfg)
	patch := map[string]any{}
	if capacity != 0 {
		standard, _ := domain.WebCapacityPreset(domain.WebCapacityStandard)
		defaults := map[string]int64{}
		for _, f := range standard.Limits.Fields() {
			defaults[f.Key] = f.Value
		}
		for _, f := range preset.Limits.Fields() {
			if telemtLimitInt(current, f.Key, defaults[f.Key]) != f.Value {
				patch[f.Key] = f.Value
			}
		}
	}
	presetChanged := len(patch) > 0
	if wantProfiles > 0 {
		if have := telemtLimitInt(current, "max_profiles", domain.TelemtDefaultMaxProfiles); have < int64(wantProfiles) {
			patch["max_profiles"] = wantProfiles
			lg.f("web.limits.max_profiles %d -> %d", have, wantProfiles)
		}
	}

	raw, mode, err := h.readTelemtConfigFile()
	if err != nil {
		return false, fmt.Errorf("read telemt config: %w", err)
	}
	connChanged := false
	if capacity != 0 {
		have, err := tomlServerMaxConnections(raw)
		if err != nil {
			return false, err
		}
		if have != preset.MaxConnections {
			connChanged = true
			lg.f("server.max_connections %d -> %d", have, preset.MaxConnections)
		}
	}
	if len(patch) == 0 && !connChanged {
		return false, nil
	}
	if presetChanged || connChanged {
		info, err := h.tm.SystemInfo(ctx)
		if err != nil {
			return false, fmt.Errorf("verify WEB capacity support: %w", err)
		}
		if !telemt.VersionAtLeast(info.Version, [3]int{3, 5, 14}) {
			return false, errors.New("WEB capacity requires Telemt 3.5.14 or newer")
		}
	}

	if len(patch) > 0 {
		if _, err := h.tm.PatchConfig(ctx, map[string]any{"web": map[string]any{"limits": patch}}, false); err != nil {
			return false, fmt.Errorf("web.limits: %w", err)
		}
		rb.config = &telemtConfigBackup{data: raw, mode: mode}
		if presetChanged {
			lg.f("web.limits set to the %d-session preset", capacity)
		}
	}
	if connChanged {
		cur, curMode, err := h.readTelemtConfigFile()
		if err != nil {
			return false, fmt.Errorf("read telemt config: %w", err)
		}
		next, err := setTomlServerMaxConnections(cur, preset.MaxConnections)
		if err != nil {
			return false, err
		}
		if err := writeAtomicKeepOwner(h.cfg.TelemtConfigPath, next, curMode); err != nil {
			return false, fmt.Errorf("write telemt config: %w", err)
		}
		rb.config = &telemtConfigBackup{data: raw, mode: mode}
	}
	return true, nil
}

// telemtCapacityReport fills the WEB session and TCP connection ceilings the node is configured with.
func (h *Handler) telemtCapacityReport(ctx context.Context, rep *agentv1.HealthReport) {
	if cfg, _, err := h.tm.GetConfig(ctx); err == nil {
		rep.WebCapacity = uint32(telemtLimitInt(telemtWebLimits(cfg), "max_sessions_global", domain.WebCapacityStandard))
	}
	if raw, _, err := h.readTelemtConfigFile(); err == nil {
		if n, err := tomlServerMaxConnections(raw); err == nil && n > 0 {
			rep.MaxConnections = uint32(n)
		}
	}
}
