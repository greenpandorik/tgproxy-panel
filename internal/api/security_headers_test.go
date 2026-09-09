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

// TestEveryResponseRefusesToBeFramed covers final-review M6. The panel is a
// session-authenticated admin UI with destructive buttons on it and the public
// subscription page is a page of connection details; neither has any reason to
// be embedded in someone else's document, and clickjacking is the cheapest
// attack to close.
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

// TestEveryResponseCarriesABaselineCSP covers the SPA and every plain API route: none of
// them set their own Content-Security-Policy, so without the global default they would ship
// none at all. Routes that need something different (branding assets, site previews, the
// subscription page) are covered separately below and must still show their own, narrower
// policy rather than this baseline.
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

// TestSitePreviewIsFramableBySameOriginOnly is the one exemption from the rule
// above, and it exists because the node Site tab renders the preview in a
// same-origin sandboxed <iframe> (web/src/pages/nodes/NodeSiteTab.tsx): a blanket
// DENY blanks it, since DENY refuses same-origin framing too. The narrowing is
// SAMEORIGIN plus frame-ancestors 'self', which still refuses every other site.
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
	// The regression this guards: a blank Site tab, with a rendered preview body
	// that the browser refuses to display.
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
	// The rest of the lockdown is untouched: the preview still loads nothing of
	// its own, so the exemption is about who may frame it and nothing else.
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

// The subscription page carries the modern spelling of the same rule in its own
// CSP, where the rest of its lockdown already lives.
func TestSubscriptionPageCSPForbidsFraming(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	// The 404 page goes through the same header helper as a real page, so an
	// unknown token is enough to assert the policy.
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

// TestViewerRedactionFailsClosedOnAnUnparsableReport covers final-review M5. The
// panel is the only writer of nodes.last_check today, so this is defensive - but
// a redaction path that passes a value through when it cannot understand it
// leaks in precisely the one case it did not anticipate.
func TestViewerRedactionFailsClosedOnAnUnparsableReport(t *testing.T) {
	h, _, n := ownerWithNode(t)
	ctx := context.Background()

	// Valid JSON (the column is jsonb) but not a Report: the detail text is
	// there in the raw bytes, and the old code returned those bytes verbatim.
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

	// A writer still gets the raw value - redaction is the only thing that has
	// to fail closed, and hiding it from the operator would hide the corruption.
	owner := h.Login("root", "pass-123456")
	var ownerGot map[string]any
	owner.JSON(owner.Get("/api/v1/nodes/"+n.ID.String()), &ownerGot)
	if ownerGot["last_check"] == nil {
		t.Fatal("the writer lost the stored report too")
	}
}
