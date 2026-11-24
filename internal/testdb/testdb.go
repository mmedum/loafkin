// Package testdb opens the scratch PostgreSQL that
// LOAFKIN_TEST_DATABASE_URL names, with loafkin's migrations applied
// (D-35). Only tests import it.
package testdb

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"loafkin.com/internal/migrations"
)

// Open returns a pool on the migrated test database, and skips the test
// when LOAFKIN_TEST_DATABASE_URL is not set. Every test in one test
// binary shares the pool, so the migrations run once per binary.
func Open(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("LOAFKIN_TEST_DATABASE_URL") == "" {
		t.Skip("LOAFKIN_TEST_DATABASE_URL is not set")
	}
	pool, err := shared()
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	return pool
}

// shared opens and migrates the test database once. The pool lives as
// long as the test binary.
var shared = sync.OnceValues(func() (*pgxpool.Pool, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("LOAFKIN_TEST_DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if _, err := migrations.Up(ctx, db); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
})

// NewAccount inserts an account that the test deletes when it ends,
// with its sessions.
func NewAccount(t testing.TB, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(t.Context(), `INSERT INTO accounts DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("inserting an account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})
	return id
}
