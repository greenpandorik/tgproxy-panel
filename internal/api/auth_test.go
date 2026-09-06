package api_test

import (
	"net/http"
	"strings"
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

func TestMethodNotAllowedEnvelope(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	resp := c.Delete("/api/v1/auth/me")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	c.JSON(resp, &body)
	if body.Error.Code != "method_not_allowed" {
		t.Fatalf("expected error.code=method_not_allowed, got %+v", body)
	}
}

// TestCannotDeleteLastOwner: without this guard an installation can end up with zero owners,
// which permanently locks it out of /admins and PUT /settings.
func TestCannotDeleteLastOwner(t *testing.T) {
	h := apitest.New(t)
	rootID := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	// Only owner: cannot be deleted, and the message says why.
	resp := c.Delete("/api/v1/admins/" + rootID.String())
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("deleting the last owner: expected 409, got %d", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	c.JSON(resp, &body)
	if !strings.Contains(body.Error.Message, "last owner") {
		t.Errorf("message %q should name the last-owner rule", body.Error.Message)
	}

	// With a second owner, deleting one is allowed again.
	secondID := h.CreateAdmin("second", "pass-123456", "owner")
	if resp := c.Delete("/api/v1/admins/" + secondID.String()); resp.StatusCode != 204 {
		t.Fatalf("deleting one of two owners: %d", resp.StatusCode)
	}
	// Non-owners are unaffected by the guard.
	helperID := h.CreateAdmin("helper", "pass-123456", "admin")
	if resp := c.Delete("/api/v1/admins/" + helperID.String()); resp.StatusCode != 204 {
		t.Fatalf("deleting a non-owner: %d", resp.StatusCode)
	}
	// Self-deletion is still refused while another owner exists.
	h.CreateAdmin("third", "pass-123456", "owner")
	if resp := c.Delete("/api/v1/admins/" + rootID.String()); resp.StatusCode != http.StatusConflict {
		t.Fatalf("self-delete: expected 409, got %d", resp.StatusCode)
	}
}
