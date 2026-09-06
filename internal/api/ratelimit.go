package api

import (
	"sync"
	"time"
)

type ipLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	block    time.Duration
	attempts map[string][]time.Time
	blocked  map[string]time.Time
	now      func() time.Time
	calls    int
}

// pruneEvery is how many Allow calls pass between sweeps of the two maps. Without
// them a distributed login spray grows both without bound for the process
// lifetime, since entries were only ever removed for the IP being checked.
const pruneEvery = 1000

func newIPLimiter(max int, window, block time.Duration) *ipLimiter {
	return &ipLimiter{max: max, window: window, block: block, attempts: map[string][]time.Time{}, blocked: map[string]time.Time{}, now: time.Now}
}

// prune drops every IP whose block has expired and whose attempts have all aged
// out of the window. Caller holds l.mu.
func (l *ipLimiter) prune(now time.Time) {
	for ip, until := range l.blocked {
		if !now.Before(until) {
			delete(l.blocked, ip)
		}
	}
	for ip, ts := range l.attempts {
		if _, stillBlocked := l.blocked[ip]; stillBlocked {
			continue
		}
		fresh := false
		for _, t := range ts {
			if now.Sub(t) < l.window {
				fresh = true
				break
			}
		}
		if !fresh {
			delete(l.attempts, ip)
		}
	}
}

// Blocked reports whether ip is currently shut out, without recording an attempt.
// It exists for the two-step login: the TOTP verify endpoint must refuse a blocked
// IP, but charging every call would make a normal two-step login cost two slots out
// of the same window the password step already spends one on. Verify therefore peeks
// here and only calls Allow when the code turns out to be wrong - which is exactly
// the traffic the limiter is there to stop.
func (l *ipLimiter) Blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.blocked[ip]
	return ok && l.now().Before(until)
}

// Allow records an attempt and reports whether it is permitted.
func (l *ipLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.calls++
	if l.calls%pruneEvery == 0 {
		l.prune(now)
	}
	if until, ok := l.blocked[ip]; ok {
		if now.Before(until) {
			return false
		}
		delete(l.blocked, ip)
		delete(l.attempts, ip)
	}
	var keep []time.Time
	for _, t := range l.attempts[ip] {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now)
	l.attempts[ip] = keep
	if len(keep) > l.max {
		l.blocked[ip] = now.Add(l.block)
		return false
	}
	return true
}
