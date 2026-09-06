package api_test

import (
	"context"
	"testing"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/store/db"
)

// TestForwardedForCannotSpoofTheLoginLimiterOrAuditIP is the end-to-end half of
// the X-Forwarded-For hardening (final review I1). Caddy is configured to
// overwrite the header with the peer address, and the panel additionally reads
// only the *last* entry - the one a proxy appended - so a client that writes its
// own X-Forwarded-For gets no say in which bucket its attempts land in.
//
// The httptest client really is 127.0.0.1, so a header naming 1.2.3.4 must never
// be the key: if it were, every assertion below would flip.
func TestForwardedForCannotSpoofTheLoginLimiterOrAuditIP(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")

	wrong := map[string]string{"username": "root", "password": "nope-nope-nope"}

	// Ten attempts from "1.2.3.4, 10.0.0.9" exhaust the per-IP budget.
	spoofer := h.Anonymous().SetHeader("X-Forwarded-For", "1.2.3.4, 10.0.0.9")
	for i := 0; i < 10; i++ {
		if resp := spoofer.Post("/api/v1/auth/login", wrong); resp.StatusCode != 401 {
			t.Fatalf("attempt %d: %d, want 401", i, resp.StatusCode)
		}
	}
	if resp := spoofer.Post("/api/v1/auth/login", wrong); resp.StatusCode != 429 {
		t.Fatalf("11th attempt: %d, want 429", resp.StatusCode)
	}

	// Same forged head, different proxy-appended entry: a separate bucket, so the
	// key cannot be the value the caller chose.
	fresh := h.Anonymous().SetHeader("X-Forwarded-For", "1.2.3.4, 10.0.0.10")
	switch code := fresh.Post("/api/v1/auth/login", wrong).StatusCode; code {
	case 401: // as expected: an untouched bucket
	case 429:
		t.Fatal("the forged first entry was used as the limiter key")
	default:
		t.Fatalf("different last entry: %d, want 401", code)
	}

	// Different forged head, same proxy-appended entry: still the exhausted bucket.
	rotated := h.Anonymous().SetHeader("X-Forwarded-For", "9.9.9.9, 10.0.0.9")
	if resp := rotated.Post("/api/v1/auth/login", wrong); resp.StatusCode != 429 {
		t.Fatalf("rotating the forged first entry escaped the block: %d, want 429", resp.StatusCode)
	}

	// The audit row records the same address the limiter keyed off.
	auditor := h.Anonymous().SetHeader("X-Forwarded-For", "1.2.3.4, 10.0.0.77")
	if resp := auditor.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "pass-123456"}); resp.StatusCode != 200 {
		t.Fatalf("login: %d, want 200", resp.StatusCode)
	}
	rows, err := h.Store.Q.ListAudit(context.Background(), db.ListAuditParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Action != "auth.login" {
			continue
		}
		found = true
		if row.Ip != "10.0.0.77" {
			t.Fatalf("audit ip = %q, want the proxy-appended 10.0.0.77", row.Ip)
		}
	}
	if !found {
		t.Fatal("no auth.login audit row")
	}
}
