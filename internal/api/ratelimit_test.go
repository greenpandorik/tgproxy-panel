package api

import (
	"strconv"
	"testing"
	"time"
)

func TestIPLimiter(t *testing.T) {
	l := newIPLimiter(3, time.Minute, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d blocked", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("4th attempt should be blocked")
	}
	if !l.Allow("5.6.7.8") {
		t.Fatal("other ip blocked")
	}
	l.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if !l.Allow("1.2.3.4") {
		t.Fatal("block should expire")
	}
}

func TestIPLimiterPrunesStaleEntries(t *testing.T) {
	l := newIPLimiter(3, time.Minute, time.Minute)
	base := time.Now()
	l.now = func() time.Time { return base }

	// One IP gets itself blocked; many others just make a single attempt.
	for i := 0; i < 4; i++ {
		l.Allow("9.9.9.9")
	}
	for i := 0; i < pruneEvery-5; i++ {
		l.Allow(strconv.Itoa(i) + ".0.0.1")
	}
	if len(l.attempts) < pruneEvery-5 {
		t.Fatalf("expected the map to have grown, got %d entries", len(l.attempts))
	}

	// Everything ages out, and the next call crosses the prune boundary.
	l.now = func() time.Time { return base.Add(2 * time.Hour) }
	l.Allow("fresh.ip")

	if len(l.blocked) != 0 {
		t.Fatalf("expired blocks not pruned: %d left", len(l.blocked))
	}
	// Only the IP from the pruning call itself should survive.
	if len(l.attempts) != 1 {
		t.Fatalf("stale attempts not pruned: %d left", len(l.attempts))
	}
	if _, ok := l.attempts["fresh.ip"]; !ok {
		t.Fatalf("the current caller's attempts were pruned: %v", l.attempts)
	}
}

// A still-blocked IP must survive a prune, or the block would be lifted early.
func TestIPLimiterPruneKeepsActiveBlocks(t *testing.T) {
	l := newIPLimiter(3, time.Minute, time.Hour)
	base := time.Now()
	l.now = func() time.Time { return base }
	for i := 0; i < 4; i++ {
		l.Allow("9.9.9.9")
	}
	l.prune(base.Add(2 * time.Minute)) // past the window, well inside the block
	if _, ok := l.blocked["9.9.9.9"]; !ok {
		t.Fatal("an active block was pruned")
	}
	if l.Allow("9.9.9.9") {
		t.Fatal("a blocked IP was allowed after a prune")
	}
}

func TestIPLimiterBlockedDoesNotRecord(t *testing.T) {
	l := newIPLimiter(2, time.Minute, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	for range 20 {
		if l.Blocked("1.2.3.4") {
			t.Fatal("peek reported a block before any attempt was recorded")
		}
	}
	for i := range 2 {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d must be allowed", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("the third attempt should be blocked")
	}
	if !l.Blocked("1.2.3.4") {
		t.Fatal("peek should report the block")
	}
	if l.Blocked("5.6.7.8") {
		t.Fatal("peek leaked the block to another ip")
	}

	now = now.Add(2 * time.Minute)
	if l.Blocked("1.2.3.4") {
		t.Fatal("peek should report the block as expired")
	}
}
