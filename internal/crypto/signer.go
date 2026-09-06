package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

type Signer struct{ key []byte }

func NewSigner(key []byte) Signer { return Signer{key: key} }

func (s Signer) mac(value string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s Signer) Sign(value string) string { return value + "." + s.mac(value) }

func (s Signer) Verify(signed string) (string, bool) {
	i := strings.LastIndexByte(signed, '.')
	if i <= 0 {
		return "", false
	}
	value, sig := signed[:i], signed[i+1:]
	if !hmac.Equal([]byte(sig), []byte(s.mac(value))) {
		return "", false
	}
	return value, true
}
