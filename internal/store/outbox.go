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

// afterSendTimeout bounds the statements that record a send. They run
// even after ctx is canceled, so a shutdown cannot undo a delivery.
const afterSendTimeout = 5 * time.Second

// SendNextEmail claims one due email and passes it to attempt, as the
// system identity background work runs as (D-21). The row stays locked
// while attempt runs, so another worker skips it. When attempt returns
// retry, the attempt is counted and the next one waits until retryAt;
// otherwise the row is deleted. The error is only ever the database's.
func (s *Store) SendNextEmail(ctx context.Context, now time.Time, attempt func(Email) (retryAt time.Time, retry bool)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	after, cancel := context.WithTimeout(context.WithoutCancel(ctx), afterSendTimeout)
	defer cancel()
	defer func() { _ = tx.Rollback(after) }()

	var e Email
	err = tx.QueryRow(ctx, `
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
	if at, retry := attempt(e); retry {
		_, err = tx.Exec(after, `UPDATE outbox SET attempts = attempts + 1, next_attempt_at = $2 WHERE id = $1`, e.ID, at)
	} else {
		_, err = tx.Exec(after, `DELETE FROM outbox WHERE id = $1`, e.ID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(after)
}

// DeleteExpiredEmails removes the emails past their expiry, as the
// system identity (D-21).
func (s *Store) DeleteExpiredEmails(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM outbox WHERE expires_at <= $1`, now)
	return err
}
