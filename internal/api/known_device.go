package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

const (
	// deviceCookie marks a browser that has signed in to an account before. While the account
	// is locked, only such a browser has its password checked.
	deviceCookie = "tgwp_device"
	deviceTTL    = 90 * 24 * time.Hour
	devicePrefix = "device:"
	deviceScope  = "/api/v1/auth"
)

// credentialGeneration changes whenever the account's password or second factor changes, which
// retires every device cookie issued before.
func credentialGeneration(u db.AdminUser) string {
	h := sha256.New()
	h.Write([]byte(u.PasswordHash))
	h.Write([]byte{0})
	h.Write(u.TotpSecretEnc)
	if u.TotpEnabled {
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// deviceToken is "device:<admin id>|<unix expiry>.<mac>". The MAC also covers the credential
// generation, which stays on the server.
func (s *Server) deviceToken(u db.AdminUser, now time.Time) string {
	payload := devicePrefix + u.ID.String() + "|" + strconv.FormatInt(now.Add(deviceTTL).Unix(), 10)
	signed := s.signer.Sign(payload + "|" + credentialGeneration(u))
	return payload + signed[strings.LastIndexByte(signed, '.'):]
}

func (s *Server) setKnownDevice(w http.ResponseWriter, u db.AdminUser) {
	http.SetCookie(w, &http.Cookie{
		Name: deviceCookie, Value: s.deviceToken(u, time.Now()), Path: deviceScope, HttpOnly: true,
		Secure: s.secureCookies, SameSite: http.SameSiteStrictMode, MaxAge: int(deviceTTL.Seconds()),
	})
}

// refreshKnownDevice reissues the device cookie after the account's credentials changed.
func (s *Server) refreshKnownDevice(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if u, err := s.store.Q.GetAdmin(r.Context(), id); err == nil {
		s.setKnownDevice(w, u)
	}
}

// knownDevice reports whether r carries a current device cookie for u.
func (s *Server) knownDevice(r *http.Request, u db.AdminUser, now time.Time) bool {
	c, err := r.Cookie(deviceCookie)
	if err != nil {
		return false
	}
	i := strings.LastIndexByte(c.Value, '.')
	if i <= 0 {
		return false
	}
	payload, mac := c.Value[:i], c.Value[i:]
	if _, ok := s.signer.Verify(payload + "|" + credentialGeneration(u) + mac); !ok {
		return false
	}
	rest, ok := strings.CutPrefix(payload, devicePrefix)
	if !ok {
		return false
	}
	idStr, expStr, ok := strings.Cut(rest, "|")
	if !ok {
		return false
	}
	id, err := uuid.Parse(idStr)
	if err != nil || id != u.ID {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	return err == nil && now.Before(time.Unix(exp, 0))
}
