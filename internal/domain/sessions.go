// Package domain holds loafkin's rules. Page and API handlers both call
// it, and it reaches the data only through the store (D-21).
package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"time"

	"loafkin.com/internal/store"
)

// D-13: a session lasts 30 days, or ends after 7 days unused. The last
// use is written at most once per touchInterval, so a busy member does
// not cost a write per request, and a session idles out between 7 days
// minus touchInterval and 7 days after its real last use.
const (
	// SessionLifetime is how long a session lasts, however often it is used.
	SessionLifetime = 30 * 24 * time.Hour
	sessionIdle     = 7 * 24 * time.Hour
	touchInterval   = time.Hour
)

// ErrNoSession reports a token that names no live session.
var ErrNoSession = errors.New("domain: no live session")

// Member is the signed-in caller of one session.
type Member struct {
	AccountID int64
	sessionID int64
}

// Sessions starts, resumes and ends members' sessions.
type Sessions struct {
	store *store.Store
	now   func() time.Time
}

// NewSessions returns Sessions on st that reads the time from now.
func NewSessions(st *store.Store, now func() time.Time) *Sessions {
	return &Sessions{store: st, now: now}
}

// Start opens a session for accountID and returns its token, which only
// the member's browser keeps.
func (s *Sessions) Start(ctx context.Context, accountID int64) (string, error) {
	token := rand.Text()
	now := s.now()
	err := s.store.CreateSession(ctx, accountID, hashToken(token), now, now.Add(SessionLifetime))
	if err != nil {
		return "", err
	}
	return token, nil
}

// Resume returns the member whose live session token names, and records
// the use. It returns ErrNoSession for an unknown, expired or idle token.
func (s *Sessions) Resume(ctx context.Context, token string) (Member, error) {
	now := s.now()
	sess, err := s.store.SessionByTokenHash(ctx, hashToken(token), now, now.Add(-sessionIdle))
	if errors.Is(err, store.ErrNotFound) {
		return Member{}, ErrNoSession
	}
	if err != nil {
		return Member{}, err
	}
	if now.Sub(sess.LastUsedAt) >= touchInterval {
		if err := s.store.TouchSession(ctx, sess.AccountID, sess.ID, now); err != nil {
			return Member{}, err
		}
	}
	return Member{AccountID: sess.AccountID, sessionID: sess.ID}, nil
}

// End signs m's session out.
func (s *Sessions) End(ctx context.Context, m Member) error {
	return s.store.DeleteSession(ctx, m.AccountID, m.sessionID)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
