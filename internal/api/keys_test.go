package api_test

import (
	"bytes"
	"io"
	"mime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

type keyResp struct {
	ID     uuid.UUID `json:"id"`
	Label  string    `json:"label"`
	Type   string    `json:"type"`
	Status string    `json:"status"`
	Secret string    `json:"secret"`
	Links  []struct {
		Hostname string `json:"hostname"`
		Kind     string `json:"kind"`
		TMe      string `json:"tme"`
	} `json:"links"`
	Nodes []struct {
		NodeID uuid.UUID `json:"node_id"`
	} `json:"nodes"`
}

func ownerWithNode(t *testing.T) (*apitest.Harness, *apitest.Client, nodeResp) {
	t.Helper()
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	return h, c, n
}

func TestCreateKeyAndFetch(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var k keyResp
	resp := c.Post("/api/v1/keys", map[string]any{"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)
	// A default (telemt) node advertises both the WEB and the Fake-TLS link.
	if k.Status != "pending" || len(k.Links) != 2 || k.Links[0].Hostname != "n1.test" || len(k.Secret) != 32 {
		t.Fatalf("key %+v", k)
	}
	if k.Links[0].Kind != "web" || k.Links[1].Kind != "tls" {
		t.Fatalf("link kinds %+v", k.Links)
	}
	var got keyResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.Label != "Ivan" || len(got.Nodes) != 1 {
		t.Fatalf("get %+v", got)
	}
	png := c.Get("/api/v1/keys/" + k.ID.String() + "/qr?node=" + n.ID.String())
	body, _ := io.ReadAll(png.Body)
	if png.StatusCode != 200 || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("qr %d", png.StatusCode)
	}
}

func TestListKeysFilters(t *testing.T) {
	_, c, n := ownerWithNode(t)
	for _, in := range []map[string]any{
		{"label": "a", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}},
		{"label": "team", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}},
	} {
		if resp := c.Post("/api/v1/keys", in); resp.StatusCode != 201 {
			t.Fatalf("create %d", resp.StatusCode)
		}
	}
	var list struct {
		Items []keyResp `json:"items"`
		Total int       `json:"total"`
	}
	c.JSON(c.Get("/api/v1/keys?type=SHARED"), &list)
	if list.Total != 1 || list.Items[0].Label != "team" {
		t.Fatalf("filter type: %+v", list)
	}
	c.JSON(c.Get("/api/v1/keys?q=tea"), &list)
	if list.Total != 1 {
		t.Fatalf("filter q: %+v", list)
	}
	c.JSON(c.Get("/api/v1/keys?per_page=1"), &list)
	if list.Total != 2 || len(list.Items) != 1 {
		t.Fatalf("pagination: %+v", list)
	}
}

func TestBatchRevokeBulk(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var batch struct {
		Items []keyResp `json:"items"`
	}
	resp := c.Post("/api/v1/keys/batch", map[string]any{"prefix": "vip", "count": 3, "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	if resp.StatusCode != 201 {
		t.Fatalf("batch %d", resp.StatusCode)
	}
	c.JSON(resp, &batch)
	if len(batch.Items) != 3 {
		t.Fatalf("batch items %d", len(batch.Items))
	}
	if resp := c.Post("/api/v1/keys/"+batch.Items[0].ID.String()+"/revoke", nil); resp.StatusCode != 200 {
		t.Fatalf("revoke %d", resp.StatusCode)
	}
	resp = c.Post("/api/v1/keys/bulk", map[string]any{"action": "revoke", "ids": []string{batch.Items[1].ID.String(), batch.Items[2].ID.String()}})
	if resp.StatusCode != 200 {
		t.Fatalf("bulk %d", resp.StatusCode)
	}
	var list struct {
		Total int `json:"total"`
	}
	c.JSON(c.Get("/api/v1/keys?status=revoked"), &list)
	if list.Total != 3 {
		t.Fatalf("revoked total %d", list.Total)
	}
}

func TestViewerSeesNoSecret(t *testing.T) {
	h, c, n := ownerWithNode(t)
	var k keyResp
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "x", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}}), &k)
	h.CreateAdmin("v", "pass-123456", "viewer")
	v := h.Login("v", "pass-123456")
	var got keyResp
	v.JSON(v.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.Secret != "" || len(got.Links) != 0 {
		t.Fatalf("viewer must not see secret/links: %+v", got)
	}
}

func TestCapacityReturns409(t *testing.T) {
	_, c, n := ownerWithNode(t)
	c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"max_profiles": 1})
	resp := c.Post("/api/v1/keys", map[string]any{"label": "x", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// TestBulkExtendValidates covers I12: POST /keys/bulk action=extend used to write
// expires_at straight to the DB, so a past date silently scheduled the key for revocation
// and a revoked key reported success while staying dead.
func TestBulkExtendValidates(t *testing.T) {
	_, c, n := ownerWithNode(t)
	mk := func(label string) keyResp {
		var k keyResp
		c.JSON(c.Post("/api/v1/keys", map[string]any{"label": label, "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}}), &k)
		return k
	}
	live, revoked := mk("live"), mk("revoked")
	if resp := c.Post("/api/v1/keys/"+revoked.ID.String()+"/revoke", nil); resp.StatusCode != 200 {
		t.Fatalf("revoke %d", resp.StatusCode)
	}

	type bulkResult struct {
		Done   int               `json:"done"`
		Failed map[string]string `json:"failed"`
	}

	// A past date must be refused for every id rather than silently accepted.
	var past bulkResult
	c.JSON(c.Post("/api/v1/keys/bulk", map[string]any{
		"action": "extend", "ids": []string{live.ID.String()}, "expires_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}), &past)
	if past.Done != 0 || len(past.Failed) != 1 {
		t.Fatalf("past expiry accepted: %+v", past)
	}

	// A future date works for the live key and is reported per-id for the revoked one.
	var mixed bulkResult
	future := time.Now().Add(72 * time.Hour).UTC()
	c.JSON(c.Post("/api/v1/keys/bulk", map[string]any{
		"action": "extend", "ids": []string{live.ID.String(), revoked.ID.String()}, "expires_at": future.Format(time.RFC3339),
	}), &mixed)
	if mixed.Done != 1 || len(mixed.Failed) != 1 {
		t.Fatalf("expected 1 done + 1 per-id failure, got %+v", mixed)
	}
	if msg := mixed.Failed[revoked.ID.String()]; !strings.Contains(msg, "revoked") {
		t.Fatalf("failure must explain the revoked key: %q", msg)
	}
	var got struct {
		ExpiresAt *time.Time `json:"expires_at"`
	}
	c.JSON(c.Get("/api/v1/keys/"+live.ID.String()), &got)
	if got.ExpiresAt == nil || got.ExpiresAt.Sub(future).Abs() > time.Second {
		t.Fatalf("expiry not applied: %v", got.ExpiresAt)
	}
}

// TestKeyQRDispositionEscapesLabel covers debt item 9: the label is free text,
// so a quote in it used to truncate the filename the browser parsed out of
// Content-Disposition.
func TestKeyQRDispositionEscapesLabel(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var k keyResp
	c.JSON(c.Post("/api/v1/keys", map[string]any{
		"label": `He said "hi"; rm -rf`, "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()},
	}), &k)

	resp := c.Get("/api/v1/keys/" + k.ID.String() + "/qr?node=" + n.ID.String())
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		t.Fatalf("qr %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	typ, params, err := mime.ParseMediaType(cd)
	if err != nil {
		t.Fatalf("unparseable Content-Disposition %q: %v", cd, err)
	}
	if typ != "inline" {
		t.Fatalf("disposition type %q, want inline", typ)
	}
	if want := `He said "hi"; rm -rf-n1.test-web.png`; params["filename"] != want {
		t.Fatalf("filename %q, want %q", params["filename"], want)
	}
}
