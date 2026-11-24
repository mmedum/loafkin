package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"loafkin.com/internal/mail"
	"loafkin.com/internal/migrations"
	"loafkin.com/internal/store"
)

const usage = `usage: loafkin <command>

commands:
  serve    apply pending migrations, then serve the website
  migrate  apply pending migrations and exit

environment:
  LOAFKIN_DATABASE_URL   PostgreSQL connection string (required)
  LOAFKIN_ADDR           address to listen on (default :8080)
  LOAFKIN_SMTP_ADDR      SMTP server host:port (required by serve)
  LOAFKIN_SMTP_TLS       implicit (default), starttls, or none for a local
                         mail catcher
  LOAFKIN_SMTP_USERNAME  SMTP user, if the server asks for one
  LOAFKIN_SMTP_PASSWORD  SMTP password
  LOAFKIN_MAIL_FROM      sender address of every email (required by serve)
`

// errUsage reports a command line that run answered with the usage text.
var errUsage = errors.New("loafkin: usage")

// failure carries only what may reach a log: a message of loafkin's own
// and a code, never raw error text (D-33, D-34).
type failure struct {
	msg  string
	code string
}

func (f *failure) Error() string { return f.msg + ": " + f.code }

func fail(msg, code string) error { return &failure{msg: msg, code: code} }

type config struct {
	addr        string
	databaseURL string
	smtp        mail.SMTP
}

// run is the whole program behind main, so tests can call it too (D-17).
// It logs every failure once, here, so main prints nothing.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))

	if len(args) != 1 || (args[0] != "serve" && args[0] != "migrate") {
		fmt.Fprint(stderr, usage)
		return errUsage
	}

	err := runCommand(ctx, args[0], getenv, logger)
	if f, ok := errors.AsType[*failure](err); ok {
		logger.Error(f.msg, "error_code", f.code)
	} else if err != nil {
		logger.Error("failed", "error_code", "unexpected")
	}
	return err
}

func runCommand(ctx context.Context, command string, getenv func(string) string, logger *slog.Logger) error {
	cfg := config{addr: getenv("LOAFKIN_ADDR"), databaseURL: getenv("LOAFKIN_DATABASE_URL")}
	if cfg.addr == "" {
		cfg.addr = ":8080"
	}
	if cfg.databaseURL == "" {
		return fail("LOAFKIN_DATABASE_URL is not set", "config_missing")
	}
	if command == "serve" {
		tlsMode, err := mail.ParseTLS(getenv("LOAFKIN_SMTP_TLS"))
		if err != nil {
			return fail("LOAFKIN_SMTP_TLS is implicit, starttls or none", "config_invalid")
		}
		cfg.smtp = mail.SMTP{
			Addr:     getenv("LOAFKIN_SMTP_ADDR"),
			Username: getenv("LOAFKIN_SMTP_USERNAME"),
			Password: getenv("LOAFKIN_SMTP_PASSWORD"),
			From:     getenv("LOAFKIN_MAIL_FROM"),
			TLS:      tlsMode,
		}
		if cfg.smtp.Addr == "" || cfg.smtp.From == "" {
			return fail("LOAFKIN_SMTP_ADDR and LOAFKIN_MAIL_FROM must be set", "config_missing")
		}
	}

	pool, err := openPool(ctx, cfg.databaseURL)
	if err != nil {
		return fail("startup failed", store.ErrorCode(err, "db_connect_failed"))
	}
	defer pool.Close()

	if err := migrate(ctx, pool, logger); err != nil {
		return err
	}
	if command == "migrate" {
		return nil
	}
	return serve(ctx, cfg, pool, logger)
}

func openPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// migrate runs before serve accepts requests, under goose's session lock
// (D-25).
func migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	applied, err := migrations.Up(ctx, db)
	if err != nil {
		return fail("migration failed", store.ErrorCode(err, "migration_failed"))
	}
	logger.Info("migrations applied", "count", applied)
	return nil
}
