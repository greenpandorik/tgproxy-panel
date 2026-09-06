// Package totp wraps RFC 6238 time-based one-time passwords for panel two-factor
// login. It fixes the parameters every authenticator app defaults to (SHA1, 6
// digits, a 30-second period) so that a secret provisioned here works in Google
// Authenticator, Aegis, 1Password and the rest without extra configuration.
package totp

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/pquerna/otp"
	otptotp "github.com/pquerna/otp/totp"
)

const (
	// secretBytes is the raw entropy behind a secret. RFC 4226 §4 requires at
	// least 128 bits and recommends 160; 20 bytes is the recommended size and
	// what every authenticator app expects.
	secretBytes = 20
	// period, digits and the algorithm are not configurable on purpose: they are
	// the defaults of every mainstream authenticator, and a panel that deviated
	// would silently produce codes users cannot generate.
	period = 30
	// skew accepts the step before and after the current one, covering the usual
	// clock drift between a phone and the server without widening the window
	// enough to matter for brute force (the login limiter caps attempts).
	skew = 1
)

// b32 is unpadded standard base32: the encoding authenticator apps expect in the
// otpauth `secret` parameter. Padding "=" is rejected by several of them.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a new 20-byte secret encoded as unpadded base32.
func GenerateSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("totp: read random: %w", err)
	}
	return b32.EncodeToString(b), nil
}

// ProvisioningURL builds the otpauth:// URI that goes into the enrolment QR code.
// The label is "issuer:account" and the issuer is repeated as a parameter, which
// is what the Key URI format asks for.
func ProvisioningURL(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", otp.AlgorithmSHA1.String())
	v.Set("digits", otp.DigitsSix.String())
	v.Set("period", fmt.Sprint(period))
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + issuer + ":" + account, RawQuery: v.Encode()}
	return u.String()
}

// Validate reports whether code is a valid one-time password for secret at now,
// accepting the neighbouring 30-second steps. Every failure mode - a malformed
// secret, a code of the wrong length, a code from another secret - is a plain
// false: callers must not be able to distinguish them.
func Validate(secret, code string, now time.Time) bool {
	ok, err := otptotp.ValidateCustom(code, secret, now.UTC(), otptotp.ValidateOpts{
		Period: period, Skew: skew, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

// recoveryAlphabet excludes nothing on purpose: the codes are copy-pasted or
// pasted from a saved file rather than transcribed, and keeping all 36 symbols
// gives 10 characters ~51.7 bits of entropy - enough that a sha256 hash (no
// stretching) is a sound way to store them.
const recoveryAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// NewRecoveryCodes returns n distinct one-time recovery codes shaped "xxxxx-xxxxx".
func NewRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		return nil, errors.New("totp: recovery code count must be positive")
	}
	out := make([]string, 0, n)
	seen := make(map[string]bool, n)
	// A collision among 51.7-bit codes is vanishingly unlikely, but retrying is
	// cheap and guarantees the caller never stores two identical hashes.
	for attempts := 0; len(out) < n; attempts++ {
		if attempts > 100*n {
			return nil, errors.New("totp: could not generate distinct recovery codes")
		}
		c, err := recoveryCode()
		if err != nil {
			return nil, err
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func recoveryCode() (string, error) {
	var sb strings.Builder
	sb.Grow(11)
	for i := range 10 {
		if i == 5 {
			sb.WriteByte('-')
		}
		c, err := randomLetter()
		if err != nil {
			return "", err
		}
		sb.WriteByte(c)
	}
	return sb.String(), nil
}

// randomLetter picks one alphabet symbol uniformly. Rejection sampling (rather
// than a modulo) keeps the distribution flat: 256 is not a multiple of 36, so
// `b % 36` would favour the first four symbols.
func randomLetter() (byte, error) {
	limit := byte(256 - 256%len(recoveryAlphabet)) // 252
	var b [1]byte
	for {
		if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
			return 0, fmt.Errorf("totp: read random: %w", err)
		}
		if b[0] < limit {
			return recoveryAlphabet[int(b[0])%len(recoveryAlphabet)], nil
		}
	}
}
