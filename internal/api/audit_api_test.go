package api_test

import (
	"strconv"
	"testing"
	"time"

	"tgwebproxy/internal/api/apitest"
)

func TestAuditFilters(t *testing.T) {
	_, c, n := ownerWithNode(t)
	// ownerWithNode already produced auth.login + node.create audit entries.
	_ = c.Post("/api/v1/keys", map[string]any{"label": "k", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})

	var byAction struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=key."), &byAction)
	if byAction.Total != 1 {
		t.Fatalf("action=key. total %d, want 1", byAction.Total)
	}

	var byUser struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?user=root"), &byUser)
	if byUser.Total < 3 {
		t.Fatalf("user=root total %d, want >= 3", byUser.Total)
	}

	var noMatch struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=nope."), &noMatch)
	if noMatch.Total != 0 {
		t.Fatalf("action=nope. total %d, want 0", noMatch.Total)
	}

	var future struct {
		Total int `json:"total"`
	}
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	c.JSON(c.Get("/api/v1/audit?from="+tomorrow), &future)
	if future.Total != 0 {
		t.Fatalf("from=tomorrow total %d, want 0", future.Total)
	}
}

func TestAuditActionFilterEscapesLikeWildcards(t *testing.T) {
	h, c, n := ownerWithNode(t)
	_ = c.Post("/api/v1/keys", map[string]any{"label": "k", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})

	var noMatch struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=key.%25"), &noMatch)
	if noMatch.Total != 0 {
		t.Fatalf("action=key.%%25 (literal 'key.%%') total %d, want 0 - %% must not act as a wildcard", noMatch.Total)
	}

	var prefixMatch struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=key."), &prefixMatch)
	if prefixMatch.Total == 0 {
		t.Fatalf("action=key. total 0, want >0")
	}

	var underscoreMatch struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=key_create"), &underscoreMatch)
	if underscoreMatch.Total != 0 {
		t.Fatalf("action=key_create total %d, want 0 - _ must not act as a wildcard", underscoreMatch.Total)
	}

	// Now insert a row whose action contains a literal '%' and confirm it is found.
	_, err := h.Store.Pool.Exec(t.Context(),
		`INSERT INTO audit_log (action, target_type, target_id, meta, ip, created_at)
		 VALUES ('key.%weird', 'test', 'x', '{}'::jsonb, '', now())`)
	if err != nil {
		t.Fatal(err)
	}
	var literalMatch struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=key.%25"), &literalMatch)
	if literalMatch.Total != 1 {
		t.Fatalf("action=key.%%25 after inserting key.%%weird: total %d, want 1", literalMatch.Total)
	}
}

func TestAuditPaginationIsStableAcrossTies(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	_, err := h.Store.Pool.Exec(t.Context(),
		`INSERT INTO audit_log (action, target_type, target_id, meta, ip, created_at)
		 SELECT 'tie.entry', 'test', i::text, '{}'::jsonb, '', now()
		 FROM generate_series(1, 6) i`)
	if err != nil {
		t.Fatal(err)
	}

	type page struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	seen := map[int64]int{}
	var total int
	for p := 1; p <= 3; p++ {
		var got page
		c.JSON(c.Get("/api/v1/audit?action=tie.&per_page=2&page="+strconv.Itoa(p)), &got)
		total = got.Total
		for _, it := range got.Items {
			seen[it.ID]++
		}
	}
	if total != 6 {
		t.Fatalf("total = %d, want 6", total)
	}
	if len(seen) != 6 {
		t.Fatalf("paging over tied timestamps returned %d distinct rows, want 6: %v", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %d appeared on %d pages", id, n)
		}
	}
}

func TestAuditHugePageIsClamped(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	for _, p := range []string{"2147483647", "99999999999", "12345678"} {
		resp := c.Get("/api/v1/audit?page=" + p)
		if resp.StatusCode != 200 {
			t.Fatalf("page=%s got %d, want 200", p, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// A rejected sign-in is the one thing worth seeing in the audit trail that no successful
// action produces, so it must be filed for a wrong password and for a username that matches
// no account at all.
func TestFailedLoginsAreAudited(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")

	anon := h.Anonymous()
	if resp := anon.Post("/api/v1/auth/login", map[string]string{"username": "root", "password": "wrong-one"}); resp.StatusCode != 401 {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp := anon.Post("/api/v1/auth/login", map[string]string{"username": "ghost", "password": "wrong-one"}); resp.StatusCode != 401 {
		t.Fatalf("unknown user: %d", resp.StatusCode)
	}

	c := h.Login("root", "pass-123456")
	var got struct {
		Items []struct {
			Action string         `json:"action"`
			Meta   map[string]any `json:"meta"`
		} `json:"items"`
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=auth.login_failed"), &got)
	if got.Total != 2 {
		t.Fatalf("auth.login_failed total %d, want 2", got.Total)
	}
	reasons := map[string]bool{}
	for _, it := range got.Items {
		reasons[it.Meta["reason"].(string)] = true
	}
	if !reasons["bad_password"] || !reasons["unknown_user"] {
		t.Fatalf("reasons %v, want both bad_password and unknown_user", reasons)
	}
}
