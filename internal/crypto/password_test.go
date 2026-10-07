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
		<-release
		running.Add(-1)
		return orig(password, salt, 1, 8, 1, keyLen)
	}
	t.Cleanup(func() { argon2Key = orig })

	callers := 3 * cap(argonSlots)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = VerifyPassword("wrong", h)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for running.Load() < int32(cap(argonSlots)) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := running.Load(); got != int32(cap(argonSlots)) {
		t.Fatalf("%d argon2 computations running at once, want %d", got, cap(argonSlots))
	}
	close(release)
	wg.Wait()
	if p := peak.Load(); p > int32(cap(argonSlots)) {
		t.Fatalf("peak of %d parallel argon2 computations, bound is %d", p, cap(argonSlots))
	}
}
