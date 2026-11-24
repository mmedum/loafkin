// Package store holds every SQL statement that reads or writes loafkin's
// data. Each method that touches members' data takes the caller and
// returns only what that caller may see (D-21, D-24).
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store runs loafkin's queries on one connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store that runs its queries on pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Ping reports whether the database answers.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// ErrorCode turns err into a code that is safe to log: PostgreSQL's
// SQLSTATE where there is one, else fallback. Raw error text can carry
// personal data and connection details, so it never reaches a log (D-33).
func ErrorCode(err error, fallback string) string {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return "pg_" + pgErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return fallback
}
