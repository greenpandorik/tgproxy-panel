package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32

	// Bounds on the PHC parameters accepted by VerifyPassword. The hash always
	// comes from our own DB, but a row rewritten by an attacker who reached
	// Postgres would otherwise turn one login attempt into an unbounded
	// memory/CPU burn. 1 GiB and t=64 are far above anything we ever write
	// (64 MiB, t=3).
	maxArgonMemoryKiB = 1 << 20 // 1 GiB, argon2 memory is expressed in KiB
	maxArgonTime      = 64
	maxArgonThreads   = 64
)

// HashPassword returns a PHC-format argon2id string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(password, phc string) (bool, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported hash format")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	if m == 0 || m > maxArgonMemoryKiB {
		return false, fmt.Errorf("argon2 memory parameter out of range: m=%d", m)
	}
	if t == 0 || t > maxArgonTime {
		return false, fmt.Errorf("argon2 time parameter out of range: t=%d", t)
	}
	if p == 0 || p > maxArgonThreads {
		return false, fmt.Errorf("argon2 parallelism parameter out of range: p=%d", p)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
