package crypto

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasswordHashAndVerify(t *testing.T) {
	ctx := t.Context()
	h, err := HashPassword(ctx, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword(ctx, "correct horse", h); !ok {
		t.Fatal("expected match")
	}
	if ok, _ := VerifyPassword(ctx, "wrong", h); ok {
		t.Fatal("expected mismatch")
	}
	if _, err := VerifyPassword(ctx, "x", "not-a-phc"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestVerifyPasswordRejectsAbsurdParams(t *testing.T) {
	ctx := t.Context()
	good, err := HashPassword(ctx, "pass-123456")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, "$")
	for name, params := range map[string]string{
		"huge memory":  "m=4194304,t=3,p=2", // 4 GiB
		"huge time":    "m=65536,t=1000,p=2",
		"zero memory":  "m=0,t=3,p=2",
		"zero time":    "m=65536,t=0,p=2",
		"zero threads": "m=65536,t=3,p=0",
	} {
		mutated := strings.Join([]string{parts[0], parts[1], parts[2], params, parts[4], parts[5]}, "$")
		ok, err := VerifyPassword(ctx, "pass-123456", mutated)
		if err == nil || ok {
			t.Errorf("%s (%s): got ok=%v err=%v, want an error", name, params, ok, err)
		}
	}
	// The parameters we actually write must still verify.
	ok, err := VerifyPassword(ctx, "pass-123456", good)
	if err != nil || !ok {
		t.Fatalf("our own hash no longer verifies: ok=%v err=%v", ok, err)
	}
}

// blockingArgon swaps in a limiter of the given size and an argon2 stand-in that holds its slot
// until release is closed. Every computation that starts sends on entered.
func blockingArgon(t *testing.T, slots int) (entered chan struct{}, release chan struct{}) {
	t.Helper()
	entered = make(chan struct{}, 64)
	release = make(chan struct{})
	origKey, origLimit := argon2Key, argonLimit
	argonLimit = newArgonLimiter(slots)
	argon2Key = func(password, salt []byte, _, _ uint32, _ uint8, keyLen uint32) []byte {
		entered <- struct{}{}
		<-release
		return origKey(password, salt, 1, 8, 1, keyLen)
	}
	t.Cleanup(func() { argon2Key, argonLimit = origKey, origLimit })
	return entered, release
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPasswordHashingIsBoundedInParallel(t *testing.T) {
	h, err := HashPassword(t.Context(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	var running, peak atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	origKey, origLimit := argon2Key, argonLimit
	argonLimit = newArgonLimiter(2)
	argon2Key = func(password, salt []byte, iterations, memory uint32, threads uint8, keyLen uint32) []byte {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		running.Add(-1)
		return origKey(password, salt, 1, 8, 1, keyLen)
	}
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait(); argon2Key, argonLimit = origKey, origLimit }()
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, _ = HashPassword(context.Background(), "new password")
			} else {
				_, _ = VerifyPassword(context.Background(), "wrong", h)
			}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("two computations did not start")
		}
	}
	select {
	case <-entered:
		t.Fatal("more than two password computations ran before a slot was released")
	case <-time.After(100 * time.Millisecond):
	}
	if p := peak.Load(); p != 2 {
		t.Fatalf("peak = %d, want 2", p)
	}
}

func TestPasswordPoolIsFixedSize(t *testing.T) {
	if got := cap(argonLimit.slots); got != 2 {
		t.Fatalf("argon2 pool has %d slots, want 2 whatever the core count", got)
	}
	if got := cap(argonLimit.admitted); got != 2*(1+argonWaitersPerSlot) {
		t.Fatalf("argon2 queue admits %d, want %d", got, 2*(1+argonWaitersPerSlot))
	}
}

func TestPasswordWaiterLeavesOnCancel(t *testing.T) {
	entered, release := blockingArgon(t, 1)
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = HashPassword(context.Background(), "holder")
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := HashPassword(ctx, "waiter")
		done <- err
	}()
	waitFor(t, "the waiter to queue", func() bool { return len(argonLimit.admitted) == 2 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter: err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled waiter kept waiting for a slot")
	}
	if n := len(argonLimit.admitted); n != 1 {
		t.Fatalf("queue holds %d after the waiter left, want 1", n)
	}
}

func TestPasswordQueueFullFailsFast(t *testing.T) {
	entered, release := blockingArgon(t, 1)
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	for range 1 + argonWaitersPerSlot {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = HashPassword(context.Background(), "queued")
		}()
	}
	<-entered
	waitFor(t, "the queue to fill", func() bool { return len(argonLimit.admitted) == 1+argonWaitersPerSlot })

	start := time.Now()
	_, err := VerifyPassword(context.Background(), "late", "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue: err = %v, want ErrBusy", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("full queue took %v to refuse", d)
	}
}
