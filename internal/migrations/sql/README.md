# Migrations

One goose SQL file per schema change, named `NNNNN_description.sql`.
D-25 in [`docs/architecture.md`](../../../docs/architecture.md) sets the
rules, and `migrations_test.go` checks the ones a file can show: each
file sets its lock timeout with `SET LOCAL lock_timeout`, and has a
Down section. A plain `SET` would outlive the migration's transaction
and stay on the connection after goose hands it back to the app's pool.
