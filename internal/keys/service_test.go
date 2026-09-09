package keys_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func setup(t *testing.T) (*keys.Service, *store.Store, uuid.UUID) {
	t.Helper()
	st := store.OpenTest(t)
	box, _ := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)})
	ctx := context.Background()
	n, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := box.EncryptString("00000000000000000000000000000000")
	_, _ = st.Q.CreateProfile(ctx, db.CreateProfileParams{NodeID: n.ID, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")})
	return keys.New(st, box), st, n.ID
}

func TestCreatePersonalCreatesProfileAndMarksDirty(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	k, err := svc.Create(ctx, keys.CreateInput{Label: "Ivan", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if err != nil {
		t.Fatal(err)
	}
	if k.Status != db.KeyStatusPending || k.Type != db.KeyTypePERSONAL {
		t.Fatalf("key %+v", k)
	}
	profiles, _ := st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	if len(profiles) != 1 || profiles[0].Name != domain.ProfileName(k.ID) || profiles[0].NodeID != nodeID {
		t.Fatalf("profiles %+v", profiles)
	}
	n, _ := st.Q.GetNode(ctx, nodeID)
	if !n.Dirty {
		t.Fatal("node must be dirty")
	}
	secret, _ := svc.Secret(ctx, k)
	if domain.ValidateSecretHex(secret) != nil {
		t.Fatalf("bad secret %q", secret)
	}
	links, _ := svc.Links(ctx, k.ID)
	if len(links) != 1 || links[0].TMe != "https://t.me/webproxy?server=n.test&secret="+secret {
		t.Fatalf("links %+v", links)
	}
}

func TestCreateValidation(t *testing.T) {
	svc, _, nodeID := setup(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, keys.CreateInput{Label: "", Type: domain.KeyShared, NodeIDs: []uuid.UUID{nodeID}})
	var ve keys.ValidationError
	if !errors.As(err, &ve) || ve["label"] == "" {
		t.Fatalf("expected label validation, got %v", err)
	}
	_, err = svc.Create(ctx, keys.CreateInput{Label: "x", Type: domain.KeyShared, CarrierMode: "bogus", NodeIDs: []uuid.UUID{nodeID}})
	if !errors.As(err, &ve) || ve["carrier_mode"] == "" {
		t.Fatalf("expected carrier validation, got %v", err)
	}
	_, err = svc.Create(ctx, keys.CreateInput{Label: "x", Type: domain.KeyShared, CarrierMode: "https"})
	if !errors.As(err, &ve) || ve["node_ids"] == "" {
		t.Fatalf("expected node_ids validation, got %v", err)
	}
}

func TestCapacityLimit(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	_, _ = st.Q.UpdateNode(ctx, db.UpdateNodeParams{ID: nodeID, Name: "n", MaxProfiles: 2})
	if _, err := svc.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, keys.CreateInput{Label: "b", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if !errors.Is(err, keys.ErrCapacity) {
		t.Fatalf("expected ErrCapacity, got %v", err)
	}
}

func TestBatchCreate(t *testing.T) {
	svc, _, nodeID := setup(t)
	ks, err := svc.CreateBatch(context.Background(), keys.CreateInput{Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}, "team", 3)
	if err != nil || len(ks) != 3 || ks[0].Label != "team-1" || ks[2].Label != "team-3" {
		t.Fatalf("batch %v %v", ks, err)
	}
}

func TestRevokeRemovesProfilesAndRotateRenewsSecret(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	k, _ := svc.Create(ctx, keys.CreateInput{Label: "team", Type: domain.KeyShared, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	before, _ := svc.Secret(ctx, k)
	k2, err := svc.Rotate(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Secret(ctx, k2)
	if before == after {
		t.Fatal("rotate must change the secret")
	}
	profiles, _ := st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	got, _ := svc.Secret(ctx, db.AccessKey{SecretEnc: profiles[0].SecretEnc})
	if got != after {
		t.Fatal("profile secret not rotated")
	}
	if err := svc.Revoke(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	profiles, _ = st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	rk, _ := st.Q.GetKey(ctx, k.ID)
	if len(profiles) != 0 || rk.Status != db.KeyStatusRevoked || rk.RevokedAt == nil {
		t.Fatalf("after revoke: %d profiles, key %+v", len(profiles), rk)
	}
	if err := svc.Revoke(ctx, k.ID); err != nil {
		t.Fatalf("revoke must be idempotent: %v", err)
	}
}

func TestBindUnbind(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	n2, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n2", Hostname: "n2.test"})
	k, _ := svc.Create(ctx, keys.CreateInput{Label: "k", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if err := svc.Bind(ctx, k.ID, n2.ID); err != nil {
		t.Fatal(err)
	}
	links, _ := svc.Links(ctx, k.ID)
	if len(links) != 2 {
		t.Fatalf("links %d", len(links))
	}
	if err := svc.Unbind(ctx, k.ID, n2.ID); err != nil {
		t.Fatal(err)
	}
	profiles, _ := st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	if len(profiles) != 1 {
		t.Fatalf("profiles after unbind %d", len(profiles))
	}
}

func TestBatchCreateIsAtomic(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	// max_profiles 5, one 'default' profile already present => 4 free.
	_, _ = st.Q.UpdateNode(ctx, db.UpdateNodeParams{ID: nodeID, Name: "n", MaxProfiles: 5})

	_, err := svc.CreateBatch(ctx, keys.CreateInput{Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}, "team", 10)
	if !errors.Is(err, keys.ErrCapacity) {
		t.Fatalf("expected ErrCapacity, got %v", err)
	}
	if !strings.Contains(err.Error(), "n.test") {
		t.Errorf("error must name the node that is short: %v", err)
	}
	total, _ := st.Q.CountKeys(ctx, db.CountKeysParams{})
	if total != 0 {
		t.Fatalf("failed batch left %d orphan keys behind", total)
	}
	count, _ := st.Q.CountNodeProfiles(ctx, nodeID)
	if count != 1 {
		t.Fatalf("failed batch left %d profiles (expected only 'default')", count)
	}
	// A batch that fits still works.
	ks, err := svc.CreateBatch(ctx, keys.CreateInput{Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}, "team", 4)
	if err != nil || len(ks) != 4 {
		t.Fatalf("fitting batch failed: %v (%d keys)", err, len(ks))
	}
}

func TestRotateRejectsRevoked(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	k, err := svc.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyShared, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	var ve keys.ValidationError
	if _, err := svc.Rotate(ctx, k.ID); !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	after, _ := st.Q.GetKey(ctx, k.ID)
	if after.Status != db.KeyStatusRevoked {
		t.Fatalf("revoked key resurrected as %s", after.Status)
	}
}

// TestExtend covers I12: the bulk-extend path used to write expires_at unchecked.
func TestExtend(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	k, err := svc.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if err != nil {
		t.Fatal(err)
	}
	var ve keys.ValidationError
	if err := svc.Extend(ctx, k.ID, time.Now().Add(-time.Hour)); !errors.As(err, &ve) {
		t.Fatalf("past date accepted: %v", err)
	}
	future := time.Now().Add(48 * time.Hour)
	if err := svc.Extend(ctx, k.ID, future); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Q.GetKey(ctx, k.ID)
	if after.ExpiresAt == nil || after.ExpiresAt.Sub(future).Abs() > time.Second {
		t.Fatalf("expiry not written: %v", after.ExpiresAt)
	}
	if err := svc.Revoke(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Extend(ctx, k.ID, time.Now().Add(72*time.Hour)); !errors.As(err, &ve) {
		t.Fatalf("extending a revoked key must fail loudly, got %v", err)
	}
	if err := svc.Extend(ctx, uuid.New(), future); !errors.Is(err, keys.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
