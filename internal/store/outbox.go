package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tx runs statements in one transaction, so a change and the email it
// causes commit together (D-29).
type Tx struct {
	tx pgx.Tx
}

// InTx runs fn in a transaction and commits it when fn returns nil.
func (s *Store) InTx(ctx context.Context, fn func(*Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Tx{tx: tx})
	})
}

// Email is one message in the outbox.
type Email struct {
	ID        int64
	AccountID *int64
	Recipient string
	Subject   string
	Body      string
	Attempts  int
}

// EnqueueEmail adds e to the outbox, to be sent from now until expires.
func (t *Tx) EnqueueEmail(ctx context.Context, e Email, now, expires time.Time) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO outbox (account_id, recipient, subject, body, next_attempt_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.AccountID, e.Recipient, e.Subject, e.Body, now, expires)
	return err
}

// ErrNoEmail reports that no email is due.
var ErrNoEmail = errors.New("store: no email due")

// SendNextEmail claims one due email and passes it to send, as the
// system identity background work runs as (D-21). The row stays locked
// while send runs, so another worker skips it. It is deleted when send
// succeeds. When send fails, the attempt is counted, the next one waits
// until retryAt(attempts), and SendNextEmail returns send's error.
func (s *Store) SendNextEmail(ctx context.Context, now time.Time, send func(Email) error, retryAt func(attempts int) time.Time) error {
	var sendErr error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var e Email
		err := tx.QueryRow(ctx, `
			SELECT id, account_id, recipient, subject, body, attempts FROM outbox
			WHERE next_attempt_at <= $1 AND expires_at > $1
			ORDER BY next_attempt_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED`, now).Scan(&e.ID, &e.AccountID, &e.Recipient, &e.Subject, &e.Body, &e.Attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoEmail
		}
		if err != nil {
			return err
		}
		if sendErr = send(e); sendErr != nil {
			_, err := tx.Exec(ctx, `UPDATE outbox SET attempts = attempts + 1, next_attempt_at = $2 WHERE id = $1`,
				e.ID, retryAt(e.Attempts+1))
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM outbox WHERE id = $1`, e.ID)
		return err
	})
	if err != nil {
		return err
	}
	return sendErr
}

// DeleteExpired removes the sessions and emails that can no longer be
// used: sessions past their end or idle since idleSince, and emails past
// their expiry. It runs as the system identity (D-21).
func (s *Store) DeleteExpired(ctx context.Context, now, idleSince time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1 OR last_used_at <= $2`, now, idleSince); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM outbox WHERE expires_at <= $1`, now)
		return err
	})
}
