package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestExpiryRevokesExpiredKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	soon := time.Now().Add(time.Second)
	k, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", ExpiresAt: &soon, NodeIDs: []uuid.UUID{f.node.ID}})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	_ = f.st.Q.SetKeyExpiry(ctx, db.SetKeyExpiryParams{ID: k.ID, ExpiresAt: &past})
	n, err := worker.NewExpiry(f.st, f.keys, slog.New(slog.DiscardHandler)).RunOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expired %d err %v", n, err)
	}
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status != db.KeyStatusRevoked {
		t.Fatalf("status %s", kk.Status)
	}
}
