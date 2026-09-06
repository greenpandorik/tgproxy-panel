package store_test

import (
	"context"
	"testing"

	"tgwebproxy/internal/store"
)

// lockID is picked to be nothing the panel itself uses, so this test cannot
// collide with a `panel serve` that happens to be pointed at the test database.
const lockID int64 = 0x7465_7374_0000_0001

// TestTryAdvisoryLockIsExclusiveAcrossConnections is the property the whole
// single-instance guard rests on: a second database session must not be able to
// take a lock a first one is holding, and must be able to as soon as it is
// released. Two pools stand in for two processes - which, unlike two pids, is a
// distinction Postgres can actually make across containers.
func TestTryAdvisoryLockIsExclusiveAcrossConnections(t *testing.T) {
	first := store.OpenTest(t)
	second := store.OpenTest(t)
	ctx := context.Background()

	held, ok, err := first.TryAdvisoryLock(ctx, lockID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("could not take a free lock")
	}

	if _, ok, err := second.TryAdvisoryLock(ctx, lockID); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("a second session took a lock the first one holds")
	}

	// A different id is a different lock: the namespace is shared, so this is
	// what keeps a future lock from accidentally standing in for this one.
	if other, ok, err := second.TryAdvisoryLock(ctx, lockID+1); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("an unrelated lock id was blocked")
	} else {
		other.Release()
	}

	held.Release()
	after, ok, err := second.TryAdvisoryLock(ctx, lockID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the lock was not released")
	}
	after.Release()
	// Releasing twice must not panic or double-unlock: shutdown paths call it
	// from a defer that may already have run.
	after.Release()
}

// TestReleaseDoesNotLeakTheLockOntoARecycledConnection: a session-level advisory
// lock outlives the transaction that took it, so handing the connection back to
// the pool without an explicit unlock would leave the lock held by whoever
// picks that connection up next.
func TestReleaseDoesNotLeakTheLockOntoARecycledConnection(t *testing.T) {
	s := store.OpenTest(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		l, ok, err := s.TryAdvisoryLock(ctx, lockID+2)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("iteration %d: the lock was still held after release", i)
		}
		l.Release()
	}
	var count int
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND ((classid::bigint << 32) | objid::bigint) = $1`,
		lockID+2).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d advisory locks still held after release", count)
	}
}
