package crypto

import "testing"

func TestSignerRoundTrip(t *testing.T) {
	s := NewSigner([]byte("k"))
	signed := s.Sign("session-id")
	v, ok := s.Verify(signed)
	if !ok || v != "session-id" {
		t.Fatalf("verify failed: %q %v", v, ok)
	}
	if _, ok := s.Verify(signed + "x"); ok {
		t.Fatal("tampered signature accepted")
	}
	if _, ok := NewSigner([]byte("other")).Verify(signed); ok {
		t.Fatal("wrong key accepted")
	}
	if _, ok := s.Verify("nodot"); ok {
		t.Fatal("malformed accepted")
	}
}
