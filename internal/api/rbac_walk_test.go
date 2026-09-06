package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

// allowedForViewer lists the mutating routes a viewer may legitimately call. All of
// them are self-service on the caller's own account: ending your own session,
// changing your own password, and enrolling or removing your own second factor.
// Hardening your own login is not a privileged action, so gating it by role would
// only leave the least-trusted accounts as the weakest way into the panel.
var allowedForViewer = map[string]bool{
	"POST /api/v1/auth/logout":       true,
	"POST /api/v1/me/password":       true,
	"POST /api/v1/auth/totp/setup":   true,
	"POST /api/v1/auth/totp/confirm": true,
	"POST /api/v1/auth/totp/disable": true,
}

// publicMutations lists the mutating routes reachable without a session: the login
// form itself; the TOTP verify step, which is the second half of login (the caller
// has no cookie yet, so requiring one would make it unreachable - it is
// authenticated instead by the HMAC-signed, five-minute challenge /auth/login hands
// out only after the password checks out, and rate-limited by the same per-IP login
// limiter); and the one-shot node registration callback, which carries its own
// single-use install token instead of a cookie.
var publicMutations = map[string]bool{
	"POST /api/v1/auth/login":               true,
	"POST /api/v1/auth/totp/verify":         true,
	"POST /api/v1/install/{token}/register": true,
}

// publicReads lists the /api/v1 read routes that are deliberately reachable without a
// session: the branding the login screen renders before anyone has logged in, and the two
// install endpoints a brand-new node fetches with its one-shot token instead of a cookie.
// Everything else must 401 for an anonymous caller.
var publicReads = map[string]bool{
	"GET /api/v1/branding":                    true,
	"GET /api/v1/branding/assets/{id}/{file}": true,
	"GET /api/v1/install/{token}.sh":          true,
	"GET /api/v1/install/agent/{platform}":    true,
	// The public status the login screen shows before anyone signs in: the
	// panel version, how many nodes exist and how many are online, and the
	// pinned relay commit. No hostname, node name or IP is in the response
	// (TestPublicStatusHasNoHostnames pins that), so anonymous access buys a
	// stranger nothing they could not learn from the install script, and the
	// handler memoises for 10s so it cannot be used as a query amplifier.
	"GET /api/v1/status/public": true,
	// The subscription page and its .json twin: the caller is a phone or a
	// Telegram client following a link a key holder was handed, not a logged-in
	// operator, so there is no session to require. What they can see is
	// rate-limited (60/min/IP, see subLimiter) and deliberately thin - node
	// names, hostnames, links and a QR code, never the key's label, owner
	// label or note - so anonymous access here is a deliberate design point,
	// not a gap.
	"GET /s/{token}":      true,
	"GET /s/{token}.json": true,
}

// TestEveryMutatingRouteIsProtected walks the real chi route table and asserts
// that every mutating /api/v1 route sits behind requireAuth + csrfCheck +
// RequireRole. It is a structural test: a route added without the middleware
// group fails here even if nobody writes a handler test for it.
func TestEveryMutatingRouteIsProtected(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	anon := h.Anonymous()

	repl := strings.NewReplacer(
		"{id}", uuid.New().String(),
		"{node}", uuid.New().String(),
		"{token}", "x",
		"{file}", "x",
		"{platform}", "linux-amd64",
	)

	var walked int
	err := chi.Walk(h.Router(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return nil
		}
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		key := method + " " + route
		if publicMutations[key] {
			return nil
		}
		walked++
		path := repl.Replace(route)

		if resp := anon.Do(method, path, map[string]any{}); statusOf(t, resp) != http.StatusUnauthorized {
			t.Errorf("%s: anonymous got %d, want 401", key, resp.StatusCode)
		}

		viewer.SendCSRF = false
		resp := viewer.Do(method, path, map[string]any{})
		code, status := errCodeOf(t, resp)
		if status != http.StatusForbidden || code != "csrf" {
			t.Errorf("%s: no-csrf got %d/%q, want 403/csrf", key, status, code)
		}
		viewer.SendCSRF = true

		if !allowedForViewer[key] {
			if resp := viewer.Do(method, path, map[string]any{}); statusOf(t, resp) != http.StatusForbidden {
				t.Errorf("%s: viewer got %d, want 403", key, resp.StatusCode)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// Guard against the walk silently covering nothing (e.g. a router change
	// that stops exposing routes) — the panel has dozens of mutating routes.
	if walked < 20 {
		t.Fatalf("walked only %d mutating routes, expected the full route table", walked)
	}
}

func statusOf(t *testing.T, resp *http.Response) int {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func errCodeOf(t *testing.T, resp *http.Response) (string, int) {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	b, _ := io.ReadAll(resp.Body)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &body)
	return body.Error.Code, resp.StatusCode
}

// TestEveryReadRouteIsProtected is the read-side half of the walk. The mutating pass skips
// GET/HEAD entirely, so the one test whose job is to catch a route mounted outside
// mountProtected would not catch a `r.Get` mounted there - and Phase 3's read endpoints
// (subscription page, backups, a Grafana proxy) are exactly that shape. Anonymous access to
// anything outside publicReads must be a 401.
func TestEveryReadRouteIsProtected(t *testing.T) {
	h := apitest.New(t)
	anon := h.Anonymous()

	repl := strings.NewReplacer(
		"{id}", uuid.New().String(),
		"{node}", uuid.New().String(),
		"{token}", "x",
		"{file}", "x",
		"{platform}", "linux-amd64",
	)

	var walked int
	err := chi.Walk(h.Router(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet && method != http.MethodHead {
			return nil
		}
		// /s/ is the one non-/api/v1 route group with real access control to check
		// (the subscription page, deliberately public - see publicReads above);
		// /healthz, /metrics and the SPA catch-all are excluded because they carry
		// no per-resource authorization to walk.
		if !strings.HasPrefix(route, "/api/v1/") && !strings.HasPrefix(route, "/s/") {
			return nil
		}
		key := method + " " + route
		if publicReads[key] {
			// Prove the exemption is real rather than a stale entry: a public route must
			// not 401, or the list is hiding a genuine regression.
			if resp := anon.Do(method, repl.Replace(route), nil); statusOf(t, resp) == http.StatusUnauthorized {
				t.Errorf("%s: listed as public but returned 401", key)
			}
			return nil
		}
		walked++
		if resp := anon.Do(method, repl.Replace(route), nil); statusOf(t, resp) != http.StatusUnauthorized {
			t.Errorf("%s: anonymous got %d, want 401", key, resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if walked < 15 {
		t.Fatalf("walked only %d read routes, expected the full route table", walked)
	}
}
