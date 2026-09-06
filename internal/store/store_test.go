package store_test

import (
	"context"
	"testing"

	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func TestMigrateAndQuery(t *testing.T) {
	s := store.OpenTest(t)
	ctx := context.Background()
	u, err := s.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "root", PasswordHash: "x", Role: db.AdminRoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Q.GetAdminByUsername(ctx, "root")
	if err != nil || got.ID != u.ID {
		t.Fatalf("got %+v err %v", got, err)
	}
	n, err := s.Q.CountAdmins(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count %d err %v", n, err)
	}
}

func TestOpenTestTruncatesBetweenTests(t *testing.T) {
	s := store.OpenTest(t)
	n, _ := s.Q.CountAdmins(context.Background())
	if n != 0 {
		t.Fatalf("expected clean db, got %d admins", n)
	}
}

func TestTxRollsBack(t *testing.T) {
	s := store.OpenTest(t)
	ctx := context.Background()
	_ = s.Tx(ctx, func(q *db.Queries) error {
		_, _ = q.CreateAdmin(ctx, db.CreateAdminParams{Username: "a", PasswordHash: "x", Role: db.AdminRoleAdmin})
		return context.Canceled
	})
	n, _ := s.Q.CountAdmins(ctx)
	if n != 0 {
		t.Fatalf("expected rollback, got %d", n)
	}
}
