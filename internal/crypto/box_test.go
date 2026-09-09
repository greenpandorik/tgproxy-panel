package crypto

import (
	"bytes"
	"testing"
)

func testKeys() (map[int][]byte, []byte) {
	k1 := bytes.Repeat([]byte{1}, 32)
	k2 := bytes.Repeat([]byte{2}, 32)
	return map[int][]byte{1: k1, 2: k2}, k2
}

func TestBoxRoundTrip(t *testing.T) {
	keys, _ := testKeys()
	b, err := NewBox(2, keys)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := b.EncryptString("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if b.Version(blob) != 2 {
		t.Fatalf("version = %d", b.Version(blob))
	}
	got, err := b.DecryptString(blob)
	if err != nil || got != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestBoxDecryptsOldVersion(t *testing.T) {
	keys, _ := testKeys()
	old, _ := NewBox(1, keys)
	blob, _ := old.EncryptString("secret")
	cur, _ := NewBox(2, keys)
	got, err := cur.DecryptString(blob)
	if err != nil || got != "secret" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestBoxRejectsTamper(t *testing.T) {
	keys, _ := testKeys()
	b, _ := NewBox(2, keys)
	blob, _ := b.EncryptString("secret")
	blob[len(blob)-1] ^= 0xff
	if _, err := b.Decrypt(blob); err == nil {
		t.Fatal("expected auth failure")
	}
}

func TestBoxUnknownVersion(t *testing.T) {
	keys, _ := testKeys()
	b, _ := NewBox(2, keys)
	if _, err := b.Decrypt([]byte{0, 9, 1, 2, 3}); err == nil {
		t.Fatal("expected unknown version error")
	}
}

func TestNewBoxRequiresCurrentKey(t *testing.T) {
	if _, err := NewBox(3, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewBoxRejectsOutOfRangeVersions(t *testing.T) {
	k := bytes.Repeat([]byte{9}, 32)
	for _, v := range []int{0, -1, 65536, 1 << 20} {
		if _, err := NewBox(v, map[int][]byte{v: k}); err == nil {
			t.Errorf("NewBox(current=%d) accepted", v)
		}
		if _, err := NewBox(1, map[int][]byte{1: k, v: k}); err == nil {
			t.Errorf("NewBox(old key v%d) accepted", v)
		}
	}
	if _, err := NewBox(65535, map[int][]byte{65535: k}); err != nil {
		t.Fatalf("version 65535 must be allowed: %v", err)
	}
}
