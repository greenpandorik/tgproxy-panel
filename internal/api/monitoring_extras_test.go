package api_test

import (
	"testing"
	"time"
)

func TestMonitoringOverviewCarriesTheLastKnownCertificateExpiry(t *testing.T) {
	h, c, n := ownerWithNode(t)
	expires := time.Now().Add(61 * 24 * time.Hour).UTC().Truncate(time.Second)
	measured := `[{"key":"transport","checks":[{"key":"certificate_expiry","status":"ok","value":"61 days","detail":"the certificate is valid until ` + expires.Format(time.RFC3339) + `"}]}]`
	unreachable := `[{"key":"transport","checks":[{"key":"certificate_expiry","status":"not_available","value":null,"detail":"port 443 could not be reached"}]}]`
	for _, run := range []struct {
		ago    string
		checks string
	}{{"2 hours", measured}, {"1 hour", unreachable}} {
		if _, err := h.Store.Pool.Exec(t.Context(),
			`INSERT INTO node_diagnostics (node_id, started_at, finished_at, overall_status, trigger, checks)
			 VALUES ($1, now() - $2::interval, now() - $2::interval, 'ok', 'scheduled', $3::jsonb)`, n.ID, run.ago, run.checks); err != nil {
			t.Fatal(err)
		}
	}

	var out struct {
		Nodes []struct {
			CertExpiresAt *time.Time `json:"cert_expires_at"`
		} `json:"nodes"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview"), &out)
	if len(out.Nodes) != 1 || out.Nodes[0].CertExpiresAt == nil || !out.Nodes[0].CertExpiresAt.Equal(expires) {
		t.Fatalf("nodes %+v, want the certificate to expire at %s", out.Nodes, expires)
	}
}

func TestMonitoringOverviewWithoutACertificateReading(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	var out struct {
		Nodes []struct {
			CertExpiresAt *time.Time `json:"cert_expires_at"`
		} `json:"nodes"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview"), &out)
	if len(out.Nodes) != 1 || out.Nodes[0].CertExpiresAt != nil {
		t.Fatalf("nodes %+v", out.Nodes)
	}
}

func TestNodeSaysSinceWhenChangesWait(t *testing.T) {
	_, c, n := ownerWithNode(t)
	type dirtyResp struct {
		Dirty      bool       `json:"dirty"`
		DirtySince *time.Time `json:"dirty_since"`
	}
	var before dirtyResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &before)
	if before.Dirty || before.DirtySince != nil {
		t.Fatalf("fresh node %+v", before)
	}
	if resp := c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"public_ip": "203.0.113.7"}); resp.StatusCode != 200 {
		t.Fatalf("patch %d", resp.StatusCode)
	}
	var first dirtyResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &first)
	if !first.Dirty || first.DirtySince == nil {
		t.Fatalf("after a change %+v", first)
	}
	if resp := c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"public_ip": "203.0.113.8"}); resp.StatusCode != 200 {
		t.Fatalf("patch %d", resp.StatusCode)
	}
	var second dirtyResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &second)
	if second.DirtySince == nil || !second.DirtySince.Equal(*first.DirtySince) {
		t.Fatalf("a second change moved the wait start: %v then %v", first.DirtySince, second.DirtySince)
	}
}
