package worker

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
)

const maxBackgroundApplies = 4

type applyRetry struct {
	revision int64
	attempts int
	after    time.Time
}

// applySchedule reserves a worker before its goroutine starts. Deferred nodes
// stay dirty in the database and are reconsidered on the next sweep.
type applySchedule struct {
	mu      sync.Mutex
	running map[uuid.UUID]bool
	retries map[uuid.UUID]applyRetry
}

func (s *applySchedule) claim(id uuid.UUID, revision int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] || len(s.running) >= maxBackgroundApplies {
		return false
	}
	if retry, ok := s.retries[id]; ok {
		if retry.revision == revision && now.Before(retry.after) {
			return false
		}
		if retry.revision != revision {
			// Newly edited settings (especially revoked keys) must not inherit the
			// delay from an older, failing desired state.
			delete(s.retries, id)
		}
	}
	if s.running == nil {
		s.running = make(map[uuid.UUID]bool)
	}
	s.running[id] = true
	return true
}

func (s *applySchedule) finish(id uuid.UUID, revision int64, now time.Time, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, id)
	if !failed {
		delete(s.retries, id)
		return
	}
	if s.retries == nil {
		s.retries = make(map[uuid.UUID]applyRetry)
	}
	retry := s.retries[id]
	if retry.revision != revision {
		retry = applyRetry{revision: revision}
	}
	if retry.attempts < 10 {
		retry.attempts++
	}
	retry.after = now.Add(applyRetryDelay(retry.attempts))
	s.retries[id] = retry
}

func (s *applySchedule) forget(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.retries, id)
}

func applyRetryDelay(attempt int) time.Duration {
	ceiling := 45 * time.Second
	for i := 1; i < attempt && ceiling < 15*time.Minute; i++ {
		ceiling *= 2
	}
	ceiling = min(ceiling, 15*time.Minute)
	floor := max(45*time.Second, ceiling/2)
	if floor == ceiling {
		return floor
	}
	return floor + time.Duration(rand.Int64N(int64(ceiling-floor)))
}
