package api_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestLoginRateLimitCountsIPv6ByPrefix(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	wrong := map[string]string{"username": "root", "password": "bad"}
	var last int
	for i := 0; i < 11; i++ {
		c := h.Anonymous().SetHeader("X-Forwarded-For", fmt.Sprintf("2001:db8:1:2::%x", i+1))
		last = c.Post("/api/v1/auth/login", wrong).StatusCode
	}
	if last != 429 {
		t.Fatalf("11th attempt from a fresh address in the same /64: %d, want 429", last)
	}
	other := h.Anonymous().SetHeader("X-Forwarded-For", "2001:db8:1:3::1")
	if code := other.Post("/api/v1/auth/login", wrong).StatusCode; code != 401 {
		t.Fatalf("another /64: %d, want 401", code)
	}
}

func TestLockoutDoesNotRelockAfterExpiry(t *testing.T) {
	h := apitest.New(t)
	id := h.CreateAdmin("root", "pass-123456", "owner")
	ctx := t.Context()
	for i := 0; i < 20; i++ {
		if err := h.Store.Q.RecordFailedLogin(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	u, err := h.Store.Q.GetAdmin(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.LockedUntil == nil || !u.LockedUntil.After(time.Now()) {
		t.Fatalf("20 failures should lock the account, locked_until = %v", u.LockedUntil)
	}
	if resp := h.Anonymous().Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}); resp.StatusCode != 401 {
		t.Fatalf("login while locked: expected 401, got %d", resp.StatusCode)
	}

	if _, err := h.Store.Pool.Exec(ctx, `UPDATE admin_users SET locked_until = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Q.RecordFailedLogin(ctx, id); err != nil {
		t.Fatal(err)
	}
	u, err = h.Store.Q.GetAdmin(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.FailedLogins != 1 || u.LockedUntil != nil {
		t.Fatalf("first failure after the lockout: failed_logins = %d, locked_until = %v; want 1, nil", u.FailedLogins, u.LockedUntil)
	}
	h.Login("root", "pass-123456")
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

type loginOutcome struct {
	status     int
	body       string
	retryAfter string
}

func outcomeOf(t *testing.T, resp *http.Response) loginOutcome {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return loginOutcome{resp.StatusCode, string(b), resp.Header.Get("Retry-After")}
}

func TestLockoutHoldsUnlessTheBrowserIsAKnownDevice(t *testing.T) {
	h := apitest.New(t)
	id := h.CreateAdmin("root", "pass-123456", "owner")
	login := func(c *apitest.Client, password string) loginOutcome {
		t.Helper()
		return outcomeOf(t, c.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": password}))
	}
	from := func(addr string) *apitest.Client { return h.Anonymous().SetHeader("X-Forwarded-For", addr) }

	stale := h.Login("root", "pass-123456")
	owner := h.Login("root", "pass-123456")
	if code := owner.Post("/api/v1/me/password", map[string]string{"current": "pass-123456", "new": "pass-654321"}).StatusCode; code != 204 {
		t.Fatalf("change password: %d", code)
	}

	wrong := login(from("198.51.100.1"), "wrong")
	if wrong.status != 401 {
		t.Fatalf("wrong password: %+v", wrong)
	}
	for i := range 19 {
		addr := fmt.Sprintf("198.51.100.%d", 1+(i+1)/10)
		if got := login(from(addr), "wrong"); got != wrong {
			t.Fatalf("failure %d from %s: %+v, want %+v", i+2, addr, got, wrong)
		}
	}
	u, err := h.Store.Q.GetAdmin(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.LockedUntil == nil || !u.LockedUntil.After(time.Now()) {
		t.Fatalf("20 failures should lock the account, locked_until = %v", u.LockedUntil)
	}

	forged := from("203.0.113.7").SetHeader("Cookie", "tgwp_device=device:"+id.String()+"|9999999999.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	for name, c := range map[string]*apitest.Client{
		"a fresh address":                        from("203.0.113.5"),
		"a forged device cookie":                 forged,
		"a device cookie from before the change": stale,
	} {
		if got := login(c, "pass-654321"); got != wrong {
			t.Fatalf("correct password from %s while locked: %+v, want the wrong-password answer %+v", name, got, wrong)
		}
		if got := login(c, "still-wrong"); got != wrong {
			t.Fatalf("wrong password from %s while locked: %+v, want %+v", name, got, wrong)
		}
	}

	if got := login(owner, "pass-654321"); got.status != 200 {
		t.Fatalf("owner from a known device while locked: %+v, want 200", got)
	}
	if code := owner.Get("/api/v1/auth/me").StatusCode; code != 200 {
		t.Fatalf("owner session: %d", code)
	}
	if got := login(from("203.0.113.6"), "pass-654321"); got != wrong {
		t.Fatalf("the owner's sign-in lifted the lock for everyone else: %+v", got)
	}
}
