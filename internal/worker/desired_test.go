package worker_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type fixture struct {
	st   *store.Store
	box  *crypto.Box
	keys *keys.Service
	node db.Node
}

// newFixture builds a tproxy node: the engine is named explicitly because the column defaults
// to telemt, and these fixtures model the relay + MTProxy stack (backends, mtproxy secrets,
// tproxy_* metrics).
func newFixture(t *testing.T) *fixture { return newFixtureEngine(t, db.NodeEngineTproxy) }

// newTelemtFixture is newFixture for a node running the telemt engine.
func newTelemtFixture(t *testing.T) *fixture { return newFixtureEngine(t, db.NodeEngineTelemt) }

func newFixtureEngine(t *testing.T, engine db.NodeEngine) *fixture {
	t.Helper()
	st := store.OpenTest(t)
	box, _ := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)})
	ctx := context.Background()
	n, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "n", Hostname: "n.test", Engine: db.NullNodeEngine{NodeEngine: engine, Valid: true},
	})
	enc, _ := box.EncryptString("00000000000000000000000000000000")
	_, _ = st.Q.CreateProfile(ctx, db.CreateProfileParams{NodeID: n.ID, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")})
	return &fixture{st: st, box: box, keys: keys.New(st, box), node: n}
}

func TestDesiredStateIncludesDefaultAndKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "websocket", Limits: domain.ProfileLimits{MaxSessions: 3}, NodeIDs: []uuid.UUID{f.node.ID}})
	des, err := worker.DesiredState(ctx, f.st, f.box, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := des.Req
	if len(des.ProfileIDs) != len(req.Profiles) {
		t.Fatalf("profile ids %v do not match profiles %+v", des.ProfileIDs, req.Profiles)
	}
	if !req.ApplyProfiles || len(req.Profiles) != 2 || req.Profiles[0].Name != "default" || req.Profiles[1].Name != domain.ProfileName(k.ID) {
		t.Fatalf("profiles %+v", req.Profiles)
	}
	if req.Profiles[1].CarrierMode != "websocket" || req.Profiles[1].Limits == nil || req.Profiles[1].Limits.MaxSessions != 3 {
		t.Fatalf("profile settings lost: %+v", req.Profiles[1])
	}
	if len(req.MTProxySecrets) != 2 || req.MTProxySecrets[0] != "00000000000000000000000000000000" {
		t.Fatalf("secrets %v", req.MTProxySecrets)
	}
	if req.Site != nil {
		t.Fatal("no site assigned, Site must be nil")
	}
}

func TestDesiredStateOmitsRevoked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	_ = f.keys.Revoke(ctx, k.ID)
	des, _ := worker.DesiredState(ctx, f.st, f.box, f.node.ID)
	if len(des.Req.Profiles) != 1 || len(des.Req.MTProxySecrets) != 1 || len(des.ProfileIDs) != 1 {
		t.Fatalf("revoked key leaked: %+v", des)
	}
}

func TestDesiredStateCarriesTelemtLimitsAndExpiry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	exp := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	limits := domain.TelemtLimits{DataQuotaBytes: 5 << 30, RateLimitUpBps: 1_000_000, RateLimitDownBps: 2_000_000, MaxUniqueIPs: 3, MaxTCPConns: 64}
	k, err := f.keys.Create(ctx, keys.CreateInput{
		Label: "a", Type: domain.KeyPersonal, CarrierMode: "https",
		TelemtLimits: limits, ExpiresAt: &exp, NodeIDs: []uuid.UUID{f.node.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	des, err := worker.DesiredState(ctx, f.st, f.box, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got *nodedriver.Profile
	for i, p := range des.Req.Profiles {
		if p.Name == domain.ProfileName(k.ID) {
			got = &des.Req.Profiles[i]
		}
	}
	if got == nil {
		t.Fatalf("key profile missing: %+v", des.Req.Profiles)
	}
	if got.Telemt == nil || *got.Telemt != limits {
		t.Fatalf("telemt limits: %+v", got.Telemt)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("expiry: %v want %v", got.ExpiresAt, exp)
	}
	if !got.Enabled {
		t.Fatal("an active key's profile must be enabled")
	}
	// The node's own default profile has no key: no limits, no expiry, still enabled.
	def := des.Req.Profiles[0]
	if def.Name != "default" || def.Telemt != nil || def.ExpiresAt != nil || !def.Enabled {
		t.Fatalf("default profile: %+v", def)
	}
}

// A key with an empty telemt_limits object must not produce an all-zero limits struct: the
// agent reads non-nil as "the panel has an opinion" and would PATCH every user on every apply.
func TestDesiredStateLeavesEmptyTelemtLimitsNil(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	des, _ := worker.DesiredState(ctx, f.st, f.box, f.node.ID)
	for _, p := range des.Req.Profiles {
		if p.Name == domain.ProfileName(k.ID) && p.Telemt != nil {
			t.Fatalf("empty limits became %+v", p.Telemt)
		}
	}
}

// C2: the Fake-TLS listener travels with every apply on a telemt node, so an operator's edit of
// tls_domain/classic_port actually reaches the agent. A tproxy node sends neither.
func TestDesiredStateCarriesListenersForTelemtOnly(t *testing.T) {
	ctx := context.Background()
	tel := newTelemtFixture(t)
	dom := "front.example.com"
	if _, err := tel.st.Q.UpdateNode(ctx, db.UpdateNodeParams{
		ID: tel.node.ID, Name: tel.node.Name, PublicIp: tel.node.PublicIp,
		MaxProfiles: tel.node.MaxProfiles, AcmeEmail: tel.node.AcmeEmail,
		TlsDomain: &dom, ClassicPort: pgtype.Int4{Int32: 9443, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	des, err := worker.DesiredState(ctx, tel.st, tel.box, tel.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if des.Req.TLSDomain != dom || des.Req.ClassicPort != 9443 {
		t.Fatalf("telemt node must carry its listener: %q %d", des.Req.TLSDomain, des.Req.ClassicPort)
	}

	tp := newFixture(t)
	tpDes, err := worker.DesiredState(ctx, tp.st, tp.box, tp.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tpDes.Req.TLSDomain != "" || tpDes.Req.ClassicPort != 0 {
		t.Fatalf("a tproxy node has no Fake-TLS listener: %q %d", tpDes.Req.TLSDomain, tpDes.Req.ClassicPort)
	}
}
