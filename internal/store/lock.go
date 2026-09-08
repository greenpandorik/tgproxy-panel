package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdvisoryLock is a Postgres session-level advisory lock held on one connection
// checked out of the pool. Postgres releases it the moment that connection goes
// away - process exit, container kill, network drop - so there is no stale-lock
// heuristic to get wrong, and unlike a file it is visible to every process that
// can reach the database, in any container and on any host.
type AdvisoryLock struct {
	// Exactly one of these is set: conn for a lock taken on a pooled connection
	// (TryAdvisoryLock, which never waits), own for one taken on a standalone
	// connection (AdvisoryLock, which may queue - see there for why it must not
	// sit on the pool).
	conn *pgxpool.Conn
	own  *pgx.Conn
	id   int64
}

// MigrateAdvisoryLockID serialises schema migrations across every process that
// talks to this database. It lives next to the panel's single-instance lock
// (panelAdvisoryLockID in cmd/panel, ...0001) in the same flat int64 namespace,
// so the two can never be confused: ...0001 says "one panel runs", ...0002 says
// "one migrator runs".
const MigrateAdvisoryLockID int64 = 0x7467_7770_0000_0002

// AdvisoryLock takes the advisory lock named by id, waiting for it if another
// session holds it. Use this where the caller must proceed once the holder is
// done rather than give up; TryAdvisoryLock is for the "someone else is already
// doing this, so I should not" case.
//
// The wait happens on a connection opened outside the pool, on purpose. A
// waiter that sat on a pooled connection would hold that connection for as long
// as it queued, and enough waiters exhaust the pool: the holder then cannot get
// a connection for the work the lock protects, everyone waits on everyone, and
// nothing ever finishes. That is not a hypothetical - it was a ten-minute hang
// in CI, where the pool defaults to four connections and eight migrators
// queued. A standalone connection costs Postgres one backend per waiter and
// costs the pool nothing.
func (s *Store) AdvisoryLock(ctx context.Context, id int64) (*AdvisoryLock, error) {
	return advisoryLock(ctx, s.Pool, id)
}

func advisoryLock(ctx context.Context, pool *pgxpool.Pool, id int64) (*AdvisoryLock, error) {
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("open a connection for the advisory lock: %w", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", id); err != nil {
		_ = conn.Close(context.Background())
		return nil, fmt.Errorf("pg_advisory_lock: %w", err)
	}
	return &AdvisoryLock{own: conn, id: id}, nil
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
	if l == nil {
		return
	}
	// A background context, deliberately: release runs on shutdown paths where the
	// request/signal context is already cancelled, and an unlock that is skipped
	// leaves the lock held on a pooled connection for the rest of the process.
	if l.own != nil {
		own := l.own
		l.own = nil
		// Closing the standalone connection releases every session-level lock it
		// held; the explicit unlock first is for the ordinary case where the close
		// itself is what takes time.
		_, _ = own.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", l.id)
		_ = own.Close(context.Background())
		return
	}
	if l.conn == nil {
		return
	}
	conn, id := l.conn, l.id
	l.conn = nil
	_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", id)
	conn.Release()
}
