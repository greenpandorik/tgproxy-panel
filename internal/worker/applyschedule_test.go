package worker

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestApplyScheduleBoundsAndDeduplicates(t *testing.T) {
	var s applySchedule
	now := time.Now()
	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.New()
	}
	for _, id := range ids[:4] {
		if !s.claim(id, 1, now) {
			t.Fatal("free worker slot refused")
		}
		if s.claim(id, 1, now) {
			t.Fatal("same node scheduled twice")
		}
	}
	if s.claim(ids[4], 1, now) {
		t.Fatal("more than four background applies admitted")
	}
	s.finish(ids[0], 1, now, false)
	if !s.claim(ids[4], 1, now) {
		t.Fatal("completed apply did not free its worker slot")
	}
}

func TestApplyScheduleBackoffAndChangedRevision(t *testing.T) {
	var s applySchedule
	id := uuid.New()
	now := time.Now()
	for attempt := 0; attempt < 10; attempt++ {
		if !s.claim(id, 1, now) {
			t.Fatal("elapsed retry not scheduled")
		}
		s.finish(id, 1, now, true)
		if s.claim(id, 1, now.Add(44*time.Second)) {
			t.Fatal("failed apply retried too soon")
		}
		if !s.claim(id, 1, now.Add(15*time.Minute)) {
			t.Fatal("retry delayed beyond fifteen minutes")
		}
		s.finish(id, 1, now.Add(15*time.Minute), true)
		now = now.Add(30 * time.Minute)
	}
	if !s.claim(id, 1, now) {
		t.Fatal("elapsed retry not scheduled")
	}
	s.finish(id, 1, now, true)
	if !s.claim(id, 2, now.Add(time.Second)) {
		t.Fatal("new desired revision must not wait for old revision's backoff")
	}
	s.finish(id, 2, now, false)
	if !s.claim(id, 2, now) {
		t.Fatal("success must clear the previous failure backoff")
	}
}

func TestApplyScheduleDelayGrowsAndStaysBounded(t *testing.T) {
	if got := applyRetryDelay(1); got != 45*time.Second {
		t.Fatalf("first retry = %s, want 45s", got)
	}
	for range 100 {
		if d := applyRetryDelay(3); d < 90*time.Second || d > 3*time.Minute {
			t.Fatalf("third retry out of range: %s", d)
		}
		if d := applyRetryDelay(1000); d < 450*time.Second || d > 15*time.Minute {
			t.Fatalf("capped retry out of range: %s", d)
		}
	}
}

func TestApplyScheduleForgetAllowsImmediateRetry(t *testing.T) {
	var s applySchedule
	id := uuid.New()
	now := time.Now()
	if !s.claim(id, 1, now) {
		t.Fatal("claim")
	}
	s.finish(id, 1, now, true)
	s.forget(id)
	if !s.claim(id, 1, now) {
		t.Fatal("successful manual apply must clear background backoff")
	}
}
