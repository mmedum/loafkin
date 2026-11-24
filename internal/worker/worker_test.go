package worker_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"loafkin.com/internal/domain"
	"loafkin.com/internal/store"
	"loafkin.com/internal/testdb"
	"loafkin.com/internal/worker"
)

var now = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

type sent struct{ to, subject, body string }

// fakeSender records what it sends, and fails for recipients in failFor.
type fakeSender struct {
	mu      sync.Mutex
	sent    []sent
	failFor map[string]bool
	delay   time.Duration
}

func (f *fakeSender) Send(ctx context.Context, to, subject, body string) error {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFor[to] {
		return errors.New("refused")
	}
	f.sent = append(f.sent, sent{to, subject, body})
	return nil
}

// emptyOutbox gives the test the outbox to itself; no other package's
// tests write to it.
func emptyOutbox(t *testing.T) (*pgxpool.Pool, *store.Store) {
	t.Helper()
	pool := testdb.Open(t)
	clear := func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM outbox`); err != nil {
			t.Fatalf("emptying the outbox: %v", err)
		}
	}
	clear()
	t.Cleanup(clear)
	return pool, store.New(pool)
}

func enqueue(t *testing.T, st *store.Store, to string, at, expires time.Time) {
	t.Helper()
	err := st.InTx(t.Context(), func(tx *store.Tx) error {
		return tx.EnqueueEmail(t.Context(), store.Email{Recipient: to, Subject: "Subject for " + to, Body: "Body\nfor " + to}, at, expires)
	})
	if err != nil {
		t.Fatalf("enqueueing for %s: %v", to, err)
	}
}

func newWorker(st *store.Store, sender worker.Sender) *worker.Worker {
	return &worker.Worker{
		Store:    st,
		Sessions: domain.NewSessions(st, func() time.Time { return now }),
		Sender:   sender,
		Logger:   slog.New(slog.DiscardHandler),
		Now:      func() time.Time { return now },
	}
}

func outboxRows(t *testing.T, pool *pgxpool.Pool) map[string]struct {
	attempts int
	next     time.Time
} {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT recipient, attempts, next_attempt_at FROM outbox`)
	if err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}
	defer rows.Close()
	got := map[string]struct {
		attempts int
		next     time.Time
	}{}
	for rows.Next() {
		var to string
		var r struct {
			attempts int
			next     time.Time
		}
		if err := rows.Scan(&to, &r.attempts, &r.next); err != nil {
			t.Fatalf("scanning the outbox: %v", err)
		}
		got[to] = r
	}
	return got
}

func TestSendDueSendsAndDeletesOnlyDueEmails(t *testing.T) {
	pool, st := emptyOutbox(t)
	enqueue(t, st, "due@example.com", now, now.Add(time.Hour))
	enqueue(t, st, "later@example.com", now.Add(time.Second), now.Add(time.Hour))
	enqueue(t, st, "expired@example.com", now.Add(-time.Hour), now)
	sender := &fakeSender{}

	newWorker(st, sender).SendDue(t.Context())

	want := []sent{{"due@example.com", "Subject for due@example.com", "Body\nfor due@example.com"}}
	if fmt.Sprint(sender.sent) != fmt.Sprint(want) {
		t.Errorf("sent %v, want %v", sender.sent, want)
	}
	rows := outboxRows(t, pool)
	if _, ok := rows["due@example.com"]; ok || len(rows) != 2 {
		t.Errorf("outbox holds %v after sending, want only later@ and expired@", rows)
	}
}

func TestFailedSendWaitsAndTheNextEmailStillGoes(t *testing.T) {
	pool, st := emptyOutbox(t)
	enqueue(t, st, "refused@example.com", now.Add(-time.Minute), now.Add(time.Hour))
	enqueue(t, st, "fine@example.com", now, now.Add(time.Hour))
	sender := &fakeSender{failFor: map[string]bool{"refused@example.com": true}}

	newWorker(st, sender).SendDue(t.Context())

	if len(sender.sent) != 1 || sender.sent[0].to != "fine@example.com" {
		t.Errorf("sent %v, want only fine@example.com", sender.sent)
	}
	rows := outboxRows(t, pool)
	if r := rows["refused@example.com"]; len(rows) != 1 || r.attempts != 1 || !r.next.Equal(now.Add(10*time.Second)) {
		t.Errorf("outbox holds %v, want refused@ with 1 attempt, next at %v", rows, now.Add(10*time.Second))
	}
}

func TestTwoWorkersSendEachEmailOnce(t *testing.T) {
	_, st := emptyOutbox(t)
	const emails = 20
	for i := range emails {
		enqueue(t, st, fmt.Sprintf("member%d@example.com", i), now, now.Add(time.Hour))
	}
	sender := &fakeSender{delay: 5 * time.Millisecond}

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { newWorker(st, sender).SendDue(t.Context()) })
	}
	wg.Wait()

	seen := map[string]int{}
	for _, s := range sender.sent {
		seen[s.to]++
	}
	for to, n := range seen {
		if n != 1 {
			t.Errorf("%s was sent %d times, want 1", to, n)
		}
	}
	if len(seen) != emails {
		t.Errorf("sent to %d recipients, want %d", len(seen), emails)
	}
}

func TestSweepDeletesExpiredSessionsAndEmails(t *testing.T) {
	pool, st := emptyOutbox(t)
	account := testdb.NewAccount(t, pool)
	insert := func(created, lastUsed, expires time.Time) {
		t.Helper()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO sessions (account_id, token_hash, created_at, last_used_at, expires_at)
			VALUES ($1, sha256(gen_random_uuid()::text::bytea), $2, $3, $4)`, account, created, lastUsed, expires)
		if err != nil {
			t.Fatalf("inserting a session: %v", err)
		}
	}
	day := 24 * time.Hour
	insert(now.Add(-day), now.Add(-time.Minute), now.Add(29*day)) // live
	insert(now.Add(-8*day), now.Add(-7*day), now.Add(22*day))     // idle 7 days
	insert(now.Add(-30*day), now.Add(-time.Minute), now)          // at its end
	enqueue(t, st, "live@example.com", now, now.Add(time.Second))
	enqueue(t, st, "expired@example.com", now.Add(-time.Hour), now)

	if err := newWorker(st, &fakeSender{}).Sessions.DeleteExpired(t.Context()); err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}

	var sessions int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE account_id = $1`, account).Scan(&sessions); err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	rows := outboxRows(t, pool)
	if _, ok := rows["live@example.com"]; sessions != 1 || len(rows) != 1 || !ok {
		t.Errorf("after the sweep: %d sessions and outbox %v; want 1 session and only live@", sessions, rows)
	}
}
