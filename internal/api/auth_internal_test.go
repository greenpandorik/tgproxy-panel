package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

func TestPasswordUnavailableAnswers503(t *testing.T) {
	for _, err := range []error{crypto.ErrBusy, fmt.Errorf("wrapped: %w", crypto.ErrBusy), context.Canceled} {
		rec := httptest.NewRecorder()
		if !passwordUnavailable(rec, err) {
			t.Fatalf("%v: not treated as unavailable", err)
		}
		if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%v: %d, Retry-After %q; want 503 with Retry-After", err, rec.Code, rec.Header().Get("Retry-After"))
		}
	}
	for _, err := range []error{nil, errors.New("unsupported hash format")} {
		rec := httptest.NewRecorder()
		if passwordUnavailable(rec, err) {
			t.Fatalf("%v: treated as unavailable", err)
		}
	}
}

func TestKnownDeviceCookie(t *testing.T) {
	s := &Server{signer: crypto.NewSigner(bytes.Repeat([]byte{9}, 32))}
	u := db.AdminUser{ID: uuid.New(), PasswordHash: "$argon2id$v=19$m=65536,t=3,p=2$salt$hash"}
	now := time.Now()
	token := s.deviceToken(u, now)
	with := func(value string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		r.AddCookie(&http.Cookie{Name: deviceCookie, Value: value})
		return r
	}
	if !s.knownDevice(with(token), u, now) {
		t.Fatal("a fresh device cookie is not accepted")
	}
	if s.knownDevice(httptest.NewRequest(http.MethodPost, "/", nil), u, now) {
		t.Fatal("a request without the cookie is a known device")
	}
	if s.knownDevice(with(token), u, now.Add(deviceTTL+time.Second)) {
		t.Fatal("an expired device cookie is accepted")
	}
	changed := u
	changed.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$salt$other"
	if s.knownDevice(with(token), changed, now) {
		t.Fatal("a device cookie survived a password change")
	}
	enrolled := u
	enrolled.TotpEnabled, enrolled.TotpSecretEnc = true, []byte("secret")
	if s.knownDevice(with(token), enrolled, now) {
		t.Fatal("a device cookie survived enabling two-factor authentication")
	}
	other := u
	other.ID = uuid.New()
	if s.knownDevice(with(token), other, now) {
		t.Fatal("a device cookie works for another account")
	}
	payload, mac, _ := strings.Cut(token, ".")
	extended := strings.Replace(payload, payload[strings.LastIndexByte(payload, '|')+1:], "9999999999", 1)
	for name, value := range map[string]string{
		"extended expiry": extended + "." + mac,
		"another signer":  (&Server{signer: crypto.NewSigner(bytes.Repeat([]byte{1}, 32))}).deviceToken(u, now),
		"no mac":          payload,
		"session-shaped":  s.signer.Sign("device:" + u.ID.String() + "|9999999999"),
	} {
		if s.knownDevice(with(value), u, now) {
			t.Fatalf("%s: accepted", name)
		}
	}
}
