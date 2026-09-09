package totp_test

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	otptotp "github.com/pquerna/otp/totp"

	"tgwebproxy/internal/totp"
)

func TestGenerateSecretIsUsableBase32(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 20 {
		s, err := totp.GenerateSecret()
		if err != nil {
			t.Fatalf("GenerateSecret: %v", err)
		}
		// 20 random bytes base32-encoded without padding is 32 chars.
		if len(s) != 32 {
			t.Fatalf("secret %q: len %d, want 32", s, len(s))
		}
		if strings.ContainsAny(s, "=abcdefghijklmnopqrstuvwxyz") {
			t.Fatalf("secret %q: want unpadded upper-case base32", s)
		}
		if seen[s] {
			t.Fatalf("secret %q repeated", s)
		}
		seen[s] = true
	}
}

func TestValidateAcceptsCurrentAndPreviousStep(t *testing.T) {
	t.Parallel()
	secret, err := totp.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"now", now, true},
		{"previous step", now.Add(-30 * time.Second), true},
		{"next step", now.Add(30 * time.Second), true},
		{"three steps back", now.Add(-90 * time.Second), false},
	} {
		code, err := otptotp.GenerateCode(secret, tc.at)
		if err != nil {
			t.Fatalf("%s: GenerateCode: %v", tc.name, err)
		}
		if got := totp.Validate(secret, code, now); got != tc.want {
			t.Errorf("%s: Validate(%q) = %v, want %v", tc.name, code, got, tc.want)
		}
	}
}

func TestValidateRejectsGarbage(t *testing.T) {
	t.Parallel()
	const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	valid := map[string]bool{}
	for _, off := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		c, err := otptotp.GenerateCode(secret, now.Add(off))
		if err != nil {
			t.Fatal(err)
		}
		valid[c] = true
	}
	wrong := ""
	for i := range 10 {
		c := fmt.Sprintf("%06d", i)
		if !valid[c] {
			wrong = c
			break
		}
	}

	for _, code := range []string{"", wrong, "abcdef", "12345", "1234567", "  123456  "} {
		if totp.Validate(secret, code, now) {
			t.Errorf("Validate accepted %q", code)
		}
	}
	// An unparsable secret can never validate anything.
	if totp.Validate("not base32!", "123456", now) {
		t.Errorf("Validate accepted a code for an invalid secret")
	}
}

func TestProvisioningURL(t *testing.T) {
	t.Parallel()
	raw := totp.ProvisioningURL("My Panel", "alice", "JBSWY3DPEHPK3PXP")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("got %q, want otpauth://totp/...", raw)
	}
	if want := "/My Panel:alice"; u.Path != want {
		t.Errorf("path = %q, want %q", u.Path, want)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"secret": "JBSWY3DPEHPK3PXP", "issuer": "My Panel",
		"algorithm": "SHA1", "digits": "6", "period": "30",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	// The URL must be usable by the reference implementation.
	if _, err := otptotp.GenerateCode(q.Get("secret"), time.Now()); err != nil {
		t.Errorf("secret from URL unusable: %v", err)
	}
}

var recoveryRe = regexp.MustCompile(`^[a-z0-9]{5}-[a-z0-9]{5}$`)

func TestNewRecoveryCodes(t *testing.T) {
	t.Parallel()
	codes, err := totp.NewRecoveryCodes(8)
	if err != nil {
		t.Fatalf("NewRecoveryCodes: %v", err)
	}
	if len(codes) != 8 {
		t.Fatalf("got %d codes, want 8", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !recoveryRe.MatchString(c) {
			t.Errorf("code %q does not match %s", c, recoveryRe)
		}
		if seen[c] {
			t.Errorf("code %q repeated", c)
		}
		seen[c] = true
	}
}

func TestNewRecoveryCodesRejectsNonPositive(t *testing.T) {
	t.Parallel()
	if _, err := totp.NewRecoveryCodes(0); err == nil {
		t.Error("NewRecoveryCodes(0): want error")
	}
}
