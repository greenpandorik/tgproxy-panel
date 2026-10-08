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

	// Bounds on the PHC parameters accepted by VerifyPassword.
	maxArgonMemoryKiB = 1 << 20 // 1 GiB, argon2 memory is expressed in KiB
	maxArgonTime      = 64
	maxArgonThreads   = 64
)

// argonSlots bounds how many argon2 computations run at once. Each one allocates its memory
// parameter (64 MiB for our hashes) and the login endpoint is unauthenticated, so without a bound
// a burst of parallel logins could take the panel down for lack of memory. Two slots keep normal
// hashes within 128 MiB regardless of the CPU count. Callers over the bound
// wait for a slot instead.
var argonSlots = make(chan struct{}, 2)

// argon2Key is argon2.IDKey, replaceable in tests.
var argon2Key = argon2.IDKey

func argonIDKey(password, salt []byte, t, m uint32, p uint8, keyLen uint32) []byte {
	argonSlots <- struct{}{}
	defer func() { <-argonSlots }()
	return argon2Key(password, salt, t, m, p, keyLen)
}

// HashPassword returns a PHC-format argon2id string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argonIDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
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
	got := argonIDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
