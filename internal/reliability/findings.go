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
	out = append(out, mePoolFindings(r.MEPool, now)...)
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

// API coverage zeros on a stale map are deliberately not recovery evidence.
func mePoolFindings(pool *MEPool, now time.Time) []Finding {
	// Keep an explicit unknown for each ME check, so the worker breaks a prior
	// incident streak while leaving the incident open. Never-observed unknown
	// checks are not persisted as notification findings.
	out := []Finding{
		{Kind: "me_hardswap_pending", Message: "Middle Proxy hardswap has been pending for at least 25 minutes"},
		{Kind: "me_hardswap_deficit", Message: "Middle Proxy pending generation has a persistent writer or DC-family deficit"},
		{Kind: "me_hardswap_stale_map", Message: "Middle Proxy pending generation targets a stale endpoint map"},
		{Kind: "me_orphan_warm_writers", Message: "Middle Proxy orphan warm writers persist for at least 5 minutes"},
		{Kind: "me_replacement_busy", Message: "Middle Proxy writer replacements remain busy for at least 5 minutes"},
	}
	if pool == nil || !pool.Enabled || pool.State == nil || pool.Reason != "" || !freshMEPool(pool.At, now) {
		return out
	}
	s := pool.State
	pending := s.Pending && s.PendingGeneration != 0
	noPending := !s.Pending && s.PendingGeneration == 0
	ageKnown := pending && s.PendingAgeSeconds != nil
	mature := ageKnown && *s.PendingAgeSeconds >= 300
	coverageKnown := mature && s.PendingMapCurrent != nil && *s.PendingMapCurrent && s.WriterDeficit != nil && s.MissingDCGroups != nil
	if s.HardswapEnabled {
		out[0].Failed = ageKnown && *s.PendingAgeSeconds >= 1500
		out[0].Known = noPending || (ageKnown && *s.PendingAgeSeconds >= 1500)
		out[1].Failed = coverageKnown && (*s.WriterDeficit > 0 || *s.MissingDCGroups > 0)
		out[1].Known = noPending || coverageKnown
		out[2].Failed = mature && s.PendingMapCurrent != nil && !*s.PendingMapCurrent
		out[2].Known = noPending || (mature && s.PendingMapCurrent != nil)
	}
	backlog := func(index int, available, busy bool, observed *uint64) {
		sustained := busy && observed != nil && *observed >= 300
		out[index].Failed = available && sustained
		out[index].Known = available && (!busy || sustained)
	}
	backlog(3, s.OrphanWarmWriters != nil, s.OrphanWarmWriters != nil && *s.OrphanWarmWriters > 0, s.OrphanObservedSeconds)
	backlog(4, s.ReplacementPreparing != nil && s.ReplacementRetiring != nil, (s.ReplacementPreparing != nil && *s.ReplacementPreparing > 0) || (s.ReplacementRetiring != nil && *s.ReplacementRetiring > 0), s.ReplacementObservedSeconds)
	return out
}

func freshMEPool(at, now time.Time) bool {
	return !at.IsZero() && !at.After(now) && now.Sub(at) <= 2*time.Minute
}
