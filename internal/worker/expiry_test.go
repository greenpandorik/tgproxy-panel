package worker_test

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func keyProfile(t *testing.T, f *fixture, keyID uuid.UUID) (enabled, secretServed bool) {
	t.Helper()
	des, err := worker.DesiredState(context.Background(), f.st, f.box, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range des.Req.Profiles {
		if p.Name == domain.ProfileName(keyID) {
			return p.Enabled, slices.Contains(des.Req.MTProxySecrets, p.Secret)
		}
	}
	t.Fatal("key profile missing from the desired state")
	return false, false
}

func TestExpiryTakesKeysOffWithoutRevokingThem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	soon := time.Now().Add(time.Second)
	k, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", ExpiresAt: &soon, NodeIDs: []uuid.UUID{f.node.ID}})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	_ = f.st.Q.SetKeyExpiry(ctx, db.SetKeyExpiryParams{ID: k.ID, ExpiresAt: &past})
	expiry := worker.NewExpiry(f.st, f.keys, slog.New(slog.DiscardHandler))
	n, err := expiry.RunOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expired %d err %v", n, err)
	}
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status == db.KeyStatusRevoked || kk.ExpiredAt == nil {
		t.Fatalf("an expired key must stay unrevoked and be marked expired: %+v", kk)
	}
	if enabled, served := keyProfile(t, f, k.ID); enabled || served {
		t.Fatal("an expired key must not be served")
	}
	if n, _ := expiry.RunOnce(ctx); n != 0 {
		t.Fatalf("an already expired key was handled again: %d", n)
	}

	if err := f.keys.Extend(ctx, k.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	kk, _ = f.st.Q.GetKey(ctx, k.ID)
	if kk.ExpiredAt != nil {
		t.Fatal("extending must clear the expired mark")
	}
	if enabled, served := keyProfile(t, f, k.ID); !enabled || !served {
		t.Fatal("an extended key must be served again")
	}
}

func TestDisabledKeyIsNotServedUntilEnabled(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: f.node.ID, Dirty: false})
	if err := f.keys.SetDisabled(ctx, k.ID, true); err != nil {
		t.Fatal(err)
	}
	if node, _ := f.st.Q.GetNode(ctx, f.node.ID); !node.Dirty {
		t.Fatal("turning a key off must schedule an apply")
	}
	if enabled, served := keyProfile(t, f, k.ID); enabled || served {
		t.Fatal("a disabled key must not be served")
	}
	if err := f.keys.SetDisabled(ctx, k.ID, false); err != nil {
		t.Fatal(err)
	}
	if enabled, served := keyProfile(t, f, k.ID); !enabled || !served {
		t.Fatal("an enabled key must be served again")
	}
	if err := f.keys.Revoke(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.SetDisabled(ctx, k.ID, true); err == nil {
		t.Fatal("a revoked key cannot be turned off or on")
	}
}
