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

// allowedForViewer lists the mutating routes a viewer may legitimately call.
var allowedForViewer = map[string]bool{
	"POST /api/v1/auth/logout":       true,
	"POST /api/v1/me/password":       true,
	"POST /api/v1/auth/totp/setup":   true,
	"POST /api/v1/auth/totp/confirm": true,
	"POST /api/v1/auth/totp/disable": true,
}

var publicMutations = map[string]bool{
	"POST /api/v1/auth/login":               true,
	"POST /api/v1/auth/totp/verify":         true,
	"POST /api/v1/install/{token}/register": true,
}

var publicReads = map[string]bool{
	"GET /api/v1/branding":                    true,
	"GET /api/v1/branding/assets/{id}/{file}": true,
	"GET /api/v1/install/{token}.sh":          true,
	"GET /api/v1/install/agent/{platform}":    true,
	"GET /api/v1/status/public":               true,
	"GET /s/{token}":                          true,
	"GET /s/{token}.json":                     true,
}

var nodeTokenReads = map[string]bool{
	"GET /api/v1/node/upgrade": true,
}

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

// TestEveryReadRouteIsProtected is the read-side half of the walk.
func TestEveryReadRouteIsProtected(t *testing.T) {
	h := apitest.New(t)
	anon := h.Anonymous()
	h.CreateAdmin("owner", "pass-123456", "owner")
	owner := h.Login("owner", "pass-123456")
	seenNodeToken := map[string]bool{}

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
		if !strings.HasPrefix(route, "/api/v1/") && !strings.HasPrefix(route, "/s/") {
			return nil
		}
		key := method + " " + route
		if publicReads[key] {
			if resp := anon.Do(method, repl.Replace(route), nil); statusOf(t, resp) == http.StatusUnauthorized {
				t.Errorf("%s: listed as public but returned 401", key)
			}
			return nil
		}
		walked++
		if resp := anon.Do(method, repl.Replace(route), nil); statusOf(t, resp) != http.StatusUnauthorized {
			t.Errorf("%s: anonymous got %d, want 401", key, resp.StatusCode)
		}
		if nodeTokenReads[key] {
			seenNodeToken[key] = true
			if resp := owner.Do(method, repl.Replace(route), nil); statusOf(t, resp) != http.StatusUnauthorized {
				t.Errorf("%s: owner session got %d, want 401 (node-token route)", key, resp.StatusCode)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for key := range nodeTokenReads {
		if !seenNodeToken[key] {
			t.Errorf("nodeTokenReads lists %q, but the route table has no such route", key)
		}
	}
	if walked < 15 {
		t.Fatalf("walked only %d read routes, expected the full route table", walked)
	}
}
