package crypto

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasswordHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("correct horse", h); !ok {
		t.Fatal("expected match")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("expected mismatch")
	}
	if _, err := VerifyPassword("x", "not-a-phc"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestVerifyPasswordRejectsAbsurdParams(t *testing.T) {
	good, err := HashPassword("pass-123456")
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
		ok, err := VerifyPassword("pass-123456", mutated)
		if err == nil || ok {
			t.Errorf("%s (%s): got ok=%v err=%v, want an error", name, params, ok, err)
		}
	}
	// The parameters we actually write must still verify.
	ok, err := VerifyPassword("pass-123456", good)
	if err != nil || !ok {
		t.Fatalf("our own hash no longer verifies: ok=%v err=%v", ok, err)
	}
}

func TestPasswordHashingIsBoundedInParallel(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	var running, peak atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	orig := argon2Key
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
		return orig(password, salt, 1, 8, 1, keyLen)
	}
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait(); argon2Key = orig }()
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, _ = HashPassword("new password")
			} else {
				_, _ = VerifyPassword("wrong", h)
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
