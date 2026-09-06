package crypto

import (
	"regexp"
	"testing"
)

func TestNewSecretHex(t *testing.T) {
	s, err := NewSecretHex()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(s) {
		t.Fatalf("bad secret %q", s)
	}
	s2, _ := NewSecretHex()
	if s == s2 {
		t.Fatal("secrets must differ")
	}
}

func TestNewTokenAndHash(t *testing.T) {
	tok, err := NewToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 { // 32 bytes base64url raw
		t.Fatalf("len = %d", len(tok))
	}
	first := HashToken(tok)
	second := HashToken(tok)
	if first != second || first == HashToken("other") {
		t.Fatal("hash must be deterministic and distinct")
	}
	if len(first) != 64 {
		t.Fatal("expected sha256 hex")
	}
}
