package worker

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/store"
)

func seedExpiredKeyStats(t *testing.T, st *store.Store, n int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var nodeID, keyID uuid.UUID
	if err := st.Pool.QueryRow(ctx,
		`INSERT INTO nodes (name, hostname) VALUES ('n', 'sweep.test') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx,
		`INSERT INTO access_keys (label, type, secret_enc) VALUES ('k', 'PERSONAL', '\x00') RETURNING id`).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-retention - 48*time.Hour)
	if _, err := st.Pool.Exec(ctx,
		`INSERT INTO key_stats_snapshots (access_key_id, node_id, taken_at, connections, total_octets)
		 SELECT $1, $2, $3::timestamptz + (g || ' seconds')::interval, 0, g FROM generate_series(1, $4::int) AS g`,
		keyID, nodeID, old, n); err != nil {
		t.Fatal(err)
	}
	return keyID
}

func countKeyStats(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM key_stats_snapshots`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSweepKeyStatsDeletesInBatches(t *testing.T) {
	st := store.OpenTest(t)
	// More rows than one batch holds, so the loop has to run more than once.
	seedExpiredKeyStats(t, st, keyStatsSweepBatch+250)
	s := NewStats(st, nil, 90*time.Second, slog.New(slog.DiscardHandler))
	s.sweepKeyStats(context.Background())
	if n := countKeyStats(t, st); n != 0 {
		t.Fatalf("the sweep must drain the whole expired tail, %d rows left", n)
	}
}

// I3: the sweep runs on its own schedule, not on every 60s tick.
func TestSweepKeyStatsRunsAtMostEveryTenMinutes(t *testing.T) {
	st := store.OpenTest(t)
	seedExpiredKeyStats(t, st, 10)
	s := NewStats(st, nil, 90*time.Second, slog.New(slog.DiscardHandler))
	base := time.Now()
	s.now = func() time.Time { return base }
	s.sweepKeyStats(context.Background())
	if n := countKeyStats(t, st); n != 0 {
		t.Fatalf("the first sweep must run, %d rows left", n)
	}

	seedExpiredKeyStats2(t, st, 10)
	// One tick later: still inside the window, so nothing is swept.
	s.now = func() time.Time { return base.Add(time.Minute) }
	s.sweepKeyStats(context.Background())
	if n := countKeyStats(t, st); n != 10 {
		t.Fatalf("a sweep inside the window must be skipped, %d rows left", n)
	}
	// Past the window: it runs again.
	s.now = func() time.Time { return base.Add(keyStatsSweepEvery + time.Second) }
	s.sweepKeyStats(context.Background())
	if n := countKeyStats(t, st); n != 0 {
		t.Fatalf("the sweep must run once the window has passed, %d rows left", n)
	}
}

// seedExpiredKeyStats2 adds rows to the key/node the first seeding created.
func seedExpiredKeyStats2(t *testing.T, st *store.Store, n int) {
	t.Helper()
	ctx := context.Background()
	var nodeID, keyID uuid.UUID
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM nodes LIMIT 1`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM access_keys LIMIT 1`).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-retention - 48*time.Hour)
	if _, err := st.Pool.Exec(ctx,
		`INSERT INTO key_stats_snapshots (access_key_id, node_id, taken_at, connections, total_octets)
		 SELECT $1, $2, $3::timestamptz + (g || ' seconds')::interval, 0, g FROM generate_series(1, $4::int) AS g`,
		keyID, nodeID, old, n); err != nil {
		t.Fatal(err)
	}
}
