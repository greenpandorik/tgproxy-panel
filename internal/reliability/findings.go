package reliability

import (
	"strconv"
	"time"
)

type Finding struct {
	Kind    string
	Message string
	Failed  bool
	Known   bool
}

// Findings never resolves an incident from an absent or stale measurement.
func Findings(r Report, now time.Time) []Finding {
	if r.Version == 0 || now.Sub(r.At) > 2*time.Minute {
		return nil
	}
	out := []Finding{}
	if w := r.Connections; w != nil && now.Sub(w.At) <= 2*time.Minute && w.Success+w.Failed > 0 {
		out = append(out, Finding{"connection_failures", "Recent Telegram connection failures exceed 20%", float64(w.Failed)/float64(w.Success+w.Failed) > .2, true})
	}
	if len(r.Routes) > 0 {
		bad := false
		known := true
		for _, route := range r.Routes {
			bad = bad || !route.Healthy
			known = known && route.Age <= 120
		}
		out = append(out, Finding{"upstream_failure", "A Telegram egress route is unavailable", bad, known})
	}
	for _, dc := range r.DCs {
		if dc.Required > 0 {
			out = append(out, Finding{"dc_" + strconv.Itoa(dc.ID), "Telegram DC has no live Middle Proxy writers", dc.Alive == 0, true})
		}
	}
	res := r.Resources
	ratio := func(kind, msg string, used, limit *uint64) {
		if used != nil && limit != nil && *limit > 0 {
			out = append(out, Finding{kind, msg, float64(*used)/float64(*limit) >= .9, true})
		}
	}
	ratio("fd_pressure", "Telemt is using at least 90% of its file descriptors", res.FDUsed, res.FDLimit)
	ratio("conntrack_pressure", "Connection tracking is at least 90% full", res.ConntrackUsed, res.ConntrackLimit)
	if res.InodesUsedPercent != nil {
		out = append(out, Finding{"inode_pressure", "Filesystem inodes are at least 90% full", *res.InodesUsedPercent >= 90, true})
	}
	return out
}
