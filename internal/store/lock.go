package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdvisoryLock is a Postgres session-level advisory lock held on one connection checked out of the pool.
type AdvisoryLock struct {
	conn *pgxpool.Conn
	own  *pgx.Conn
	id   int64
}

// MigrateAdvisoryLockID serialises schema migrations across every process that talks to this database.
const MigrateAdvisoryLockID int64 = 0x7467_7770_0000_0002

// AdvisoryLock takes the advisory lock named by id, waiting for it if another session holds it.
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

// TryAdvisoryLock takes the advisory lock named by id without waiting.
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

// Release unlocks and hands the connection back.
func (l *AdvisoryLock) Release() {
	if l == nil {
		return
	}
	if l.own != nil {
		own := l.own
		l.own = nil
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
