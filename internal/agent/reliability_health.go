package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"tgwebproxy/internal/reliability"
	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

type connectionSampler struct {
	mu              sync.Mutex
	at              time.Time
	success, failed int64
	window          *reliability.Window
}

func (s *connectionSampler) sample(now time.Time, success, failed int64) *reliability.Window {
	s.mu.Lock()
	defer s.mu.Unlock()
	elapsed := now.Sub(s.at)
	if s.at.IsZero() || success < s.success || failed < s.failed || elapsed > 2*time.Minute {
		s.at = now
		s.success = success
		s.failed = failed
		s.window = nil
		return nil
	}
	if elapsed >= 15*time.Second {
		s.window = &reliability.Window{At: now, Seconds: elapsed.Seconds(), Success: success - s.success, Failed: failed - s.failed}
		s.at = now
		s.success = success
		s.failed = failed
	}
	if s.window == nil {
		return nil
	}
	w := *s.window
	return &w
}

func readUint(path string) *uint64 {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil
	}
	n, e := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if e != nil {
		return nil
	}
	return &n
}

func procCounter(path, group, key string) *uint64 {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	for i := 0; i+1 < len(lines); i++ {
		names := strings.Fields(lines[i])
		values := strings.Fields(lines[i+1])
		if len(names) == 0 || names[0] != group+":" || len(names) != len(values) {
			continue
		}
		for j, n := range names {
			if n == key {
				v, e := strconv.ParseUint(values[j], 10, 64)
				if e == nil {
					return &v
				}
			}
		}
	}
	return nil
}

func (h *Handler) readResources(ctx context.Context, site string) reliability.Resources {
	r := reliability.Resources{ConntrackUsed: readUint("/proc/sys/net/netfilter/nf_conntrack_count"), ConntrackLimit: readUint("/proc/sys/net/netfilter/nf_conntrack_max"), TCPRetransmits: procCounter("/proc/net/snmp", "Tcp", "RetransSegs"), ListenDrops: procCounter("/proc/net/netstat", "TcpExt", "ListenDrops")}
	// Read telemt's descriptors, not the agent's. Missing pid/proc data stays unavailable.
	if b, e := h.exec.Run(ctx, "systemctl", "show", "telemt", "--property=MainPID", "--value"); e == nil {
		if pid, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && pid > 0 {
			base := filepath.Join("/proc", strconv.Itoa(pid))
			if entries, e := os.ReadDir(filepath.Join(base, "fd")); e == nil {
				v := uint64(len(entries))
				r.FDUsed = &v
			}
			if limits, e := os.ReadFile(filepath.Join(base, "limits")); e == nil {
				for _, line := range strings.Split(string(limits), "\n") {
					if strings.HasPrefix(line, "Max open files") {
						f := strings.Fields(line)
						if len(f) > 3 {
							if n, e := strconv.ParseUint(f[3], 10, 64); e == nil {
								r.FDLimit = &n
							}
						}
					}
				}
			}
		}
	}
	var fs unix.Statfs_t
	if unix.Statfs(site, &fs) == nil && fs.Files > 0 {
		v := float64(fs.Files-fs.Ffree) / float64(fs.Files) * 100
		r.InodesUsedPercent = &v
	}
	if b, e := os.ReadFile("/proc/vmstat"); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "oom_kill" {
				if n, e := strconv.ParseUint(f[1], 10, 64); e == nil {
					r.OOMKills = &n
				}
			}
		}
	}
	return r
}

func (h *Handler) reliabilityHealth(ctx context.Context, rep *agentv1.HealthReport, upstreams *telemt.UpstreamsStats) {
	now := time.Now().UTC()
	out := reliability.Report{Version: 1, At: now, Resources: h.readResources(ctx, h.siteDir()), Routes: []reliability.Route{}, Events: []reliability.Event{}}
	if upstreams != nil {
		out.Connections = h.connections.sample(now, upstreams.Zero.ConnectSuccessTotal, upstreams.Zero.ConnectFailTotal)
		for _, u := range upstreams.Upstreams {
			out.Routes = append(out.Routes, reliability.Route{Kind: u.RouteKind, Healthy: u.Healthy, Age: u.LastCheckAgeSecs, DCs: len(u.DC)})
		}
	}
	if h.tm != nil {
		if d, e := h.tm.DCStats(ctx); e == nil && d.MiddleProxyEnabled {
			out.DCs = d.DCs
		}
	}
	h.recoveryMu.Lock()
	out.Policy = h.recoveryPolicy
	out.ActiveEgress = h.activeEgress
	out.ConsecutiveFailures = h.recoveryFailures
	out.Events = append(out.Events, h.recoveryEvents...)
	h.recoveryMu.Unlock()
	rep.ReliabilityJson, _ = json.Marshal(out)
}
