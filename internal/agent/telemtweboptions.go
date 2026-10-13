package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func webVhostIndex(web map[string]any, host string) (int, error) {
	vs, _ := web["vhosts"].([]any)
	index := -1
	for i, raw := range vs {
		v, _ := raw.(map[string]any)
		if v == nil {
			continue
		}
		if host == "" {
			return i, nil
		}
		if v["host"] == host {
			if index >= 0 {
				return 0, fmt.Errorf("managed WEB vhost %q is ambiguous", host)
			}
			index = i
		}
	}
	if index >= 0 {
		return index, nil
	}
	return 0, fmt.Errorf("managed WEB vhost %q is missing", host)
}

func (h *Handler) checkWebAdvanced(ctx context.Context, p domain.WebPolicy) error {
	if !p.HasAdvancedOptions() {
		return nil
	}
	info, err := h.tm.SystemInfo(ctx)
	if err != nil {
		return fmt.Errorf("verify advanced WEB support: %w", err)
	}
	if !telemt.VersionAtLeast(info.Version, [3]int{3, 5, 14}) {
		return errors.New("advanced WEB options require Telemt 3.5.14 or newer")
	}
	return nil
}

func webVhostPolicyPatch(web map[string]any, host string, p domain.WebPolicy) ([]any, error) {
	if p.BasePath == nil && p.DecoyResolve == nil {
		return nil, nil
	}
	i, err := webVhostIndex(web, host)
	if err != nil {
		return nil, err
	}
	vs := web["vhosts"].([]any)
	next, err := copyVhosts(vs)
	if err != nil {
		return nil, err
	}
	v := next[i].(map[string]any)
	changed := false
	if p.BasePath != nil {
		cur, _ := v["base_path"].(string)
		if cur != *p.BasePath {
			v["base_path"] = *p.BasePath
			changed = true
		}
	}
	if p.DecoyResolve != nil {
		d, _ := v["decoy"].(map[string]any)
		if d == nil || d["mode"] != "http_upstream" {
			if changed {
				return next, nil
			}
			return nil, nil
		}
		cur, _ := d["resolve"].(string)
		if cur == "" {
			cur = "never"
		}
		if cur != string(*p.DecoyResolve) {
			d["resolve"] = string(*p.DecoyResolve)
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return next, nil
}

// Only an agent that completed a reload, or whose runtime was already verified to serve the
// configured prefix, may acknowledge it.
func (h *Handler) verifiedWebBasePath(ctx context.Context, req *agentv1.ApplyRequest, runtimeRevision string) (*string, error) {
	if req.ManagedWebHost == "" && (req.WebPolicy == nil || req.WebPolicy.BasePath == nil) {
		return nil, nil
	}
	cfg, revision, err := h.tm.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if runtimeRevision != "" && revision != runtimeRevision {
		return nil, errors.New("WEB config revision differs from the loaded runtime; retry apply")
	}
	web, _ := cfg["web"].(map[string]any)
	i, err := webVhostIndex(web, req.ManagedWebHost)
	if err != nil {
		return nil, err
	}
	v := web["vhosts"].([]any)[i].(map[string]any)
	path, _ := v["base_path"].(string)
	if runtimeRevision == "" {
		loaded, ok := readWebPrefixes(h.cfg.StateDir)[req.ManagedWebHost]
		if !ok || loaded != path {
			return nil, errors.New("WEB config revision differs from the loaded runtime; retry apply")
		}
	}
	if path != "" {
		info, e := h.tm.SystemInfo(ctx)
		if e != nil {
			return nil, e
		}
		if !telemt.VersionAtLeast(info.Version, [3]int{3, 5, 14}) {
			return nil, errors.New("path WEB links require Telemt 3.5.14 or newer")
		}
	}
	if err := domain.ValidateWebBasePath(path); err != nil {
		return nil, err
	}
	if p := req.GetWebPolicy(); p != nil && p.BasePath != nil && path != *p.BasePath {
		return nil, errors.New("WEB base_path verification failed")
	}
	if err := h.rememberWebPrefix(req.ManagedWebHost, path); err != nil {
		return nil, err
	}
	return &path, nil
}

func webPrefixesPath(stateDir string) string {
	return filepath.Join(stateDir, "telemt-web.json")
}

// readWebPrefixes returns the base_path the running telemt was last verified to serve, per vhost.
func readWebPrefixes(stateDir string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(webPrefixesPath(stateDir))
	if err != nil {
		return out
	}
	var st struct {
		BasePaths map[string]string `json:"base_paths"`
	}
	if json.Unmarshal(raw, &st) != nil || st.BasePaths == nil {
		return out
	}
	return st.BasePaths
}

func writeWebPrefixes(stateDir string, paths map[string]string) error {
	raw, err := json.Marshal(map[string]any{"base_paths": paths})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return writeAtomic(webPrefixesPath(stateDir), append(raw, '\n'), 0o600)
}

func (h *Handler) rememberWebPrefix(host, path string) error {
	paths := readWebPrefixes(h.cfg.StateDir)
	if cur, ok := paths[host]; ok && cur == path {
		return nil
	}
	paths[host] = path
	return writeWebPrefixes(h.cfg.StateDir, paths)
}

func (h *Handler) forgetWebPrefix(host string) error {
	paths := readWebPrefixes(h.cfg.StateDir)
	if _, ok := paths[host]; !ok {
		return nil
	}
	delete(paths, host)
	return writeWebPrefixes(h.cfg.StateDir, paths)
}

// webPrefixLoaded reports whether the vhost's configured base_path is the one the runtime was
// last verified to serve, so acknowledging it needs no reload.
func (h *Handler) webPrefixLoaded(ctx context.Context, host string) (bool, error) {
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	v, err := managedVhost(cfg, host)
	if err != nil {
		return false, err
	}
	path, _ := v["base_path"].(string)
	loaded, ok := readWebPrefixes(h.cfg.StateDir)[host]
	return ok && loaded == path, nil
}

func managedVhost(cfg map[string]any, host string) (map[string]any, error) {
	web, _ := cfg["web"].(map[string]any)
	i, err := webVhostIndex(web, host)
	if err != nil {
		return nil, err
	}
	return web["vhosts"].([]any)[i].(map[string]any), nil
}

func optionalHost(hosts []string) string {
	if len(hosts) > 0 {
		return hosts[0]
	}
	return ""
}
