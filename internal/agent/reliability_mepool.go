package agent

import (
	"sync"
	"time"

	"tgwebproxy/internal/reliability"
)

// ME exposes occupancy counters, not writer identity/phase ages. These timers
// describe continuously observed backlogs, never an individual stuck writer.
type mePoolSampler struct {
	mu                            sync.Mutex
	at                            time.Time
	active, pending               uint64
	orphanSince, replacementSince time.Time
}

func (s *mePoolSampler) sample(pool *reliability.MEPool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pool == nil || !pool.Enabled || pool.State == nil || pool.Reason != "" || pool.At.IsZero() || pool.At.After(now) || now.Sub(pool.At) > 2*time.Minute {
		s.at = time.Time{}
		s.orphanSince = time.Time{}
		s.replacementSince = time.Time{}
		return
	}
	state := pool.State
	if s.at.IsZero() || pool.At.Before(s.at) || pool.At.Sub(s.at) > 2*time.Minute || s.active != state.ActiveGeneration {
		s.orphanSince = time.Time{}
		s.replacementSince = time.Time{}
	}
	if s.pending != state.PendingGeneration {
		s.orphanSince = time.Time{}
	}
	duration := func(available, busy bool, since *time.Time) *uint64 {
		if !available || !busy {
			*since = time.Time{}
		}
		if !available {
			return nil
		}
		var seconds uint64
		if busy {
			if since.IsZero() {
				*since = pool.At
			}
			seconds = uint64(pool.At.Sub(*since) / time.Second)
		}
		return &seconds
	}
	state.OrphanObservedSeconds = duration(state.OrphanWarmWriters != nil, state.OrphanWarmWriters != nil && *state.OrphanWarmWriters > 0, &s.orphanSince)
	state.ReplacementObservedSeconds = duration(state.ReplacementPreparing != nil && state.ReplacementRetiring != nil, (state.ReplacementPreparing != nil && *state.ReplacementPreparing > 0) || (state.ReplacementRetiring != nil && *state.ReplacementRetiring > 0), &s.replacementSince)
	s.at, s.active, s.pending = pool.At, state.ActiveGeneration, state.PendingGeneration
}
