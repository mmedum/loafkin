package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound reports that no row the caller may see matches.
var ErrNotFound = errors.New("store: not found")

// Session is a signed-in browser of one account.
type Session struct {
	ID         int64
	AccountID  int64
	LastUsedAt time.Time
}

// CreateSession stores a session for accountID under the hash of its
// token.
func (s *Store) CreateSession(ctx context.Context, accountID int64, tokenHash []byte, now, expires time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (account_id, token_hash, created_at, last_used_at, expires_at)
		VALUES ($1, $2, $3, $3, $4)`,
		accountID, tokenHash, now, expires)
	return err
}

// SessionByTokenHash returns the session that tokenHash names, if it
// expires after now and was used after idleSince. Holding the token is
// what makes the caller that session's member (D-21).
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte, now, idleSince time.Time) (Session, error) {
	var sess Session
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, last_used_at FROM sessions
		WHERE token_hash = $1 AND expires_at > $2 AND last_used_at > $3`,
		tokenHash, now, idleSince).Scan(&sess.ID, &sess.AccountID, &sess.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

// TouchSession records that the account's session was used at now.
func (s *Store) TouchSession(ctx context.Context, accountID, sessionID int64, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET last_used_at = $3
		WHERE account_id = $1 AND id = $2`,
		accountID, sessionID, now)
	return err
}

// DeleteSession ends one of the account's sessions.
func (s *Store) DeleteSession(ctx context.Context, accountID, sessionID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE account_id = $1 AND id = $2`,
		accountID, sessionID)
	return err
}
