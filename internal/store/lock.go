package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdvisoryLock is a Postgres session-level advisory lock held on one connection
// checked out of the pool. Postgres releases it the moment that connection goes
// away - process exit, container kill, network drop - so there is no stale-lock
// heuristic to get wrong, and unlike a file it is visible to every process that
// can reach the database, in any container and on any host.
type AdvisoryLock struct {
	conn *pgxpool.Conn
	id   int64
}

// TryAdvisoryLock takes the advisory lock named by id without waiting. It reports
// false (and no error) when another database session already holds it.
//
// The lock is held on a connection the caller keeps checked out for as long as
// the lock is wanted, so a holder costs one connection out of the pool for its
// whole lifetime. Release must be called before the pool is closed:
// pgxpool.Close blocks until every acquired connection is back.
func (s *Store) TryAdvisoryLock(ctx context.Context, id int64) (*AdvisoryLock, bool, error) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire a connection for the advisory lock: %w", err)
	}
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", id).Scan(&got); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("pg_try_advisory_lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}
	return &AdvisoryLock{conn: conn, id: id}, true, nil
}

// Release unlocks and hands the connection back. It is safe to call more than
// once. The explicit unlock matters because the connection goes back into the
// pool for reuse: a session-level advisory lock outlives the transaction that
// took it and would otherwise ride along on the recycled connection.
func (l *AdvisoryLock) Release() {
	if l == nil || l.conn == nil {
		return
	}
	conn, id := l.conn, l.id
	l.conn = nil
	// A background context, deliberately: release runs on shutdown paths where the
	// request/signal context is already cancelled, and an unlock that is skipped
	// leaves the lock held on a pooled connection for the rest of the process.
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", id)
	conn.Release()
}
