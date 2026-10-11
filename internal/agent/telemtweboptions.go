package agent

import (
	"context"
	"errors"
	"fmt"

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

// Only an agent that completed the reload and read the effective config may acknowledge a prefix.
func (h *Handler) verifiedWebBasePath(ctx context.Context, req *agentv1.ApplyRequest, runtimeRevision string) (*string, error) {
	if req.ManagedWebHost == "" && (req.WebPolicy == nil || req.WebPolicy.BasePath == nil) {
		return nil, nil
	}
	cfg, revision, err := h.tm.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if runtimeRevision == "" || revision != runtimeRevision {
		return nil, errors.New("WEB config revision differs from the loaded runtime; retry apply")
	}
	web, _ := cfg["web"].(map[string]any)
	i, err := webVhostIndex(web, req.ManagedWebHost)
	if err != nil {
		return nil, err
	}
	v := web["vhosts"].([]any)[i].(map[string]any)
	path, _ := v["base_path"].(string)
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
	return &path, nil
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
