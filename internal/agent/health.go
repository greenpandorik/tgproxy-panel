package agent

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	agentv1 "tgwebproxy/proto/agent/v1"
)

func (h *Handler) unitActive(ctx context.Context, unit string) bool {
	out, err := h.exec.Run(ctx, "systemctl", "is-active", unit)
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

func (h *Handler) profileCount() int {
	raw, err := os.ReadFile(h.cfg.ProfilesPath)
	if err != nil {
		return 0
	}
	var f struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	_ = json.Unmarshal(raw, &f)
	return len(f.Profiles)
}

func readUptime() int64 {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	return int64(f)
}

func readLoadPercent() float64 {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	load, _ := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	p := load / float64(runtime.NumCPU()) * 100
	if p > 100 {
		p = 100
	}
	return p
}

func readMemPercent() float64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var total, avail float64
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total == 0 {
		return 0
	}
	return (total - avail) / total * 100
}

func diskPercent(path string) float64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0
	}
	used := float64(st.Blocks-st.Bfree) / float64(st.Blocks) * 100
	return used
}

func (h *Handler) Health(ctx context.Context) *agentv1.HealthReport {
	if h.cfg.Engine == EngineTelemt {
		return h.healthTelemt(ctx)
	}
	return &agentv1.HealthReport{
		RelayActive: h.unitActive(ctx, "tproxy-server"), MtproxyActive: h.unitActive(ctx, "mtproxy"), CaddyActive: h.unitActive(ctx, "caddy"),
		Healthz: h.probe(ctx, "/healthz"), Readyz: h.probe(ctx, "/readyz"),
		TproxyVersion: h.cfg.TProxyVersion, AgentVersion: Version,
		UptimeSeconds: readUptime(), CpuPercent: readLoadPercent(), MemUsedPercent: readMemPercent(), DiskUsedPercent: diskPercent(h.siteDir()),
		ProfileCount: int32(h.profileCount()),
	}
}
