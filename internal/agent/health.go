package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

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

// cpuSampler turns /proc/stat's monotonic jiffy counters into a utilisation percentage. A single
// reading says nothing: the counters are totals since boot, so utilisation only exists between two
// of them. The first call therefore reports nothing rather than a number derived from uptime, and
// the panel shows "not available" until the second heartbeat.
type cpuSampler struct {
	mu    sync.Mutex
	prev  cpuTimes
	valid bool
}

type cpuTimes struct{ total, idle uint64 }

// sample returns utilisation since the previous call, and whether there was one to measure against.
func (c *cpuSampler) sample() (float64, bool) {
	now, err := readCPUTimes()
	if err != nil {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, had := c.prev, c.valid
	c.prev, c.valid = now, true
	if !had {
		return 0, false
	}
	return busyBetween(prev, now)
}

// busyBetween is the share of the interval the processor spent working, or false when the two
// readings cannot be subtracted: the counters restart at zero when the node reboots.
func busyBetween(prev, now cpuTimes) (float64, bool) {
	if now.total <= prev.total || now.idle < prev.idle {
		return 0, false
	}
	totalDelta := now.total - prev.total
	idleDelta := now.idle - prev.idle
	if idleDelta > totalDelta {
		return 0, false
	}
	busy := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	return min(max(busy, 0), 100), true
}

func readCPUTimes() (cpuTimes, error) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, errors.New("/proc/stat: no aggregate cpu line")
	}
	var out cpuTimes
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return cpuTimes{}, err
		}
		out.total += v
		// user nice system idle iowait ...: idle and iowait are both time not spent working.
		if i == 3 || i == 4 {
			out.idle += v
		}
	}
	return out, nil
}

// readLoadAverages returns the three load averages, which are a different measurement from
// utilisation and are reported alongside it rather than in place of it.
func readLoadAverages() (one, five, fifteen float64, ok bool) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, false
	}
	f := strings.Fields(string(raw))
	if len(f) < 3 {
		return 0, 0, 0, false
	}
	one, _ = strconv.ParseFloat(f[0], 64)
	five, _ = strconv.ParseFloat(f[1], 64)
	fifteen, _ = strconv.ParseFloat(f[2], 64)
	return one, five, fifteen, true
}

// withLoad fills in the load figures every engine reports the same way.
func (h *Handler) withLoad(rep *agentv1.HealthReport) *agentv1.HealthReport {
	if v, ok := h.cpu.sample(); ok {
		rep.CpuUtilisationPercent = &v
	}
	if one, five, fifteen, ok := readLoadAverages(); ok {
		rep.LoadAverage_1, rep.LoadAverage_5, rep.LoadAverage_15 = &one, &five, &fifteen
	}
	return rep
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
	return h.withLoad(&agentv1.HealthReport{
		RelayActive: h.unitActive(ctx, "tproxy-server"), MtproxyActive: h.unitActive(ctx, "mtproxy"), CaddyActive: h.unitActive(ctx, "caddy"),
		Healthz: h.probe(ctx, "/healthz"), Readyz: h.probe(ctx, "/readyz"),
		TproxyVersion: h.cfg.TProxyVersion, AgentVersion: Version,
		UptimeSeconds: readUptime(), CpuPercent: readLoadPercent(), MemUsedPercent: readMemPercent(), DiskUsedPercent: diskPercent(h.siteDir()),
		ProfileCount: int32(h.profileCount()),
	})
}
