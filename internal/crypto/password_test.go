package crypto

import (
	"strings"
	"testing"
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

// TestVerifyPasswordRejectsAbsurdParams covers the PHC-parameter caps: the
// hash string drives argon2's memory and time cost, so an attacker who can
// rewrite a row must not be able to turn one login into a CPU/memory DoS.
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
