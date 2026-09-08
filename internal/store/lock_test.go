package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestAdvisoryLockBlocksUntilReleased pins the primitive Migrate relies on: a
// second session asking for the same lock waits for the first to let go, rather
// than failing or proceeding.
func TestAdvisoryLockBlocksUntilReleased(t *testing.T) {
	st := OpenTest(t)
	ctx := context.Background()
	const id int64 = 0x7467_7770_7e57_0001 // a test-only id, far from the two real ones

	first, err := st.AdvisoryLock(ctx, id)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	got := make(chan time.Time, 1)
	go func() {
		second, err := st.AdvisoryLock(ctx, id)
		if err != nil {
			t.Errorf("second lock: %v", err)
			got <- time.Time{}
			return
		}
		got <- time.Now()
		second.Release()
	}()

	select {
	case <-got:
		t.Fatal("second lock was granted while the first was still held")
	case <-time.After(300 * time.Millisecond):
	}

	released := time.Now()
	first.Release()

	select {
	case at := <-got:
		if at.Before(released) {
			t.Fatal("second lock reports a grant before the first release")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second lock never granted after the first was released")
	}
}

// TestMigrateConcurrently runs Migrate from several goroutines at once. Every call
// must succeed, and the migration lock must be free afterwards; a leaked lock
// would make the next panel start hang forever.
func TestMigrateConcurrently(t *testing.T) {
	st := OpenTest(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Migrate(ctx, st.Pool)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Migrate: %v", err)
		}
	}

	lock, ok, err := st.TryAdvisoryLock(ctx, MigrateAdvisoryLockID)
	if err != nil {
		t.Fatalf("try lock after migrate: %v", err)
	}
	if !ok {
		t.Fatal("migration lock still held after every Migrate returned")
	}
	lock.Release()
}
