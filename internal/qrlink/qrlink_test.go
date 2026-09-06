package qrlink

import (
	"bytes"
	"strings"
	"testing"
)

func TestLinks(t *testing.T) {
	got := TMe("proxy.example.com", "000102030405060708090a0b0c0d0e0f")
	want := "https://t.me/webproxy?server=proxy.example.com&secret=000102030405060708090a0b0c0d0e0f"
	if got != want {
		t.Fatalf("got %s", got)
	}
	if Tg("proxy.example.com", "aa") != "tg://webproxy?server=proxy.example.com&secret=aa" {
		t.Fatal("tg link wrong")
	}
}

func TestPNGAndDataURI(t *testing.T) {
	png, err := PNG("https://t.me/webproxy?server=a.b&secret=00", 256)
	if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("bad png err=%v", err)
	}
	uri, err := DataURI("x", 128)
	if err != nil || !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("bad uri %q err=%v", uri[:30], err)
	}
}

// The Fake-TLS secret is the "ee" marker, the 16-byte hex secret and the SNI
// domain hex-encoded, exactly as Telegram clients parse it. "example.com" has a
// stable encoding, so it doubles as the regression vector for the hex step.
func TestFakeTLSSecret(t *testing.T) {
	const secret = "000102030405060708090a0b0c0d0e0f"
	got := FakeTLSSecret(secret, "example.com")
	want := "ee" + secret + "6578616d706c652e636f6d"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if !strings.HasPrefix(got, "ee") {
		t.Fatal("fake-tls secret must start with the ee marker")
	}
	if FakeTLSSecret(secret, "") != "ee"+secret {
		t.Fatal("empty domain must contribute no hex")
	}
}

func TestProxyLinks(t *testing.T) {
	secret := FakeTLSSecret("000102030405060708090a0b0c0d0e0f", "example.com")
	tme := TMeProxy("n1.example.com", 8443, secret)
	want := "https://t.me/proxy?server=n1.example.com&port=8443&secret=" + secret
	if tme != want {
		t.Fatalf("tme %s want %s", tme, want)
	}
	tg := TgProxy("n1.example.com", 8443, secret)
	if tg != "tg://proxy?server=n1.example.com&port=8443&secret="+secret {
		t.Fatalf("tg %s", tg)
	}
	// The host is query-escaped like it is in the webproxy links.
	if !strings.Contains(TMeProxy("a b", 443, "s"), "server=a+b") {
		t.Fatalf("host must be query escaped: %s", TMeProxy("a b", 443, "s"))
	}
}
