package api_test

import (
	"strconv"
	"testing"
	"time"
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

// TestAuditActionFilterEscapesLikeWildcards covers Task 33 item 2: the action
// filter is matched with a LIKE prefix, so an unescaped %/_ in the query string
// used to act as a SQL wildcard rather than a literal character. "key.%" must
// only match rows whose action literally starts with "key.%" (none of the
// ordinary key.* actions, since none contains a literal percent), and once a
// row with a literal "%" in its action exists, it must match.
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

	// Sanity: the same prefix without the literal %, still matched as a normal
	// prefix, does find the key.create entry.
	var prefixMatch struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/audit?action=key."), &prefixMatch)
	if prefixMatch.Total == 0 {
		t.Fatalf("action=key. total 0, want >0")
	}

	// A literal underscore must not act as a single-char wildcard either:
	// "key_create" (underscore, not dot) should not match any key.* action.
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

// TestAuditPaginationIsStableAcrossTies covers the missing ORDER BY tiebreaker: audit_log.id
// is a bigserial and entries written in one transaction share a created_at, so paging over a
// tie could return a row twice or skip it entirely.
func TestAuditPaginationIsStableAcrossTies(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	// Six entries sharing one timestamp to the microsecond - the exact tie the tiebreaker
	// exists for.
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

// TestAuditHugePageIsClamped covers the int32 offset overflow: (page-1)*per used to wrap
// negative for a page above ~10.7M, producing a Postgres error the handler served as a 500.
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
