package domain_test

import (
	"errors"
	"testing"
	"time"

	"loafkin.com/internal/domain"
	"loafkin.com/internal/store"
	"loafkin.com/internal/testdb"
)

// clock is a settable time source for Sessions.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var start = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func TestSessionResumesUntilItIdlesOrExpires(t *testing.T) {
	pool := testdb.Open(t)
	tests := []struct {
		name string
		// uses are the offsets from the start at which the member returns;
		// the last one is the use under test.
		uses     []time.Duration
		wantLive bool
	}{
		{"just started", []time.Duration{0}, true},
		{"one second before 7 idle days", []time.Duration{7*day - time.Second}, true},
		{"after 7 idle days", []time.Duration{7 * day}, false},
		{"used every 6 days, one second before 30 days", []time.Duration{6 * day, 12 * day, 18 * day, 24 * day, 30*day - time.Second}, true},
		{"used every 6 days, at 30 days", []time.Duration{6 * day, 12 * day, 18 * day, 24 * day, 30 * day}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &clock{start}
			sessions := domain.NewSessions(store.New(pool), c.now)
			account := testdb.NewAccount(t, pool)
			token, err := sessions.Start(t.Context(), account)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}

			var m domain.Member
			for _, at := range tt.uses {
				c.t = start.Add(at)
				m, err = sessions.Resume(t.Context(), token)
			}

			if live := err == nil; live != tt.wantLive || (live && m.AccountID != account) {
				t.Errorf("Resume after uses %v: member %+v, err %v; want live %v for account %d",
					tt.uses, m, err, tt.wantLive, account)
			}
			if !tt.wantLive && !errors.Is(err, domain.ErrNoSession) {
				t.Errorf("Resume after uses %v: err %v, want ErrNoSession", tt.uses, err)
			}
		})
	}
}

func TestResumeWritesTheLastUseAtMostHourly(t *testing.T) {
	pool := testdb.Open(t)
	tests := []struct {
		after    time.Duration
		wantLast time.Time
	}{
		{time.Hour - time.Second, start},
		{time.Hour, start.Add(time.Hour)},
	}
	for _, tt := range tests {
		c := &clock{start}
		sessions := domain.NewSessions(store.New(pool), c.now)
		account := testdb.NewAccount(t, pool)
		token, err := sessions.Start(t.Context(), account)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		c.t = start.Add(tt.after)
		if _, err := sessions.Resume(t.Context(), token); err != nil {
			t.Fatalf("Resume after %v: %v", tt.after, err)
		}

		var last time.Time
		if err := pool.QueryRow(t.Context(), `SELECT last_used_at FROM sessions WHERE account_id = $1`, account).Scan(&last); err != nil {
			t.Fatalf("reading last_used_at: %v", err)
		}
		if !last.Equal(tt.wantLast) {
			t.Errorf("Resume after %v: last_used_at %v, want %v", tt.after, last, tt.wantLast)
		}
	}
}

func TestSessionStoresOnlyTheTokensHash(t *testing.T) {
	pool := testdb.Open(t)
	sessions := domain.NewSessions(store.New(pool), (&clock{start}).now)
	account := testdb.NewAccount(t, pool)
	token, err := sessions.Start(t.Context(), account)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// PostgreSQL's own sha256 is the reference, so the expectation does
	// not come from the code under test.
	var hashed, rows int
	err = pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE token_hash = sha256(convert_to($2, 'UTF8'))), count(*)
		FROM sessions WHERE account_id = $1`, account, token).Scan(&hashed, &rows)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if rows != 1 || hashed != 1 {
		t.Errorf("account %d has %d sessions, %d stored as SHA-256 of the token; want 1, 1", account, rows, hashed)
	}
}

func TestEndedOrUnknownSessionDoesNotResume(t *testing.T) {
	pool := testdb.Open(t)
	sessions := domain.NewSessions(store.New(pool), (&clock{start}).now)
	account := testdb.NewAccount(t, pool)
	token, err := sessions.Start(t.Context(), account)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	m, err := sessions.Resume(t.Context(), token)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := sessions.End(t.Context(), m); err != nil {
		t.Fatalf("End: %v", err)
	}

	for name, tok := range map[string]string{"ended": token, "unknown": "NOTATOKENNOTATOKENNOTATOK"} {
		if _, err := sessions.Resume(t.Context(), tok); !errors.Is(err, domain.ErrNoSession) {
			t.Errorf("Resume of an %s token: err %v, want ErrNoSession", name, err)
		}
	}
}
