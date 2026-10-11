package agent

import (
	"testing"
	"time"

	"tgwebproxy/internal/reliability"
)

func samplerPool(at time.Time, active, pending uint64, orphans, preparing, retiring *uint64) *reliability.MEPool {
	return &reliability.MEPool{At: at, Enabled: true, State: &reliability.MEPoolState{ActiveGeneration: active, PendingGeneration: pending, OrphanWarmWriters: orphans, ReplacementPreparing: preparing, ReplacementRetiring: retiring}}
}

func TestMEPoolSamplerMeasuresContinuousBacklogFromFreshSource(t *testing.T) {
	var sampler mePoolSampler
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	one, zero := uint64(1), uint64(0)
	for i := 0; i <= 5; i++ {
		at := now.Add(time.Duration(i) * time.Minute)
		pool := samplerPool(at, 7, 8, &one, &one, &zero)
		sampler.sample(pool, at)
		if pool.State.OrphanObservedSeconds == nil || *pool.State.OrphanObservedSeconds != uint64(i*60) || pool.State.ReplacementObservedSeconds == nil || *pool.State.ReplacementObservedSeconds != uint64(i*60) {
			t.Fatalf("sample%d: %+v", i, pool.State)
		}
	}
	// A duplicate source cannot age the backlog using the heartbeat clock.
	pool := samplerPool(now.Add(5*time.Minute), 7, 8, &one, &zero, &one)
	sampler.sample(pool, now.Add(6*time.Minute))
	if pool.State.ReplacementObservedSeconds == nil || *pool.State.ReplacementObservedSeconds != 300 {
		t.Fatalf("duplicate source aged: %+v", pool.State)
	}
}

func TestMEPoolSamplerResetsOnUnknownGapsAndNewGenerations(t *testing.T) {
	one, zero := uint64(1), uint64(0)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                        string
		interrupt                   func(time.Time) *reliability.MEPool
		after                       time.Duration
		active, pending             uint64
		wantOrphan, wantReplacement uint64
	}{
		{"unavailable", func(at time.Time) *reliability.MEPool { return nil }, time.Minute, 7, 8, 0, 0},
		{"disabled", func(at time.Time) *reliability.MEPool { return &reliability.MEPool{At: at} }, time.Minute, 7, 8, 0, 0},
		{"stale", func(at time.Time) *reliability.MEPool {
			return samplerPool(at.Add(-121*time.Second), 7, 8, &one, &one, &zero)
		}, time.Minute, 7, 8, 0, 0},
		{"future", func(at time.Time) *reliability.MEPool {
			return samplerPool(at.Add(time.Second), 7, 8, &one, &one, &zero)
		}, time.Minute, 7, 8, 0, 0},
		{"missing counters", func(at time.Time) *reliability.MEPool { return samplerPool(at, 7, 8, nil, nil, nil) }, time.Minute, 7, 8, 0, 0},
		{"long sampling gap", nil, 3 * time.Minute, 7, 8, 0, 0},
		{"new active generation", nil, time.Minute, 9, 8, 0, 0},
		{"new pending generation", nil, time.Minute, 7, 9, 0, 120},
		{"clear", func(at time.Time) *reliability.MEPool { return samplerPool(at, 7, 8, &zero, &zero, &zero) }, time.Minute, 7, 8, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sampler mePoolSampler
			sampler.sample(samplerPool(now, 7, 8, &one, &one, &zero), now)
			sampler.sample(samplerPool(now.Add(time.Minute), 7, 8, &one, &one, &zero), now.Add(time.Minute))
			if tc.interrupt != nil {
				sampler.sample(tc.interrupt(now.Add(90*time.Second)), now.Add(90*time.Second))
			}
			at := now.Add(time.Minute + tc.after)
			pool := samplerPool(at, tc.active, tc.pending, &one, &one, &zero)
			sampler.sample(pool, at)
			if pool.State.OrphanObservedSeconds == nil || *pool.State.OrphanObservedSeconds != tc.wantOrphan || pool.State.ReplacementObservedSeconds == nil || *pool.State.ReplacementObservedSeconds != tc.wantReplacement {
				t.Fatalf("backlog failed reset: %+v", pool.State)
			}
		})
	}
}
