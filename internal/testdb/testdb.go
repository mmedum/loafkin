// Package testdb opens the scratch PostgreSQL that
// LOAFKIN_TEST_DATABASE_URL names, with loafkin's migrations applied
// (D-35). Only tests import it.
package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"loafkin.com/internal/migrations"
)

// Open returns a pool on the migrated test database, and skips the test
// when LOAFKIN_TEST_DATABASE_URL is not set.
func Open(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("LOAFKIN_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LOAFKIN_TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if _, err := migrations.Up(t.Context(), db); err != nil {
		t.Fatalf("migrating the test database: %v", err)
	}
	return pool
}

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
