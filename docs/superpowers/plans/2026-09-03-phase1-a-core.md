# Phase 1 / Part A — Core (skeleton, crypto, store, domain, auth) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Go backend foundation: config, crypto primitives, Postgres schema + sqlc, domain types, key/link/QR generation, and admin authentication with sessions, CSRF, rate limiting, RBAC and audit.

**Architecture:** Single Go module `tgwebproxy` with two binaries (`cmd/panel`, `cmd/agent`). Postgres via pgx/v5 + sqlc + goose (embedded migrations run on startup). chi router. slog JSON logs. All secrets encrypted with AES-256-GCM using a versioned master key from env.

**Tech Stack:** Go 1.26, chi v5, pgx v5, sqlc, goose v3 (library), golang.org/x/crypto (argon2), google/uuid, skip2/go-qrcode, testify.

**Spec:** `docs/superpowers/specs/2026-09-03-webproxy-panel-design.md`

## Global Constraints

- Go `1.26` (go.mod `go 1.26`). Node 20+ only for the frontend build.
- Module path: `tgwebproxy`. Never publish; no external import path needed.
- No git in this project (user decision). Skip commit steps; instead run the full test suite at the end of each task.
- Secrets (profile/key secrets, node tokens, TOTP) are never logged. Types holding secrets implement `LogValue()` returning `slog.StringValue("[redacted]")`.
- Postgres tests read `TEST_DATABASE_URL`; when it is empty, DB-backed tests call `t.Skip`.
- Errors from HTTP API: `{"error":{"code":"...","message":"...","fields":{...}}}`.
- UI/README copy: plain, short, no marketing adjectives.
- Formatting: `gofumpt`. Lint: `golangci-lint run ./...` must be clean.
- WEB proxy secret: exactly 32 lowercase hex chars (16 bytes), no `dd` prefix.
- Reference relay commit: `52a5feb7fac38f68da5afef9cedd9b3bfc8473ca` (`TPROXY_COMMIT` default).

---

## File structure (Part A)

```
go.mod, go.sum
Makefile
.env.example
.golangci.yml
cmd/panel/main.go              # wires config, store, api, workers; subcommands: serve (default), admin create, migrate
cmd/agent/main.go              # placeholder until Part B
internal/config/config.go      # env loader
internal/config/config_test.go
internal/logging/logging.go    # slog setup, Redacted type
internal/crypto/box.go         # AES-256-GCM with key version prefix
internal/crypto/password.go    # argon2id PHC
internal/crypto/token.go       # random secrets/tokens, sha256 token hash
internal/crypto/signer.go      # HMAC signer for cookies
internal/crypto/*_test.go
internal/store/migrations/00001_init.sql
internal/store/migrations/embed.go
internal/store/store.go        # Open, Migrate, Store struct, tx helper
internal/store/testing.go      # OpenTest helper
internal/store/queries/*.sql   # sqlc inputs
internal/store/db/*            # sqlc output (generated)
sqlc.yaml
internal/domain/types.go       # enums, limits, validation
internal/domain/types_test.go
internal/qrlink/qrlink.go
internal/qrlink/qrlink_test.go
internal/api/server.go         # chi router, middleware chain, mounting
internal/api/respond.go        # JSON helpers, error envelope
internal/api/auth.go           # login/logout/me handlers
internal/api/session.go        # session cookie middleware
internal/api/csrf.go
internal/api/ratelimit.go
internal/api/rbac.go
internal/api/audit.go
internal/api/*_test.go
internal/api/apitest/apitest.go # test harness: server + test store + login helper
web/embed.go                   # //go:embed all:dist (placeholder dist/index.html)
web/dist/index.html
```

---

### Task 1: Module skeleton, config loader, logging, Makefile

**Files:**
- Create: `go.mod`, `Makefile`, `.env.example`, `.golangci.yml`
- Create: `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/logging/logging.go`
- Create: `cmd/panel/main.go`, `cmd/agent/main.go`
- Create: `web/embed.go`, `web/dist/index.html`

**Interfaces:**
- Produces: `config.Load(getenv func(string) string) (Config, error)`; `logging.New(level string) *slog.Logger`; `logging.Redacted` type.

- [ ] **Step 1: Init module and install tools**

```bash
cd /Users/mihailvolkov/Desktop/dev/TGProxy_WEB
go mod init tgwebproxy
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install mvdan.cc/gofumpt@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

- [ ] **Step 2: Write failing config test**

`internal/config/config_test.go`:
```go
package config

import (
	"encoding/base64"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
}

func TestLoadDefaultsAndDecodesKeys(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL":   "postgres://u:p@localhost/db",
		"MASTER_KEY":     key,
		"SESSION_SECRET": key,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DataDir != "./data" || cfg.NodeDriver != "gateway" {
		t.Fatalf("bad defaults: %+v", cfg)
	}
	if len(cfg.MasterKey) != 32 || cfg.MasterKeyVersion != 1 {
		t.Fatalf("master key not decoded: %+v", cfg)
	}
	if cfg.TProxyCommit != "52a5feb7fac38f68da5afef9cedd9b3bfc8473ca" {
		t.Fatalf("bad tproxy commit default: %s", cfg.TProxyCommit)
	}
}

func TestLoadRejectsShortMasterKey(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost/db", "MASTER_KEY": short, "SESSION_SECRET": short,
	}))
	if err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoad -v`
Expected: FAIL, `undefined: Load`.

- [ ] **Step 4: Implement config loader**

`internal/config/config.go`:
```go
// Package config loads panel configuration from environment variables.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	MasterKey        []byte
	MasterKeyVersion int
	OldMasterKeys    map[int][]byte // MASTER_KEY_V<n>=base64 for decrypting older versions
	SessionSecret    []byte
	PublicURL        string // e.g. https://panel.example.com, no trailing slash
	DataDir          string
	NodeDriver       string // gateway | mock
	MetricsToken     string
	TProxyCommit     string
	FeatureTOTP      bool
	LogLevel         string
	ApplyInterval    int // seconds
	OfflineAfter     int // seconds
}

const DefaultTProxyCommit = "52a5feb7fac38f68da5afef9cedd9b3bfc8473ca"

func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	cfg := Config{
		HTTPAddr:     get("PANEL_HTTP_ADDR", ":8080"),
		DatabaseURL:  get("DATABASE_URL", ""),
		PublicURL:    strings.TrimRight(get("PANEL_PUBLIC_URL", "http://localhost:8080"), "/"),
		DataDir:      get("DATA_DIR", "./data"),
		NodeDriver:   get("NODE_DRIVER", "gateway"),
		MetricsToken: get("METRICS_TOKEN", ""),
		TProxyCommit: get("TPROXY_COMMIT", DefaultTProxyCommit),
		FeatureTOTP:  get("FEATURE_TOTP", "false") == "true",
		LogLevel:     get("LOG_LEVEL", "info"),
		OldMasterKeys: map[int][]byte{},
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	var err error
	if cfg.MasterKey, err = key32(get("MASTER_KEY", "")); err != nil {
		return cfg, fmt.Errorf("MASTER_KEY: %w", err)
	}
	if cfg.SessionSecret, err = key32(get("SESSION_SECRET", "")); err != nil {
		return cfg, fmt.Errorf("SESSION_SECRET: %w", err)
	}
	if cfg.MasterKeyVersion, err = strconv.Atoi(get("MASTER_KEY_VERSION", "1")); err != nil || cfg.MasterKeyVersion < 1 {
		return cfg, errors.New("MASTER_KEY_VERSION must be a positive integer")
	}
	for v := 1; v < cfg.MasterKeyVersion; v++ {
		if raw := getenv(fmt.Sprintf("MASTER_KEY_V%d", v)); raw != "" {
			k, err := key32(raw)
			if err != nil {
				return cfg, fmt.Errorf("MASTER_KEY_V%d: %w", v, err)
			}
			cfg.OldMasterKeys[v] = k
		}
	}
	if cfg.ApplyInterval, err = strconv.Atoi(get("APPLY_INTERVAL", "45")); err != nil {
		return cfg, errors.New("APPLY_INTERVAL must be integer seconds")
	}
	if cfg.OfflineAfter, err = strconv.Atoi(get("OFFLINE_AFTER", "90")); err != nil {
		return cfg, errors.New("OFFLINE_AFTER must be integer seconds")
	}
	if cfg.NodeDriver != "gateway" && cfg.NodeDriver != "mock" {
		return cfg, errors.New("NODE_DRIVER must be gateway or mock")
	}
	return cfg, nil
}

func key32(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("required (base64 of 32 random bytes)")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("must be standard base64")
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("must decode to 32 bytes, got %d", len(raw))
	}
	return raw, nil
}
```

- [ ] **Step 5: Logging package**

`internal/logging/logging.go`:
```go
// Package logging configures slog and provides a Redacted value type.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Redacted wraps a secret so it never appears in logs.
type Redacted string

func (Redacted) LogValue() slog.Value { return slog.StringValue("[redacted]") }
func (Redacted) String() string       { return "[redacted]" }

func New(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
```

- [ ] **Step 6: Placeholder binaries and embed**

`web/dist/index.html`:
```html
<!doctype html><title>panel</title><p>Frontend not built. Run <code>make web</code>.</p>
```

`web/embed.go`:
```go
// Package web embeds the built SPA.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
```

`cmd/panel/main.go` (minimal; grows in later tasks):
```go
package main

import (
	"fmt"
	"os"

	"tgwebproxy/internal/config"
	"tgwebproxy/internal/logging"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	log := logging.New(cfg.LogLevel)
	log.Info("panel starting", "addr", cfg.HTTPAddr)
}
```

`cmd/agent/main.go`:
```go
package main

import "fmt"

func main() { fmt.Println("tgwp-agent: not implemented yet") }
```

- [ ] **Step 7: Makefile, env example, lint config**

`Makefile`:
```make
GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

.PHONY: tools test lint fmt sqlc proto web run agent-linux

tools:
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

fmt:
	gofumpt -l -w ./cmd ./internal ./proto 2>/dev/null || gofumpt -l -w ./cmd ./internal

lint:
	golangci-lint run ./...

test:
	go test ./...

sqlc:
	sqlc generate

proto:
	protoc -I proto --go_out=. --go_opt=module=tgwebproxy --go-grpc_out=. --go-grpc_opt=module=tgwebproxy proto/agent/v1/agent.proto

web:
	cd web && npm ci && npm run build

run:
	go run ./cmd/panel serve

agent-linux:
	mkdir -p data/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o data/agent/tgwp-agent-linux-amd64 ./cmd/agent
	shasum -a 256 data/agent/tgwp-agent-linux-amd64 | awk '{print $$1}' > data/agent/tgwp-agent-linux-amd64.sha256
```

`.env.example`:
```
DATABASE_URL=postgres://tgwp:tgwp@localhost:5432/tgwp?sslmode=disable
# 32 random bytes, base64: openssl rand -base64 32
MASTER_KEY=
MASTER_KEY_VERSION=1
SESSION_SECRET=
PANEL_HTTP_ADDR=:8080
PANEL_PUBLIC_URL=http://localhost:8080
DATA_DIR=./data
NODE_DRIVER=gateway
METRICS_TOKEN=
TPROXY_COMMIT=52a5feb7fac38f68da5afef9cedd9b3bfc8473ca
FEATURE_TOTP=false
LOG_LEVEL=info
APPLY_INTERVAL=45
OFFLINE_AFTER=90
# tests
TEST_DATABASE_URL=postgres://tgwp:tgwp@localhost:5432/tgwp_test?sslmode=disable
```

`.golangci.yml`:
```yaml
version: "2"
linters:
  default: standard
  enable: [errcheck, govet, staticcheck, unused, ineffassign, misspell]
formatters:
  enable: [gofumpt]
```

- [ ] **Step 8: Run tests and build**

Run: `go mod tidy && go test ./... && go build ./...`
Expected: config tests PASS, both binaries build.

---

### Task 2: Crypto primitives

**Files:**
- Create: `internal/crypto/box.go`, `internal/crypto/password.go`, `internal/crypto/token.go`, `internal/crypto/signer.go`
- Test: `internal/crypto/box_test.go`, `internal/crypto/password_test.go`, `internal/crypto/token_test.go`, `internal/crypto/signer_test.go`

**Interfaces:**
- Produces:
  - `crypto.NewBox(current int, keys map[int][]byte) (*Box, error)`; `(*Box).Encrypt([]byte) ([]byte, error)`; `(*Box).Decrypt([]byte) ([]byte, error)`; `(*Box).EncryptString(string) ([]byte, error)`; `(*Box).DecryptString([]byte) (string, error)`; `(*Box).Version(blob []byte) int`
  - `crypto.HashPassword(string) (string, error)`; `crypto.VerifyPassword(password, phc string) (bool, error)`
  - `crypto.NewSecretHex() (string, error)` (32 lowercase hex); `crypto.NewToken(n int) (string, error)` (base64url raw); `crypto.HashToken(string) string` (sha256 hex)
  - `crypto.NewSigner(key []byte) Signer`; `(Signer).Sign(value string) string`; `(Signer).Verify(signed string) (string, bool)`

- [ ] **Step 1: Write failing tests**

`internal/crypto/box_test.go`:
```go
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
```

`internal/crypto/password_test.go`:
```go
package crypto

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("correct horse", h); !ok {
		t.Fatal("expected match")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("expected mismatch")
	}
	if _, err := VerifyPassword("x", "not-a-phc"); err == nil {
		t.Fatal("expected parse error")
	}
}
```

`internal/crypto/token_test.go`:
```go
package crypto

import (
	"regexp"
	"testing"
)

func TestNewSecretHex(t *testing.T) {
	s, err := NewSecretHex()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(s) {
		t.Fatalf("bad secret %q", s)
	}
	s2, _ := NewSecretHex()
	if s == s2 {
		t.Fatal("secrets must differ")
	}
}

func TestNewTokenAndHash(t *testing.T) {
	tok, err := NewToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 { // 32 bytes base64url raw
		t.Fatalf("len = %d", len(tok))
	}
	if HashToken(tok) != HashToken(tok) || HashToken(tok) == HashToken("other") {
		t.Fatal("hash must be deterministic and distinct")
	}
	if len(HashToken(tok)) != 64 {
		t.Fatal("expected sha256 hex")
	}
}
```

`internal/crypto/signer_test.go`:
```go
package crypto

import "testing"

func TestSignerRoundTrip(t *testing.T) {
	s := NewSigner([]byte("k"))
	signed := s.Sign("session-id")
	v, ok := s.Verify(signed)
	if !ok || v != "session-id" {
		t.Fatalf("verify failed: %q %v", v, ok)
	}
	if _, ok := s.Verify(signed + "x"); ok {
		t.Fatal("tampered signature accepted")
	}
	if _, ok := NewSigner([]byte("other")).Verify(signed); ok {
		t.Fatal("wrong key accepted")
	}
	if _, ok := s.Verify("nodot"); ok {
		t.Fatal("malformed accepted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/crypto/ -v`
Expected: FAIL to compile (undefined symbols).

- [ ] **Step 3: Implement**

`internal/crypto/box.go`:
```go
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

func NewBox(current int, keys map[int][]byte) (*Box, error) {
	b := &Box{current: current, aeads: map[int]cipher.AEAD{}}
	for v, k := range keys {
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
```

`internal/crypto/password.go`:
```go
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
```

`internal/crypto/token.go`:
```go
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// NewSecretHex returns 16 random bytes as 32 lowercase hex chars (WEB proxy secret).
func NewSecretHex() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewToken returns n random bytes base64url-encoded without padding.
func NewToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken hashes a high-entropy token for storage (sha256 hex).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
```

`internal/crypto/signer.go`:
```go
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
```

- [ ] **Step 4: Run tests**

Run: `go mod tidy && go test ./internal/crypto/ -v`
Expected: all PASS.

---

### Task 3: Postgres schema, sqlc, store bootstrap

**Files:**
- Create: `sqlc.yaml`, `internal/store/migrations/00001_init.sql`, `internal/store/migrations/embed.go`, `internal/store/store.go`, `internal/store/testing.go`, `internal/store/queries/admins.sql`, `internal/store/queries/sessions.sql`, `internal/store/queries/audit.sql`, `internal/store/queries/settings.sql`
- Generate: `internal/store/db/*.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `store.Open(ctx, url string) (*Store, error)`; `store.Migrate(ctx, pool *pgxpool.Pool) error`; `type Store struct{ Pool *pgxpool.Pool; Q *db.Queries }`; `(*Store).Tx(ctx, fn func(q *db.Queries) error) error`; `store.OpenTest(t *testing.T) *Store` (skips without `TEST_DATABASE_URL`, migrates, truncates all tables).
- Generated `db.Queries` methods named in the SQL files below.

- [ ] **Step 1: Write sqlc config and migration**

`sqlc.yaml`:
```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/store/queries"
    schema: "internal/store/migrations"
    gen:
      go:
        package: "db"
        out: "internal/store/db"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_empty_slices: true
        overrides:
          - db_type: "uuid"
            go_type: "github.com/google/uuid.UUID"
          - db_type: "uuid"
            nullable: true
            go_type: { import: "github.com/google/uuid", type: "NullUUID" }
          - db_type: "timestamptz"
            go_type: { import: "time", type: "Time" }
          - db_type: "timestamptz"
            nullable: true
            go_type: { import: "time", type: "Time", pointer: true }
          - db_type: "text"
            nullable: true
            go_type: { type: "string", pointer: true }
          - db_type: "jsonb"
            go_type: { type: "[]byte" }
          - db_type: "jsonb"
            nullable: true
            go_type: { type: "[]byte" }
```

`internal/store/migrations/00001_init.sql`:
```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE admin_role AS ENUM ('owner','admin','viewer');
CREATE TYPE node_status AS ENUM ('pending','online','offline','degraded');
CREATE TYPE sync_state AS ENUM ('pending','synced','failed');
CREATE TYPE key_type AS ENUM ('SHARED','PERSONAL');
CREATE TYPE key_status AS ENUM ('pending','active','revoked');
CREATE TYPE apply_status AS ENUM ('queued','running','ok','failed','rolled_back');
CREATE TYPE apply_kind AS ENUM ('profiles','site','both');
CREATE TYPE backup_kind AS ENUM ('manual','scheduled');

CREATE TABLE admin_users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username text NOT NULL UNIQUE,
  password_hash text NOT NULL,
  role admin_role NOT NULL DEFAULT 'admin',
  totp_secret_enc bytea,
  totp_enabled boolean NOT NULL DEFAULT false,
  failed_logins int NOT NULL DEFAULT 0,
  locked_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_codes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
  code_hash text NOT NULL,
  used_at timestamptz
);

CREATE TABLE sessions (
  id text PRIMARY KEY,
  admin_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  ip text NOT NULL DEFAULT '',
  user_agent text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(admin_user_id);

CREATE TABLE nodes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  hostname text NOT NULL UNIQUE,
  public_ip text NOT NULL DEFAULT '',
  acme_email text NOT NULL DEFAULT '',
  status node_status NOT NULL DEFAULT 'pending',
  agent_token_hash text,
  install_token_hash text,
  install_token_expires timestamptz,
  tproxy_version text NOT NULL DEFAULT '',
  agent_version text NOT NULL DEFAULT '',
  max_profiles int NOT NULL DEFAULT 128,
  dirty boolean NOT NULL DEFAULT false,
  last_seen_at timestamptz,
  last_apply_at timestamptz,
  last_health jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE access_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  label text NOT NULL,
  type key_type NOT NULL,
  owner_label text NOT NULL DEFAULT '',
  secret_enc bytea NOT NULL,
  status key_status NOT NULL DEFAULT 'pending',
  carrier_mode text NOT NULL DEFAULT 'https',
  limits jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz,
  revoked_at timestamptz,
  note text NOT NULL DEFAULT '',
  created_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX access_keys_status_idx ON access_keys(status);

CREATE TABLE profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  access_key_id uuid REFERENCES access_keys(id) ON DELETE CASCADE,
  name text NOT NULL,
  secret_enc bytea NOT NULL,
  backend text NOT NULL DEFAULT '127.0.0.1:2398',
  carrier_mode text NOT NULL DEFAULT 'https',
  limits jsonb NOT NULL DEFAULT '{}'::jsonb,
  sync_state sync_state NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(node_id, name)
);
CREATE INDEX profiles_key_idx ON profiles(access_key_id);

CREATE TABLE key_bindings (
  access_key_id uuid NOT NULL REFERENCES access_keys(id) ON DELETE CASCADE,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  PRIMARY KEY (access_key_id, node_id)
);

CREATE TABLE site_templates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  html text NOT NULL,
  assets jsonb NOT NULL DEFAULT '{}'::jsonb,
  is_preset boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE node_sites (
  node_id uuid PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  template_id uuid REFERENCES site_templates(id) ON DELETE SET NULL,
  bundle jsonb NOT NULL,
  bundle_hash text NOT NULL,
  deployed_hash text,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE apply_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  status apply_status NOT NULL DEFAULT 'queued',
  kind apply_kind NOT NULL,
  started_at timestamptz,
  finished_at timestamptz,
  error text NOT NULL DEFAULT '',
  log text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX apply_jobs_node_idx ON apply_jobs(node_id, created_at DESC);

CREATE TABLE branding_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  is_active boolean NOT NULL DEFAULT false,
  panel_name text NOT NULL DEFAULT 'WEB Proxy Panel',
  logo_path text NOT NULL DEFAULT '',
  favicon_path text NOT NULL DEFAULT '',
  primary_color text NOT NULL DEFAULT '#3b82f6',
  accent_color text NOT NULL DEFAULT '#22c55e',
  theme_default text NOT NULL DEFAULT 'dark',
  login_bg_path text NOT NULL DEFAULT '',
  login_text text NOT NULL DEFAULT '',
  support_link text NOT NULL DEFAULT '',
  footer_text text NOT NULL DEFAULT '',
  custom_css text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX branding_profiles_one_active ON branding_profiles(is_active) WHERE is_active;
INSERT INTO branding_profiles (name, is_active) VALUES ('default', true);

CREATE TABLE audit_log (
  id bigserial PRIMARY KEY,
  admin_user_id uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  action text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id text NOT NULL DEFAULT '',
  meta jsonb NOT NULL DEFAULT '{}'::jsonb,
  ip text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_created_idx ON audit_log(created_at DESC);

CREATE TABLE node_stats_snapshots (
  id bigserial PRIMARY KEY,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  taken_at timestamptz NOT NULL DEFAULT now(),
  sessions_live int NOT NULL DEFAULT 0,
  streams_live int NOT NULL DEFAULT 0,
  bytes_up bigint NOT NULL DEFAULT 0,
  bytes_down bigint NOT NULL DEFAULT 0,
  sessions_created bigint NOT NULL DEFAULT 0,
  limit_hits bigint NOT NULL DEFAULT 0,
  mtproxy_raw jsonb NOT NULL DEFAULT '{}'::jsonb,
  relay_raw text NOT NULL DEFAULT ''
);
CREATE INDEX node_stats_node_time_idx ON node_stats_snapshots(node_id, taken_at DESC);

CREATE TABLE alerts (
  id bigserial PRIMARY KEY,
  node_id uuid REFERENCES nodes(id) ON DELETE CASCADE,
  kind text NOT NULL,
  message text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);

CREATE TABLE subscription_tokens (
  token_hash text PRIMARY KEY,
  access_key_id uuid NOT NULL REFERENCES access_keys(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);

CREATE TABLE settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL
);

CREATE TABLE backups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  path text NOT NULL,
  size bigint NOT NULL DEFAULT 0,
  kind backup_kind NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS backups, settings, subscription_tokens, alerts, node_stats_snapshots, audit_log,
  branding_profiles, apply_jobs, node_sites, site_templates, key_bindings, profiles, access_keys,
  nodes, sessions, recovery_codes, admin_users;
DROP TYPE IF EXISTS backup_kind, apply_kind, apply_status, key_status, key_type, sync_state, node_status, admin_role;
```

`internal/store/migrations/embed.go`:
```go
// Package migrations embeds goose SQL migrations.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 2: Queries for admins, sessions, audit, settings**

`internal/store/queries/admins.sql`:
```sql
-- name: CreateAdmin :one
INSERT INTO admin_users (username, password_hash, role) VALUES ($1, $2, $3) RETURNING *;

-- name: GetAdminByUsername :one
SELECT * FROM admin_users WHERE username = $1;

-- name: GetAdmin :one
SELECT * FROM admin_users WHERE id = $1;

-- name: ListAdmins :many
SELECT * FROM admin_users ORDER BY created_at;

-- name: CountAdmins :one
SELECT count(*) FROM admin_users;

-- name: UpdateAdminPassword :exec
UPDATE admin_users SET password_hash = $2 WHERE id = $1;

-- name: UpdateAdminRole :exec
UPDATE admin_users SET role = $2 WHERE id = $1;

-- name: RecordFailedLogin :exec
UPDATE admin_users SET failed_logins = failed_logins + 1,
  locked_until = CASE WHEN failed_logins + 1 >= 20 THEN now() + interval '15 minutes' ELSE locked_until END
WHERE id = $1;

-- name: ResetFailedLogins :exec
UPDATE admin_users SET failed_logins = 0, locked_until = NULL WHERE id = $1;

-- name: DeleteAdmin :exec
DELETE FROM admin_users WHERE id = $1;

-- name: SetAdminTOTP :exec
UPDATE admin_users SET totp_secret_enc = $2, totp_enabled = $3 WHERE id = $1;
```

`internal/store/queries/sessions.sql`:
```sql
-- name: CreateSession :exec
INSERT INTO sessions (id, admin_user_id, expires_at, ip, user_agent) VALUES ($1, $2, $3, $4, $5);

-- name: GetSession :one
SELECT s.*, u.username, u.role FROM sessions s JOIN admin_users u ON u.id = s.admin_user_id
WHERE s.id = $1 AND s.expires_at > now();

-- name: TouchSession :exec
UPDATE sessions SET expires_at = $2 WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE admin_user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();
```

`internal/store/queries/audit.sql`:
```sql
-- name: InsertAudit :exec
INSERT INTO audit_log (admin_user_id, action, target_type, target_id, meta, ip) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAudit :many
SELECT a.*, u.username FROM audit_log a LEFT JOIN admin_users u ON u.id = a.admin_user_id
ORDER BY a.created_at DESC LIMIT $1 OFFSET $2;

-- name: CountAudit :one
SELECT count(*) FROM audit_log;
```

`internal/store/queries/settings.sql`:
```sql
-- name: GetSetting :one
SELECT value FROM settings WHERE key = $1;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

-- name: ListSettings :many
SELECT * FROM settings ORDER BY key;
```

- [ ] **Step 3: Generate**

Run: `sqlc generate`
Expected: `internal/store/db/` contains `models.go`, `db.go`, `admins.sql.go`, `sessions.sql.go`, `audit.sql.go`, `settings.sql.go`. If sqlc complains about an override, fix the override rather than the SQL.

- [ ] **Step 4: Write failing store test**

`internal/store/store_test.go`:
```go
package store_test

import (
	"context"
	"testing"

	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func TestMigrateAndQuery(t *testing.T) {
	s := store.OpenTest(t)
	ctx := context.Background()
	u, err := s.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "root", PasswordHash: "x", Role: db.AdminRoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Q.GetAdminByUsername(ctx, "root")
	if err != nil || got.ID != u.ID {
		t.Fatalf("got %+v err %v", got, err)
	}
	n, err := s.Q.CountAdmins(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count %d err %v", n, err)
	}
}

func TestOpenTestTruncatesBetweenTests(t *testing.T) {
	s := store.OpenTest(t)
	n, _ := s.Q.CountAdmins(context.Background())
	if n != 0 {
		t.Fatalf("expected clean db, got %d admins", n)
	}
}

func TestTxRollsBack(t *testing.T) {
	s := store.OpenTest(t)
	ctx := context.Background()
	_ = s.Tx(ctx, func(q *db.Queries) error {
		_, _ = q.CreateAdmin(ctx, db.CreateAdminParams{Username: "a", PasswordHash: "x", Role: db.AdminRoleAdmin})
		return context.Canceled
	})
	n, _ := s.Q.CountAdmins(ctx)
	if n != 0 {
		t.Fatalf("expected rollback, got %d", n)
	}
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/store/ -v`
Expected: compile error, `undefined: store.OpenTest`.

- [ ] **Step 6: Implement store**

`internal/store/store.go`:
```go
// Package store owns database access: pool, migrations, sqlc queries.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/store/migrations"
)

type Store struct {
	Pool *pgxpool.Pool
	Q    *db.Queries
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{Pool: pool, Q: db.New(pool)}, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	return goose.UpContext(ctx, sqlDB, ".")
}

// Tx runs fn inside a transaction; returning an error rolls back.
func (s *Store) Tx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(s.Q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Close() { s.Pool.Close() }
```

`internal/store/testing.go`:
```go
package store

import (
	"context"
	"os"
	"testing"
)

// OpenTest connects to TEST_DATABASE_URL, migrates and truncates every table. Skips when unset.
func OpenTest(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := Migrate(ctx, s.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err = s.Pool.Exec(ctx, `TRUNCATE backups, settings, subscription_tokens, alerts, node_stats_snapshots,
		audit_log, apply_jobs, node_sites, site_templates, key_bindings, profiles, access_keys, nodes,
		sessions, recovery_codes, admin_users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	_, _ = s.Pool.Exec(ctx, `DELETE FROM branding_profiles; INSERT INTO branding_profiles (name, is_active) VALUES ('default', true)`)
	t.Cleanup(s.Close)
	return s
}
```

- [ ] **Step 7: Create the test database and run**

```bash
brew services start postgresql@16 2>/dev/null || true
createuser -s tgwp 2>/dev/null || true
psql -d postgres -c "ALTER USER tgwp PASSWORD 'tgwp'" 
createdb -O tgwp tgwp 2>/dev/null || true
createdb -O tgwp tgwp_test 2>/dev/null || true
export TEST_DATABASE_URL='postgres://tgwp:tgwp@localhost:5432/tgwp_test?sslmode=disable'
go mod tidy && go test ./internal/store/ -v
```
Expected: 3 tests PASS. Document the export in README later (Part D).

- [ ] **Step 8: Wire `migrate` into panel main**

Add to `cmd/panel/main.go` a subcommand switch: `serve` (default), `migrate`. `migrate` opens store, runs `store.Migrate`, prints `migrations applied`. `serve` for now: open store, migrate, log, exit 0. Run `go build ./... && go test ./...`.

---

### Task 4: Domain types, limits validation, links and QR

**Files:**
- Create: `internal/domain/types.go`, `internal/domain/types_test.go`
- Create: `internal/qrlink/qrlink.go`, `internal/qrlink/qrlink_test.go`

**Interfaces:**
- Produces:
  - `domain.KeyType` (`KeyShared`, `KeyPersonal`), `domain.KeyStatus` (`KeyPending`, `KeyActive`, `KeyRevoked`), `domain.CarrierMode` with `Valid()`, `domain.AllCarrierModes`
  - `domain.ProfileLimits` struct (json tags exactly as relay) + `Validate() error`
  - `domain.ValidateSecretHex(string) error`, `domain.ValidateHostname(string) error`, `domain.ProfileName(keyID uuid.UUID) string`
  - `qrlink.TMe(host, secret string) string`, `qrlink.Tg(host, secret string) string`, `qrlink.PNG(content string, size int) ([]byte, error)`, `qrlink.DataURI(content string, size int) (string, error)`

- [ ] **Step 1: Write failing tests**

`internal/domain/types_test.go`:
```go
package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestCarrierModeValid(t *testing.T) {
	for _, m := range []CarrierMode{"https", "https-lanes", "websocket", "websocket-lanes"} {
		if !m.Valid() {
			t.Errorf("%s should be valid", m)
		}
	}
	if CarrierMode("tcp").Valid() || CarrierMode("").Valid() {
		t.Error("invalid modes accepted")
	}
}

func TestValidateSecretHex(t *testing.T) {
	if err := ValidateSecretHex("0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "dd0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF", "zz"} {
		if ValidateSecretHex(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateHostname(t *testing.T) {
	if err := ValidateHostname("proxy.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Proxy.Example.com", "localhost", "https://a.b", "a.b/", "-a.b"} {
		if ValidateHostname(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestProfileName(t *testing.T) {
	id := uuid.MustParse("0d5e7c1a-1234-4bcd-9ef0-112233445566")
	if got := ProfileName(id); got != "k0d5e7c1a1234" {
		t.Fatalf("got %s", got)
	}
}

func TestProfileLimitsValidate(t *testing.T) {
	ok := ProfileLimits{MaxSessions: 10, MaxStreams: 100, MaxStreamsPerSession: 10}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := ProfileLimits{MaxStreams: 5, MaxStreamsPerSession: 10}
	if bad.Validate() == nil {
		t.Fatal("max_streams_per_session > max_streams accepted")
	}
	neg := ProfileLimits{MaxSessions: -1}
	if neg.Validate() == nil {
		t.Fatal("negative accepted")
	}
}
```

`internal/qrlink/qrlink_test.go`:
```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/ ./internal/qrlink/`
Expected: compile errors.

- [ ] **Step 3: Implement domain**

`internal/domain/types.go`:
```go
// Package domain holds entity types and validation rules shared by the panel.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type KeyType string

const (
	KeyShared   KeyType = "SHARED"
	KeyPersonal KeyType = "PERSONAL"
)

type KeyStatus string

const (
	KeyPending KeyStatus = "pending"
	KeyActive  KeyStatus = "active"
	KeyRevoked KeyStatus = "revoked"
)

type NodeStatus string

const (
	NodePending  NodeStatus = "pending"
	NodeOnline   NodeStatus = "online"
	NodeOffline  NodeStatus = "offline"
	NodeDegraded NodeStatus = "degraded"
)

type CarrierMode string

var AllCarrierModes = []CarrierMode{"https", "https-lanes", "websocket", "websocket-lanes"}

func (c CarrierMode) Valid() bool {
	for _, m := range AllCarrierModes {
		if m == c {
			return true
		}
	}
	return false
}

// ProfileLimits mirrors the relay's per-profile limits. Zero means "inherit global".
type ProfileLimits struct {
	MaxSessions             int `json:"max_sessions,omitempty"`
	MaxStreams              int `json:"max_streams,omitempty"`
	MaxBackendDialsInFlight int `json:"max_backend_dials_in_flight,omitempty"`
	NewSessionsPerMinute    int `json:"new_sessions_per_minute,omitempty"`
	NewSessionsBurst        int `json:"new_sessions_burst,omitempty"`
	NewStreamsPerMinute     int `json:"new_streams_per_minute,omitempty"`
	NewStreamsBurst         int `json:"new_streams_burst,omitempty"`
	MaxStreamsPerSession    int `json:"max_streams_per_session,omitempty"`
	MaxPendingPerSession    int `json:"max_pending_per_session,omitempty"`
}

func (l ProfileLimits) Validate() error {
	for name, v := range map[string]int{
		"max_sessions": l.MaxSessions, "max_streams": l.MaxStreams,
		"max_backend_dials_in_flight": l.MaxBackendDialsInFlight,
		"new_sessions_per_minute": l.NewSessionsPerMinute, "new_sessions_burst": l.NewSessionsBurst,
		"new_streams_per_minute": l.NewStreamsPerMinute, "new_streams_burst": l.NewStreamsBurst,
		"max_streams_per_session": l.MaxStreamsPerSession, "max_pending_per_session": l.MaxPendingPerSession,
	} {
		if v < 0 {
			return fmt.Errorf("%s must be >= 0", name)
		}
	}
	if l.MaxStreams > 0 && l.MaxStreamsPerSession > l.MaxStreams {
		return errors.New("max_streams_per_session must not exceed max_streams")
	}
	if l.MaxStreams > 0 && l.MaxBackendDialsInFlight > l.MaxStreams {
		return errors.New("max_backend_dials_in_flight must not exceed max_streams")
	}
	return nil
}

var secretRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidateSecretHex(s string) error {
	if !secretRe.MatchString(s) {
		return errors.New("secret must be 32 lowercase hex characters")
	}
	return nil
}

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

func ValidateHostname(h string) error {
	if !hostRe.MatchString(h) || !strings.Contains(h, ".") || len(h) > 253 {
		return errors.New("hostname must be a lowercase DNS name with a dot, no scheme or path")
	}
	return nil
}

// ProfileName derives the relay profile name for a key: "k" + first 12 hex of the uuid.
func ProfileName(keyID uuid.UUID) string {
	return "k" + strings.ReplaceAll(keyID.String(), "-", "")[:12]
}
```

`internal/qrlink/qrlink.go`:
```go
// Package qrlink builds WEB proxy links and QR codes.
package qrlink

import (
	"encoding/base64"
	"net/url"

	qrcode "github.com/skip2/go-qrcode"
)

func params(host, secret string) string {
	v := url.Values{}
	v.Set("server", host)
	v.Set("secret", secret)
	return v.Encode() // keys sorted: secret, server -> we want server first
}

func TMe(host, secret string) string {
	return "https://t.me/webproxy?server=" + url.QueryEscape(host) + "&secret=" + url.QueryEscape(secret)
}

func Tg(host, secret string) string {
	return "tg://webproxy?server=" + url.QueryEscape(host) + "&secret=" + url.QueryEscape(secret)
}

func PNG(content string, size int) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, size)
}

func DataURI(content string, size int) (string, error) {
	png, err := PNG(content, size)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
```
(Remove the unused `params` helper before finishing; it is shown only to explain why manual ordering is used.)

- [ ] **Step 4: Run tests**

Run: `go mod tidy && go test ./internal/domain/ ./internal/qrlink/ -v`
Expected: PASS.

---

### Task 5: HTTP server, auth, sessions, CSRF, rate limit, RBAC, audit, admin CLI

**Files:**
- Create: `internal/api/server.go`, `internal/api/respond.go`, `internal/api/session.go`, `internal/api/csrf.go`, `internal/api/ratelimit.go`, `internal/api/rbac.go`, `internal/api/audit.go`, `internal/api/auth.go`
- Create: `internal/api/apitest/apitest.go`
- Test: `internal/api/auth_test.go`, `internal/api/respond_test.go`, `internal/api/ratelimit_test.go`
- Modify: `cmd/panel/main.go` (serve + `admin create`)

**Interfaces:**
- Produces:
  - `api.Deps{Store *store.Store; Box *crypto.Box; Signer crypto.Signer; Log *slog.Logger; Cfg config.Config; Driver nodedriver.Driver (added in Part B, nil-safe now); Apply ApplyTrigger (Part C)}` and `api.New(deps Deps) *Server` with `(*Server).Handler() http.Handler` (chi router). Later parts add routes via methods `mountNodes`, `mountKeys`, etc. inside `Handler()`.
  - `api.Principal{UserID uuid.UUID; Username string; Role string; SessionID string}` and `api.PrincipalFrom(ctx) (Principal, bool)`
  - `api.RequireRole(roles ...string) func(http.Handler) http.Handler`
  - `api.Audit(ctx, action, targetType, targetID string, meta map[string]any)` — records audit row using principal and IP from ctx
  - `respond.JSON(w, status, v)`, `respond.Error(w, status, code, msg)`, `respond.Fields(w, fields map[string]string)`, `decodeJSON(r, &v) error`
  - `apitest.New(t) *apitest.Harness` with fields `Server *httptest.Server`, `Store *store.Store`, `Box *crypto.Box`; methods `CreateAdmin(username, password, role) uuid.UUID`, `Login(username, password) *apitest.Client`; `Client` has `Get(path) *http.Response`, `Post(path string, body any) *http.Response`, `Patch`, `Delete`, `JSON(resp, &out)`; the client stores cookies and sends `X-CSRF-Token` automatically.

- [ ] **Step 1: Write failing tests**

`internal/api/respond_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 400, "bad_request", "nope", map[string]string{"name": "required"})
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"]["code"] != "bad_request" || body["error"]["message"] != "nope" {
		t.Fatalf("body %s", rec.Body.String())
	}
	if body["error"]["fields"].(map[string]any)["name"] != "required" {
		t.Fatalf("fields missing: %s", rec.Body.String())
	}
}
```

`internal/api/ratelimit_test.go`:
```go
package api

import (
	"testing"
	"time"
)

func TestIPLimiter(t *testing.T) {
	l := newIPLimiter(3, time.Minute, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d blocked", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("4th attempt should be blocked")
	}
	if !l.Allow("5.6.7.8") {
		t.Fatal("other ip blocked")
	}
	l.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if !l.Allow("1.2.3.4") {
		t.Fatal("block should expire")
	}
}
```

`internal/api/auth_test.go`:
```go
package api_test

import (
	"net/http"
	"testing"

	"tgwebproxy/internal/api/apitest"
)

func TestLoginLogoutMe(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	resp := c.Get("/api/v1/auth/me")
	c.JSON(resp, &me)
	if resp.StatusCode != 200 || me.Username != "root" || me.Role != "owner" {
		t.Fatalf("me: %d %+v", resp.StatusCode, me)
	}

	if resp := c.Post("/api/v1/auth/logout", nil); resp.StatusCode != 204 {
		t.Fatalf("logout %d", resp.StatusCode)
	}
	if resp := c.Get("/api/v1/auth/me"); resp.StatusCode != 401 {
		t.Fatalf("after logout expected 401, got %d", resp.StatusCode)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	resp := h.Anonymous().Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "nope"})
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMutationRequiresCSRF(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	c.SendCSRF = false
	if resp := c.Post("/api/v1/auth/logout", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf, got %d", resp.StatusCode)
	}
}

func TestViewerCannotMutate(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	c := h.Login("v", "pass-123456")
	resp := c.Post("/api/v1/me/password", map[string]string{"current": "pass-123456", "new": "pass-654321"})
	// password change is allowed for everyone, so use an owner-only route instead
	_ = resp
	resp = c.Post("/api/v1/admins", map[string]string{"username": "x", "password": "pass-123456", "role": "admin"})
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestPasswordChangeKillsOtherSessions(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c1 := h.Login("root", "pass-123456")
	c2 := h.Login("root", "pass-123456")
	resp := c1.Post("/api/v1/me/password", map[string]string{"current": "pass-123456", "new": "pass-654321"})
	if resp.StatusCode != 204 {
		t.Fatalf("change %d", resp.StatusCode)
	}
	if resp := c2.Get("/api/v1/auth/me"); resp.StatusCode != 401 {
		t.Fatalf("other session should be dead, got %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	a := h.Anonymous()
	var last int
	for i := 0; i < 11; i++ {
		last = a.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "bad"}).StatusCode
	}
	if last != 429 {
		t.Fatalf("expected 429 on 11th attempt, got %d", last)
	}
}

func TestAuditRecordsLogin(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	h.Login("root", "pass-123456")
	n, _ := h.Store.Q.CountAudit(t.Context())
	if n != 1 {
		t.Fatalf("expected 1 audit row, got %d", n)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... 2>&1 | head`
Expected: compile errors.

- [ ] **Step 3: Implement respond helpers**

`internal/api/respond.go`:
```go
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type errorBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string, fields map[string]string) {
	var b errorBody
	b.Error.Code, b.Error.Message, b.Error.Fields = code, msg, fields
	writeJSON(w, status, b)
}

func badRequest(w http.ResponseWriter, msg string)          { writeError(w, 400, "bad_request", msg, nil) }
func unauthorized(w http.ResponseWriter)                    { writeError(w, 401, "unauthorized", "authentication required", nil) }
func forbidden(w http.ResponseWriter)                       { writeError(w, 403, "forbidden", "not allowed", nil) }
func notFound(w http.ResponseWriter)                        { writeError(w, 404, "not_found", "not found", nil) }
func conflict(w http.ResponseWriter, msg string)            { writeError(w, 409, "conflict", msg, nil) }
func validation(w http.ResponseWriter, f map[string]string) { writeError(w, 422, "validation", "invalid input", f) }
func internal(w http.ResponseWriter)                        { writeError(w, 500, "internal", "internal error", nil) }

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}
```

- [ ] **Step 4: Implement session middleware + principal**

`internal/api/session.go`:
```go
package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

const (
	sessionCookie = "tgwp_session"
	csrfCookie    = "tgwp_csrf"
	sessionTTL    = 24 * time.Hour
)

type Principal struct {
	UserID    uuid.UUID
	Username  string
	Role      string
	SessionID string
}

type ctxKey int

const (
	ctxPrincipal ctxKey = iota
	ctxIP
)

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxPrincipal).(Principal)
	return p, ok
}

func ipFrom(ctx context.Context) string {
	s, _ := ctx.Value(ctxIP).(string)
	return s
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// withIP stores the client IP in the context for audit and rate limiting.
func withIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxIP, clientIP(r))))
	})
}

// loadSession attaches a Principal when a valid signed session cookie is present. It never rejects.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		id, ok := s.signer.Verify(c.Value)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		row, err := s.store.Q.GetSession(r.Context(), id)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if time.Until(row.ExpiresAt) < sessionTTL/2 {
			_ = s.store.Q.TouchSession(r.Context(), db.TouchSessionParams{ID: id, ExpiresAt: time.Now().Add(sessionTTL)})
		}
		p := Principal{UserID: row.AdminUserID, Username: row.Username, Role: string(row.Role), SessionID: id}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
	})
}

func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom(r.Context()); !ok {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	id, err := crypto.NewToken(32)
	if err != nil {
		return err
	}
	if err := s.store.Q.CreateSession(r.Context(), db.CreateSessionParams{
		ID: id, AdminUserID: userID, ExpiresAt: time.Now().Add(sessionTTL),
		Ip: ipFrom(r.Context()), UserAgent: r.UserAgent(),
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.signer.Sign(id), Path: "/", HttpOnly: true,
		Secure: s.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	csrf, _ := crypto.NewToken(32)
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: csrf, Path: "/", HttpOnly: false,
		Secure: s.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

func (s *Server) clearSession(w http.ResponseWriter) {
	for _, n := range []string{sessionCookie, csrfCookie} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", MaxAge: -1, HttpOnly: n == sessionCookie})
	}
}
```

- [ ] **Step 5: CSRF, rate limit, RBAC, audit**

`internal/api/csrf.go`:
```go
package api

import (
	"crypto/subtle"
	"net/http"
)

// csrfCheck enforces double-submit: cookie value must equal X-CSRF-Token header on unsafe methods.
func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(csrfCookie)
		h := r.Header.Get("X-CSRF-Token")
		if err != nil || h == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) != 1 {
			writeError(w, http.StatusForbidden, "csrf", "missing or invalid CSRF token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

`internal/api/ratelimit.go`:
```go
package api

import (
	"sync"
	"time"
)

type ipLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	block    time.Duration
	attempts map[string][]time.Time
	blocked  map[string]time.Time
	now      func() time.Time
}

func newIPLimiter(max int, window, block time.Duration) *ipLimiter {
	return &ipLimiter{max: max, window: window, block: block, attempts: map[string][]time.Time{}, blocked: map[string]time.Time{}, now: time.Now}
}

// Allow records an attempt and reports whether it is permitted.
func (l *ipLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if until, ok := l.blocked[ip]; ok {
		if now.Before(until) {
			return false
		}
		delete(l.blocked, ip)
		delete(l.attempts, ip)
	}
	var keep []time.Time
	for _, t := range l.attempts[ip] {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now)
	l.attempts[ip] = keep
	if len(keep) > l.max {
		l.blocked[ip] = now.Add(l.block)
		return false
	}
	return true
}
```

`internal/api/rbac.go`:
```go
package api

import "net/http"

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				unauthorized(w)
				return
			}
			if !allowed[p.Role] {
				forbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writers is the role set allowed to mutate resources.
var writers = []string{RoleOwner, RoleAdmin}
```

`internal/api/audit.go`:
```go
package api

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

// Audit records an admin action. meta must not contain secrets.
func (s *Server) Audit(ctx context.Context, action, targetType, targetID string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	raw, _ := json.Marshal(meta)
	var uid uuid.NullUUID
	if p, ok := PrincipalFrom(ctx); ok {
		uid = uuid.NullUUID{UUID: p.UserID, Valid: true}
	}
	if err := s.store.Q.InsertAudit(ctx, db.InsertAuditParams{
		AdminUserID: uid, Action: action, TargetType: targetType, TargetID: targetID, Meta: raw, Ip: ipFrom(ctx),
	}); err != nil {
		s.log.Error("audit insert failed", "err", err, "action", action)
	}
}
```

- [ ] **Step 6: Server and auth handlers**

`internal/api/server.go`:
```go
// Package api serves the panel HTTP API and the embedded SPA.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store"
	"tgwebproxy/web"
)

type Deps struct {
	Store  *store.Store
	Box    *crypto.Box
	Signer crypto.Signer
	Log    *slog.Logger
	Cfg    config.Config
	// Driver and Apply are added by later parts (nodedriver.Driver, worker trigger).
}

type Server struct {
	store         *store.Store
	box           *crypto.Box
	signer        crypto.Signer
	log           *slog.Logger
	cfg           config.Config
	secureCookies bool
	loginLimiter  *ipLimiter
}

func New(d Deps) *Server {
	return &Server{
		store: d.Store, box: d.Box, signer: d.Signer, log: d.Log, cfg: d.Cfg,
		secureCookies: strings.HasPrefix(d.Cfg.PublicURL, "https://"),
		loginLimiter:  newIPLimiter(10, 10*time.Minute, 15*time.Minute),
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, withIP, s.loadSession)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, csrfCheck)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)
			r.Post("/me/password", s.handleChangePassword)
			r.With(RequireRole(RoleOwner)).Get("/admins", s.handleListAdmins)
			r.With(RequireRole(RoleOwner)).Post("/admins", s.handleCreateAdmin)
			r.With(RequireRole(RoleOwner)).Delete("/admins/{id}", s.handleDeleteAdmin)
			s.mountProtected(r) // later parts add nodes, keys, etc.
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { notFound(w) })
	})

	r.Handle("/*", spaHandler())
	return r
}

// mountProtected is extended in later tasks (nodes, keys, sites, branding, dashboard).
func (s *Server) mountProtected(r chi.Router) {}

func spaHandler() http.Handler {
	dist, _ := fs.Sub(web.Dist, "dist")
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := dist.Open(p); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
```

`internal/api/auth.go`:
```go
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(ipFrom(r.Context())) {
		writeError(w, 429, "rate_limited", "too many attempts, try later", nil)
		return
	}
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	u, err := s.store.Q.GetAdminByUsername(r.Context(), req.Username)
	if err != nil {
		// burn time to keep timing similar
		_, _ = crypto.VerifyPassword(req.Password, "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		writeError(w, 401, "invalid_credentials", "wrong username or password", nil)
		return
	}
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		writeError(w, 423, "locked", "account temporarily locked", nil)
		return
	}
	ok, _ := crypto.VerifyPassword(req.Password, u.PasswordHash)
	if !ok {
		_ = s.store.Q.RecordFailedLogin(r.Context(), u.ID)
		writeError(w, 401, "invalid_credentials", "wrong username or password", nil)
		return
	}
	_ = s.store.Q.ResetFailedLogins(r.Context(), u.ID)
	if err := s.issueSession(w, r, u.ID); err != nil {
		s.log.Error("issue session", "err", err)
		internal(w)
		return
	}
	ctx := r.Context()
	s.Audit(withPrincipal(ctx, Principal{UserID: u.ID, Username: u.Username, Role: string(u.Role)}), "auth.login", "admin", u.ID.String(), nil)
	writeJSON(w, 200, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	writeJSON(w, 200, map[string]any{"id": p.UserID, "username": p.Username, "role": p.Role, "features": map[string]bool{"totp": s.cfg.FeatureTOTP}})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	_ = s.store.Q.DeleteSession(r.Context(), p.SessionID)
	s.clearSession(w)
	w.WriteHeader(204)
}

type changePasswordReq struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req changePasswordReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(req.New) < 10 {
		validation(w, map[string]string{"new": "at least 10 characters"})
		return
	}
	u, err := s.store.Q.GetAdmin(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	if ok, _ := crypto.VerifyPassword(req.Current, u.PasswordHash); !ok {
		validation(w, map[string]string{"current": "wrong password"})
		return
	}
	hash, err := crypto.HashPassword(req.New)
	if err != nil {
		internal(w)
		return
	}
	if err := s.store.Q.UpdateAdminPassword(r.Context(), db.UpdateAdminPasswordParams{ID: p.UserID, PasswordHash: hash}); err != nil {
		internal(w)
		return
	}
	_ = s.store.Q.DeleteUserSessions(r.Context(), p.UserID)
	if err := s.issueSession(w, r, p.UserID); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "auth.password_changed", "admin", p.UserID.String(), nil)
	w.WriteHeader(204)
}

type createAdminReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (s *Server) handleListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListAdmins(r.Context())
	if err != nil {
		internal(w)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, u := range rows {
		out = append(out, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "totp_enabled": u.TotpEnabled, "created_at": u.CreatedAt})
	}
	writeJSON(w, 200, map[string]any{"items": out, "total": len(out)})
}

func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req createAdminReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	fields := map[string]string{}
	if len(req.Username) < 2 {
		fields["username"] = "at least 2 characters"
	}
	if len(req.Password) < 10 {
		fields["password"] = "at least 10 characters"
	}
	if req.Role != RoleOwner && req.Role != RoleAdmin && req.Role != RoleViewer {
		fields["role"] = "owner, admin or viewer"
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		internal(w)
		return
	}
	u, err := s.store.Q.CreateAdmin(r.Context(), db.CreateAdminParams{Username: req.Username, PasswordHash: hash, Role: db.AdminRole(req.Role)})
	if err != nil {
		conflict(w, "username already exists")
		return
	}
	s.Audit(r.Context(), "admin.create", "admin", u.ID.String(), map[string]any{"username": u.Username, "role": u.Role})
	writeJSON(w, 201, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
}

func (s *Server) handleDeleteAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return
	}
	p, _ := PrincipalFrom(r.Context())
	if p.UserID == id {
		conflict(w, "cannot delete yourself")
		return
	}
	if err := s.store.Q.DeleteAdmin(r.Context(), id); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "admin.delete", "admin", id.String(), nil)
	w.WriteHeader(204)
}
```

Add to `session.go`:
```go
func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxPrincipal, p)
}
```

- [ ] **Step 7: Test harness**

`internal/api/apitest/apitest.go`:
```go
// Package apitest provides an HTTP test harness for the panel API.
package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type Harness struct {
	T      *testing.T
	Server *httptest.Server
	Store  *store.Store
	Box    *crypto.Box
	Deps   api.Deps
}

// Option lets later parts inject a NodeDriver or other deps into the harness.
type Option func(*api.Deps)

func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	st := store.OpenTest(t)
	key := bytes.Repeat([]byte{7}, 32)
	box, _ := crypto.NewBox(1, map[int][]byte{1: key})
	cfg := config.Config{PublicURL: "http://panel.test", DataDir: t.TempDir(), NodeDriver: "mock",
		MasterKey: key, MasterKeyVersion: 1, SessionSecret: key, TProxyCommit: config.DefaultTProxyCommit,
		ApplyInterval: 45, OfflineAfter: 90}
	deps := api.Deps{Store: st, Box: box, Signer: crypto.NewSigner(key), Log: slog.New(slog.DiscardHandler), Cfg: cfg}
	for _, o := range opts {
		o(&deps)
	}
	srv := httptest.NewServer(api.New(deps).Handler())
	t.Cleanup(srv.Close)
	return &Harness{T: t, Server: srv, Store: st, Box: box, Deps: deps}
}

func (h *Harness) CreateAdmin(username, password, role string) uuid.UUID {
	h.T.Helper()
	hash, err := crypto.HashPassword(password)
	if err != nil {
		h.T.Fatal(err)
	}
	u, err := h.Store.Q.CreateAdmin(context.Background(), db.CreateAdminParams{Username: username, PasswordHash: hash, Role: db.AdminRole(role)})
	if err != nil {
		h.T.Fatal(err)
	}
	return u.ID
}

type Client struct {
	h        *Harness
	http     *http.Client
	SendCSRF bool
}

func (h *Harness) Anonymous() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{h: h, http: &http.Client{Jar: jar}, SendCSRF: true}
}

func (h *Harness) Login(username, password string) *Client {
	h.T.Helper()
	c := h.Anonymous()
	resp := c.Post("/api/v1/auth/login", map[string]string{"username": username, "password": password})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		h.T.Fatalf("login failed: %d %s", resp.StatusCode, b)
	}
	return c
}

func (c *Client) do(method, path string, body any) *http.Response {
	c.h.T.Helper()
	var rd io.Reader
	if body != nil {
		if raw, ok := body.([]byte); ok {
			rd = bytes.NewReader(raw)
		} else {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
	}
	req, _ := http.NewRequest(method, c.h.Server.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if c.SendCSRF {
		u, _ := req.URL.Parse("/")
		for _, ck := range c.http.Jar.Cookies(u) {
			if ck.Name == "tgwp_csrf" {
				req.Header.Set("X-CSRF-Token", ck.Value)
			}
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.h.T.Fatal(err)
	}
	return resp
}

func (c *Client) Get(p string) *http.Response              { return c.do(http.MethodGet, p, nil) }
func (c *Client) Post(p string, b any) *http.Response      { return c.do(http.MethodPost, p, b) }
func (c *Client) Patch(p string, b any) *http.Response     { return c.do(http.MethodPatch, p, b) }
func (c *Client) Put(p string, b any) *http.Response       { return c.do(http.MethodPut, p, b) }
func (c *Client) Delete(p string) *http.Response           { return c.do(http.MethodDelete, p, nil) }

func (c *Client) JSON(resp *http.Response, out any) {
	c.h.T.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, out); err != nil {
		c.h.T.Fatalf("decode %s: %v", b, err)
	}
}
```

- [ ] **Step 8: Panel main: serve + admin create**

Replace `cmd/panel/main.go`:
```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/logging"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logging.New(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := store.Migrate(ctx, st.Pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "migrate":
		fmt.Println("migrations applied")
		return nil
	case "admin":
		return adminCmd(ctx, st, args[1:])
	case "serve":
		return serve(ctx, cfg, st, log)
	}
	return fmt.Errorf("unknown command %q (serve|migrate|admin create)", cmd)
}

func adminCmd(ctx context.Context, st *store.Store, args []string) error {
	if len(args) < 3 || args[0] != "create" {
		return errors.New("usage: panel admin create <username> <password> [owner|admin|viewer]")
	}
	role := "owner"
	if len(args) > 3 {
		role = args[3]
	}
	hash, err := crypto.HashPassword(args[2])
	if err != nil {
		return err
	}
	u, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: args[1], PasswordHash: hash, Role: db.AdminRole(role)})
	if err != nil {
		return err
	}
	fmt.Println("created admin", u.Username, "role", u.Role)
	return nil
}

func serve(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) error {
	keys := map[int][]byte{cfg.MasterKeyVersion: cfg.MasterKey}
	for v, k := range cfg.OldMasterKeys {
		keys[v] = k
	}
	box, err := crypto.NewBox(cfg.MasterKeyVersion, keys)
	if err != nil {
		return err
	}
	srv := api.New(api.Deps{Store: st, Box: box, Signer: crypto.NewSigner(cfg.SessionSecret), Log: log, Cfg: cfg})
	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	log.Info("panel listening", "addr", cfg.HTTPAddr)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```
(Add `"log/slog"` import.)

- [ ] **Step 9: Run all tests, lint, build**

Run: `go mod tidy && go build ./... && go test ./... && golangci-lint run ./...`
Expected: all PASS, lint clean. If `TestLoginRateLimit` is flaky due to `RealIP` middleware picking a different IP, ensure the harness sends no `X-Forwarded-For` and the limiter keys on `127.0.0.1`.

- [ ] **Step 10: Manual smoke**

```bash
set -a; source .env.example; set +a
export MASTER_KEY=$(openssl rand -base64 32) SESSION_SECRET=$(openssl rand -base64 32)
go run ./cmd/panel admin create root 'change-me-now-1'
go run ./cmd/panel serve &
curl -s -c c.txt -H 'Content-Type: application/json' -d '{"username":"root","password":"change-me-now-1"}' localhost:8080/api/v1/auth/login
curl -s -b c.txt localhost:8080/api/v1/auth/me
kill %1
```
Expected: login returns the user JSON; `me` returns the same.
