package crypto

import (
	"context"
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

	// argonSlots is how many argon2 computations run at once. It is fixed so that peak memory
	// (argonSlots × argonMemory, 128 MiB for our hashes) does not grow with the core count.
	argonSlots = 2
	// argonWaitersPerSlot is how many callers may queue per argon2 slot before new ones get
	// ErrBusy.
	argonWaitersPerSlot = 4
)

// ErrBusy means every argon2 slot is taken and the wait queue is full.
var ErrBusy = errors.New("password hashing is saturated")

// argonLimiter bounds how many argon2 computations run at once and how many wait for a slot.
// Each one allocates its memory parameter and the login endpoint is unauthenticated, so without
// a bound a burst of parallel logins could take the panel down for lack of memory.
type argonLimiter struct {
	slots    chan struct{}
	admitted chan struct{}
}

func newArgonLimiter(slots int) *argonLimiter {
	return &argonLimiter{
		slots:    make(chan struct{}, slots),
		admitted: make(chan struct{}, slots*(1+argonWaitersPerSlot)),
	}
}

var argonLimit = newArgonLimiter(argonSlots)

// argon2Key is argon2.IDKey, replaceable in tests.
var argon2Key = argon2.IDKey

func argonIDKey(ctx context.Context, password, salt []byte, t, m uint32, p uint8, keyLen uint32) ([]byte, error) {
	l := argonLimit
	select {
	case l.admitted <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	defer func() { <-l.admitted }()
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-l.slots }()
	return argon2Key(password, salt, t, m, p, keyLen), nil
}

// HashPassword returns a PHC-format argon2id string.
func HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := argonIDKey(ctx, []byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword reports whether password matches the PHC string. It fails with ErrBusy when
// the argon2 queue is full, and with the context's error when ctx ends while it waits.
func VerifyPassword(ctx context.Context, password, phc string) (bool, error) {
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
	got, err := argonIDKey(ctx, []byte(password), salt, t, m, p, uint32(len(want)))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
