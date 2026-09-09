package api_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

func TestEveryResponseRefusesToBeFramed(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	for _, path := range []string{"/", "/healthz", "/api/v1/auth/me", "/api/v1/nodes", "/s/nope"} {
		resp := c.Get(path)
		resp.Body.Close() //nolint:errcheck
		if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q, want DENY", path, got)
		}
	}
}

func TestEveryResponseCarriesABaselineCSP(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	for _, path := range []string{"/", "/healthz", "/api/v1/auth/me", "/api/v1/nodes"} {
		resp := c.Get(path)
		resp.Body.Close() //nolint:errcheck
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: CSP = %q, want the SPA baseline", path, csp)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", path, got)
		}
	}
}

func TestSitePreviewIsFramableBySameOriginOnly(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var tpl struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON(c.Post("/api/v1/site-templates", map[string]any{"name": "framable", "html": goodHTML}), &tpl)
	if resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/site", map[string]any{"template_id": tpl.ID}); resp.StatusCode != 200 {
		t.Fatalf("assign site: %d", resp.StatusCode)
	}

	resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/site/preview")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		t.Fatalf("preview: %d %s", resp.StatusCode, body)
	}
	if len(body) == 0 {
		t.Fatal("preview returned an empty body")
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("preview: X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("preview CSP %q is missing frame-ancestors 'self'", csp)
	}
	for _, want := range []string{"default-src 'none'", "style-src 'unsafe-inline'", "img-src data:"} {
		if !strings.Contains(csp, want) {
			t.Errorf("preview CSP %q lost %q", csp, want)
		}
	}

	// And the exemption really is one route: its sibling still refuses framing.
	sibling := c.Get("/api/v1/nodes/" + n.ID.String() + "/site")
	sibling.Body.Close() //nolint:errcheck
	if got := sibling.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("GET /nodes/{id}/site: X-Frame-Options = %q, want DENY", got)
	}
	list := c.Get("/api/v1/nodes")
	list.Body.Close() //nolint:errcheck
	if got := list.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("GET /nodes: X-Frame-Options = %q, want DENY", got)
	}
}

func TestSubscriptionPageCSPForbidsFraming(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	resp := c.Get("/s/nope")
	resp.Body.Close() //nolint:errcheck
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("CSP %q is missing frame-ancestors 'none'", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP %q lost its default-src", csp)
	}
}

func TestViewerRedactionFailsClosedOnAnUnparsableReport(t *testing.T) {
	h, _, n := ownerWithNode(t)
	ctx := context.Background()

	const junk = `{"results": "resolved 10.1.2.3, TLS handshake failed for internal.example"}`
	if _, err := h.Store.Pool.Exec(ctx, `UPDATE nodes SET last_check = $1 WHERE id = $2`, junk, n.ID); err != nil {
		t.Fatal(err)
	}

	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	resp := viewer.Get("/api/v1/nodes/" + n.ID.String())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer GET node: %d", resp.StatusCode)
	}
	var got map[string]any
	viewer.JSON(resp, &got)
	if v, ok := got["last_check"]; ok && v != nil {
		t.Fatalf("last_check = %v, want null: an unparsable report must not reach a viewer", v)
	}

	// A writer still gets the raw value.
	owner := h.Login("root", "pass-123456")
	var ownerGot map[string]any
	owner.JSON(owner.Get("/api/v1/nodes/"+n.ID.String()), &ownerGot)
	if ownerGot["last_check"] == nil {
		t.Fatal("the writer lost the stored report too")
	}
}
