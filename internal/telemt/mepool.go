package telemt

import (
	"context"
	"net/http"
	"time"

	"tgwebproxy/internal/reliability"
)

// MEPoolState reads the authoritative control API snapshot, rather than the
// Prometheus gauges which may render zeros when ME telemetry is silent.
func (c *Client) MEPoolState(ctx context.Context) (*reliability.MEPool, error) {
	var raw struct {
		Enabled     bool   `json:"enabled"`
		Reason      string `json:"reason"`
		GeneratedAt uint64 `json:"generated_at_epoch_secs"`
		Data        *struct {
			Generations *struct {
				Active  *uint64 `json:"active_generation"`
				Pending *uint64 `json:"pending_hardswap_generation"`
				Age     *uint64 `json:"pending_hardswap_age_secs"`
			} `json:"generations"`
			Hardswap *struct {
				Enabled    *bool   `json:"enabled"`
				Pending    *bool   `json:"pending"`
				MapCurrent *bool   `json:"pending_map_current"`
				Writers    *uint64 `json:"pending_writers_current"`
				Deficit    *uint64 `json:"pending_writer_deficit"`
				Missing    *uint64 `json:"pending_missing_dc_groups"`
				Orphans    *uint64 `json:"orphan_warm_writers_current"`
				Preparing  *uint64 `json:"replacement_preparing_current"`
				Retiring   *uint64 `json:"replacement_retiring_current"`
			} `json:"hardswap"`
		} `json:"data"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/runtime/me-pool-state", nil, &raw); err != nil {
		return nil, err
	}
	out := &reliability.MEPool{Enabled: raw.Enabled, Reason: raw.Reason}
	// Ignore an absent/out-of-range timestamp instead of treating retrieval time as freshness.
	if raw.GeneratedAt > 0 && raw.GeneratedAt <= 253402300799 {
		out.At = time.Unix(int64(raw.GeneratedAt), 0).UTC()
	}
	if !raw.Enabled || raw.Reason != "" {
		return out, nil
	}
	if raw.Data == nil || raw.Data.Generations == nil || raw.Data.Hardswap == nil {
		out.Reason = "incomplete_snapshot"
		return out, nil
	}
	g, hs := raw.Data.Generations, raw.Data.Hardswap
	if g.Active == nil || g.Pending == nil || hs.Enabled == nil || hs.Pending == nil || (*hs.Pending != (*g.Pending != 0)) {
		out.Reason = "incomplete_snapshot"
		return out, nil
	}
	out.State = &reliability.MEPoolState{
		ActiveGeneration: *g.Active, PendingGeneration: *g.Pending, PendingAgeSeconds: g.Age,
		HardswapEnabled: *hs.Enabled, Pending: *hs.Pending, PendingMapCurrent: hs.MapCurrent,
		PendingWriters: hs.Writers, WriterDeficit: hs.Deficit, MissingDCGroups: hs.Missing,
		OrphanWarmWriters: hs.Orphans, ReplacementPreparing: hs.Preparing, ReplacementRetiring: hs.Retiring,
	}
	return out, nil
}
