package api_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

type keyPresenceResp struct {
	Shared bool   `json:"shared"`
	Range  string `json:"range"`
	Step   int64  `json:"step"`
	Now    struct {
		People      int32 `json:"people"`
		People15m   int32 `json:"people_15m"`
		Connections int32 `json:"connections"`
	} `json:"now"`
	Nodes []struct {
		NodeID      uuid.UUID `json:"node_id"`
		NodeName    string    `json:"node_name"`
		People      int32     `json:"people"`
		Connections int32     `json:"connections"`
	} `json:"nodes"`
	Points []struct {
		T           time.Time `json:"t"`
		People      *int32    `json:"people"`
		Connections *int32    `json:"connections"`
	} `json:"points"`
}

func createKeyOn(t *testing.T, c *apitest.Client, typ string, nodes ...uuid.UUID) keyResp {
	t.Helper()
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.String())
	}
	resp := c.Post("/api/v1/keys", map[string]any{"label": "ComfyPublic", "type": typ, "carrier_mode": "https", "node_ids": ids})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create key %d %s", resp.StatusCode, b)
	}
	var k keyResp
	c.JSON(resp, &k)
	return k
}

func seedLivePresence(t *testing.T, h *apitest.Harness, keyID uuid.UUID, connections, devices, devices15m int) {
	t.Helper()
	if _, err := h.Store.Pool.Exec(context.Background(),
		`INSERT INTO key_presence (access_key_id, connections, devices, devices_15m) VALUES ($1, $2, $3, $4)`,
		keyID, connections, devices, devices15m); err != nil {
		t.Fatal(err)
	}
}

func seedPresenceSample(t *testing.T, h *apitest.Harness, keyID uuid.UUID, at time.Time, people, connections int, nodes string) {
	t.Helper()
	if _, err := h.Store.Pool.Exec(context.Background(),
		`INSERT INTO key_presence_samples (access_key_id, taken_at, people, connections, nodes) VALUES ($1, $2, $3, $4, $5::jsonb)`,
		keyID, at, people, connections, nodes); err != nil {
		t.Fatal(err)
	}
}

func getPresence(t *testing.T, c *apitest.Client, keyID uuid.UUID, query string) keyPresenceResp {
	t.Helper()
	resp := c.Get("/api/v1/keys/" + keyID.String() + "/presence" + query)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("presence %d %s", resp.StatusCode, b)
	}
	var got keyPresenceResp
	c.JSON(resp, &got)
	return got
}

func TestKeyPresenceSharedUser(t *testing.T) {
	h, c, a := ownerWithNode(t)
	b, _ := createNode(t, c, "n2.test")
	idle, _ := createNode(t, c, "n3.test")
	if resp := c.Put("/api/v1/nodes/order", map[string]any{"ids": []string{idle.ID.String(), b.ID.String(), a.ID.String()}}); resp.StatusCode != 204 && resp.StatusCode != 200 {
		t.Fatalf("order %d", resp.StatusCode)
	}
	k := createKeyOn(t, c, "SHARED", a.ID, b.ID, idle.ID)
	now := time.Now()
	seedLivePresence(t, h, k.ID, 20, 11, 15)
	seedPresenceSample(t, h, k.ID, now.Add(-3*time.Hour-2*time.Minute), 100, 300, `{}`)
	seedPresenceSample(t, h, k.ID, now.Add(-3*time.Hour-4*time.Minute), 200, 500, `{}`)
	seedPresenceSample(t, h, k.ID, now.Add(-30*time.Hour), 999, 999, `{}`)
	seedPresenceSample(t, h, k.ID, now.Add(-30*time.Second), 11, 20,
		`{"`+a.ID.String()+`": {"people": 7, "connections": 12}, "`+b.ID.String()+`": {"people": 5, "connections": 8}}`)

	got := getPresence(t, c, k.ID, "")
	if !got.Shared || got.Range != "24h" || got.Step != 900 {
		t.Fatalf("header: %+v", got)
	}
	if got.Now.People != 11 || got.Now.People15m != 15 || got.Now.Connections != 20 {
		t.Fatalf("now: %+v", got.Now)
	}
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes: %+v", got.Nodes)
	}
	want := []struct {
		id                  uuid.UUID
		people, connections int32
	}{{idle.ID, 0, 0}, {b.ID, 5, 8}, {a.ID, 7, 12}}
	for i, w := range want {
		n := got.Nodes[i]
		if n.NodeID != w.id || n.People != w.people || n.Connections != w.connections {
			t.Fatalf("node %d = %+v, want %+v", i, n, w)
		}
	}
	if len(got.Points) != 96 {
		t.Fatalf("%d points", len(got.Points))
	}
	filled := 0
	for _, p := range got.Points {
		if p.People != nil {
			filled++
		}
	}
	if filled != 2 {
		t.Fatalf("%d filled buckets, want 2", filled)
	}
	if p := got.Points[83]; p.People == nil || *p.People != 150 || *p.Connections != 400 {
		t.Fatalf("bucket three hours ago: %+v", p)
	}
	if p := got.Points[95]; p.People == nil || *p.People != 11 || *p.Connections != 20 {
		t.Fatalf("last bucket: %+v", p)
	}
	if d := got.Points[1].T.Sub(got.Points[0].T); d != 15*time.Minute {
		t.Fatalf("bucket width %s", d)
	}
	if d := time.Since(got.Points[95].T); d < 0 || d > time.Minute {
		t.Fatalf("last bucket ends %s ago", d)
	}

	week := getPresence(t, c, k.ID, "?range=7d")
	if week.Range != "7d" || week.Step != 6300 || len(week.Points) != 96 {
		t.Fatalf("7d: range %s step %d points %d", week.Range, week.Step, len(week.Points))
	}
	filled = 0
	for _, p := range week.Points {
		if p.People != nil {
			filled++
		}
	}
	if filled != 3 {
		t.Fatalf("7d: %d filled buckets, want 3", filled)
	}
}

func TestKeyPresenceStaleSplitReadsAsNobody(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := createKeyOn(t, c, "SHARED", n.ID)
	seedPresenceSample(t, h, k.ID, time.Now().Add(-10*time.Minute), 4, 9, `{"`+n.ID.String()+`": {"people": 4, "connections": 9}}`)
	got := getPresence(t, c, k.ID, "")
	if got.Now.People != 0 || len(got.Nodes) != 1 || got.Nodes[0].People != 0 {
		t.Fatalf("stale presence: now %+v nodes %+v", got.Now, got.Nodes)
	}
}

func TestKeyPresencePersonalUserHasNoHistory(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := createKeyOn(t, c, "PERSONAL", n.ID)
	seedLivePresence(t, h, k.ID, 3, 2, 2)
	got := getPresence(t, c, k.ID, "")
	if got.Shared || got.Now.People != 1 || got.Now.People15m != 1 || got.Now.Connections != 3 {
		t.Fatalf("personal: %+v", got)
	}
	if len(got.Nodes) != 0 || len(got.Points) != 0 {
		t.Fatalf("personal history: nodes %d points %d", len(got.Nodes), len(got.Points))
	}
}

func TestKeyPresenceRangeAndUnknownKey(t *testing.T) {
	_, c, n := ownerWithNode(t)
	k := createKeyOn(t, c, "SHARED", n.ID)
	if resp := c.Get("/api/v1/keys/" + k.ID.String() + "/presence?range=30d"); resp.StatusCode != 400 {
		t.Fatalf("bad range: %d", resp.StatusCode)
	}
	if resp := c.Get("/api/v1/keys/" + uuid.NewString() + "/presence"); resp.StatusCode != 404 {
		t.Fatalf("unknown key: %d", resp.StatusCode)
	}
}

func TestKeyPresenceReadersAndTokens(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := createKeyOn(t, c, "SHARED", n.ID)
	seedLivePresence(t, h, k.ID, 2, 2, 2)
	path := "/api/v1/keys/" + k.ID.String() + "/presence"

	h.CreateAdmin("view", "pass-123456", "viewer")
	vc := h.Login("view", "pass-123456")
	if got := getPresence(t, vc, k.ID, ""); got.Now.People != 2 {
		t.Fatalf("viewer: %+v", got.Now)
	}

	read := issueToken(t, c, "users:read")
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+read.Token)
	bearer.SendCSRF = false
	tokenStatus(t, bearer.Get(path), 200)

	other := issueToken(t, c, "nodes:read", "users:write")
	denied := h.Anonymous().SetHeader("Authorization", "Bearer "+other.Token)
	denied.SendCSRF = false
	tokenStatus(t, denied.Get(path), 403)

	tokenStatus(t, h.Anonymous().Get(path), 401)
}
