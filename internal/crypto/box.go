// Package crypto holds encryption, hashing and token helpers.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Box encrypts with AES-256-GCM. Blob layout: 2-byte big-endian key version || 12-byte nonce || ciphertext.
type Box struct {
	current int
	aeads   map[int]cipher.AEAD
}

const maxKeyVersion = 65535

func NewBox(current int, keys map[int][]byte) (*Box, error) {
	if current < 1 || current > maxKeyVersion {
		return nil, fmt.Errorf("current key version %d out of range 1..%d", current, maxKeyVersion)
	}
	b := &Box{current: current, aeads: map[int]cipher.AEAD{}}
	for v, k := range keys {
		if v < 1 || v > maxKeyVersion {
			return nil, fmt.Errorf("key version %d out of range 1..%d", v, maxKeyVersion)
		}
		if len(k) != 32 {
			return nil, fmt.Errorf("key v%d: must be 32 bytes", v)
		}
		block, err := aes.NewCipher(k)
		if err != nil {
			return nil, err
		}
		a, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		b.aeads[v] = a
	}
	if _, ok := b.aeads[current]; !ok {
		return nil, fmt.Errorf("current key version %d not provided", current)
	}
	return b, nil
}

func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	a := b.aeads[b.current]
	nonce := make([]byte, a.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 2, 2+len(nonce)+len(plain)+a.Overhead())
	binary.BigEndian.PutUint16(out, uint16(b.current))
	out = append(out, nonce...)
	return a.Seal(out, nonce, plain, out[:2]), nil
}

func (b *Box) Version(blob []byte) int {
	if len(blob) < 2 {
		return 0
	}
	return int(binary.BigEndian.Uint16(blob))
}

func (b *Box) Decrypt(blob []byte) ([]byte, error) {
	v := b.Version(blob)
	a, ok := b.aeads[v]
	if !ok {
		return nil, fmt.Errorf("unknown key version %d", v)
	}
	if len(blob) < 2+a.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce := blob[2 : 2+a.NonceSize()]
	return a.Open(nil, nonce, blob[2+a.NonceSize():], blob[:2])
}

func (b *Box) EncryptString(s string) ([]byte, error) { return b.Encrypt([]byte(s)) }

func (b *Box) DecryptString(blob []byte) (string, error) {
	p, err := b.Decrypt(blob)
	if err != nil {
		return "", err
	}
	return string(p), nil
}

// CurrentVersion returns the key version used for new encryptions.
func (b *Box) CurrentVersion() int { return b.current }
