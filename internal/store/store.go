// Package store owns database access: pool, migrations, sqlc queries.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/store/migrations"
)

type Store struct {
	Pool *pgxpool.Pool
	Q    *db.Queries
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{Pool: pool, Q: db.New(pool)}, nil
}

// Migrate brings the schema up to date. It holds MigrateAdvisoryLockID for the
// whole run, so two processes migrating the same fresh database wait on each
// other instead of racing.
//
// The race is real and was seen in the field: `serve` migrates on start and every
// CLI command (`admin create` included) migrates before doing its job, and when
// the two land on a fresh database together they both run
// `CREATE EXTENSION IF NOT EXISTS pgcrypto`. IF NOT EXISTS is not atomic across
// sessions for extensions, so the loser fails with a duplicate-key error on
// pg_extension_name_index and the command dies before it ever creates the admin.
// Waiting on the lock makes the second migrator a no-op, which is what it should
// have been all along.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	lock, err := advisoryLock(ctx, pool, MigrateAdvisoryLockID)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer lock.Release()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close() //nolint:errcheck
	return goose.UpContext(ctx, sqlDB, ".")
}

// Tx runs fn inside a transaction; returning an error rolls back.
func (s *Store) Tx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(s.Q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Close() { s.Pool.Close() }
