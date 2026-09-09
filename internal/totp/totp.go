// Package totp wraps RFC 6238 time-based one-time passwords for panel two-factor login.
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
	// secretBytes is the raw entropy behind a secret.
	secretBytes = 20
	period      = 30
	skew        = 1
)

// b32 is unpadded standard base32: the encoding authenticator apps expect in the otpauth `secret` parameter.
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

func Validate(secret, code string, now time.Time) bool {
	ok, err := otptotp.ValidateCustom(code, secret, now.UTC(), otptotp.ValidateOpts{
		Period: period, Skew: skew, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

const recoveryAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// NewRecoveryCodes returns n distinct one-time recovery codes shaped "xxxxx-xxxxx".
func NewRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		return nil, errors.New("totp: recovery code count must be positive")
	}
	out := make([]string, 0, n)
	seen := make(map[string]bool, n)
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

// randomLetter picks one alphabet symbol uniformly.
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
