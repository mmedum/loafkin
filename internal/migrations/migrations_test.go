package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// checkFile lists the D-25 rules that one migration file breaks.
func checkFile(content string) []string {
	var broken []string
	if !strings.Contains(content, "-- +goose Up") {
		broken = append(broken, "no Up section")
	}
	if !strings.Contains(content, "-- +goose Down") {
		broken = append(broken, "no Down section")
	}
	// A session-level SET would outlive the migration's transaction and
	// go back into the app's pool with the connection.
	if !strings.Contains(content, "SET LOCAL lock_timeout") {
		broken = append(broken, "no SET LOCAL lock_timeout")
	}
	return broken
}

func TestCheckFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"complete", "-- +goose Up\nSET LOCAL lock_timeout = '5s';\n-- +goose Down\n", nil},
		{"no down", "-- +goose Up\nSET LOCAL lock_timeout = '5s';\n", []string{"no Down section"}},
		{"session lock timeout", "-- +goose Up\nSET lock_timeout = '5s';\n-- +goose Down\n", []string{"no SET LOCAL lock_timeout"}},
		{"empty", "", []string{"no Up section", "no Down section", "no SET LOCAL lock_timeout"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkFile(tt.content); !slices.Equal(got, tt.want) {
				t.Errorf("checkFile(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestEmbeddedMigrationsFollowTheRules(t *testing.T) {
	entries, err := fs.ReadDir(embedded(), ".")
	if err != nil {
		t.Fatalf("reading the embedded migrations: %v", err)
	}
	// The README is always embedded, so an empty listing means the embed
	// broke rather than that there is nothing to check.
	if len(entries) == 0 {
		t.Fatalf("embedded migrations list %d entries, want at least 1", len(entries))
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		content, err := fs.ReadFile(embedded(), e.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		if broken := checkFile(string(content)); len(broken) > 0 {
			t.Errorf("%s breaks D-25: %s", e.Name(), strings.Join(broken, ", "))
		}
	}
}

// testDB opens the scratch database that LOAFKIN_TEST_DATABASE_URL names,
// and skips the test without one (D-35).
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("LOAFKIN_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LOAFKIN_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// sampleMigrations creates two tables whose names carry suffix, so the
// test leaves no trace another run can collide with. Each sleeps, so two
// concurrent runs overlap.
func sampleMigrations(suffix string) fstest.MapFS {
	tables := sampleTables(suffix)
	file := func(table string) *fstest.MapFile {
		return &fstest.MapFile{Data: fmt.Appendf(nil,
			"-- +goose Up\nSET LOCAL lock_timeout = '5s';\nSELECT pg_sleep(0.5);\nCREATE TABLE %[1]s (id int);\n-- +goose Down\nDROP TABLE %[1]s;\n",
			table)}
	}
	return fstest.MapFS{
		"00001_first.sql":  file(tables[0]),
		"00002_second.sql": file(tables[1]),
	}
}

func sampleTables(suffix string) []string {
	return []string{"migrations_test_a_" + suffix, "migrations_test_b_" + suffix}
}

func TestUpAppliesEachMigrationOnce(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	suffix := fmt.Sprint(time.Now().UnixNano())
	table := "migrations_test_version_" + suffix
	t.Cleanup(func() { dropTables(t, db, suffix, table) })

	// Two instances starting at once: the session lock lets one migrate
	// and the other find nothing left to do.
	var wg sync.WaitGroup
	counts := make([]int, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { counts[i], errs[i] = up(ctx, db, sampleMigrations(suffix), table) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent up %d: %v", i, err)
		}
	}
	if got := counts[0] + counts[1]; got != 2 {
		t.Errorf("concurrent up applied %d migrations in total (%v), want 2", got, counts)
	}

	again, err := up(ctx, db, sampleMigrations(suffix), table)
	if err != nil {
		t.Fatalf("second up: %v", err)
	}
	if again != 0 {
		t.Errorf("second up applied %d migrations, want 0", again)
	}
}

func TestUpWithNoMigrationsDoesNothing(t *testing.T) {
	db := testDB(t)
	got, err := up(t.Context(), db, fstest.MapFS{"README.md": {Data: []byte("x")}}, "unused")
	if err != nil || got != 0 {
		t.Errorf("up over no migrations = %d, %v; want 0, nil", got, err)
	}
}

func dropTables(t *testing.T, db *sql.DB, suffix, table string) {
	t.Helper()
	for _, name := range append(sampleTables(suffix), table) {
		if _, err := db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+name); err != nil {
			t.Errorf("dropping %s: %v", name, err)
		}
	}
}
