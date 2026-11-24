// Package migrations holds loafkin's SQL migrations, embedded in the
// binary, and applies them with goose under a PostgreSQL session lock
// (D-25).
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed sql
var files embed.FS

// versionTable is goose's own table of applied versions.
const versionTable = "goose_db_version"

// embedded returns the migration files embedded in the binary.
func embedded() fs.FS {
	sub, err := fs.Sub(files, "sql")
	if err != nil {
		panic(err) // the directory is embedded, so this cannot fail
	}
	return sub
}

// Up applies every pending embedded migration and returns how many it
// applied. Concurrent callers wait on one session lock, so a single
// instance migrates.
func Up(ctx context.Context, db *sql.DB) (int, error) {
	return up(ctx, db, embedded(), versionTable)
}

func up(ctx context.Context, db *sql.DB, fsys fs.FS, table string) (int, error) {
	// A waiting instance checks the lock every second for up to five
	// minutes, goose's default total, so it starts serving soon after the
	// migrating one finishes.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300))
	if err != nil {
		return 0, err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(locker),
		goose.WithTableName(table),
	)
	if errors.Is(err, goose.ErrNoMigrations) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	results, err := p.Up(ctx)
	return len(results), err
}
