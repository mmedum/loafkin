// Package worker runs loafkin's background work in the app process:
// it sends the outbox and deletes what has expired (D-29).
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"loafkin.com/internal/domain"
	"loafkin.com/internal/store"
)

// Sender delivers one email. The key is the same for every attempt at
// one email, so a sender that takes an idempotency key sends it once
// (D-29).
type Sender interface {
	Send(ctx context.Context, key int64, to, subject, body string) error
}

// A failed send waits retryFirst, doubling each time up to retryMax,
// until the email expires or the sender refuses it for good.
const (
	retryFirst = 10 * time.Second
	retryMax   = 10 * time.Minute
)

// Worker sends the outbox every poll interval and deletes expired rows
// every sweep interval.
type Worker struct {
	Store    *store.Store
	Sessions *domain.Sessions
	Sender   Sender
	Logger   *slog.Logger
	Now      func() time.Time
	Poll     time.Duration
	Sweep    time.Duration
}

// Run works until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	poll := time.NewTicker(w.Poll)
	defer poll.Stop()
	sweep := time.NewTicker(w.Sweep)
	defer sweep.Stop()
	w.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			w.SendDue(ctx)
		case <-sweep.C:
			w.sweep(ctx)
		}
	}
}

// sendsPerPoll bounds one SendDue, so a row that is sent but never
// deleted cannot keep the worker in one loop.
const sendsPerPoll = 100

// SendDue sends the emails that are due, one at a time, up to
// sendsPerPoll, and stops at the first database error.
func (w *Worker) SendDue(ctx context.Context) {
	for range sendsPerPoll {
		if ctx.Err() != nil {
			return
		}
		var sendErr error
		err := w.Store.SendNextEmail(ctx, w.Now(), func(e store.Email) error {
			sendErr = w.Sender.Send(ctx, e.ID, e.Recipient, e.Subject, e.Body)
			return sendErr
		}, func(attempts int, err error) (time.Time, bool) {
			if p, ok := errors.AsType[permanent](err); ok && p.Permanent() {
				return time.Time{}, false
			}
			// Read after the send, which can take longer than a retry delay.
			return w.Now().Add(retryDelay(attempts)), true
		})
		switch {
		case errors.Is(err, store.ErrNoEmail):
			return
		case sendErr != nil && err == sendErr:
			// One email failed; the next may not.
			w.Logger.Info("email not sent", "error_code", errorCode(err, "send_failed"))
		case err != nil:
			// A shutdown cancels ctx in the middle of a claim; that is no fault.
			if ctx.Err() == nil {
				w.Logger.Info("outbox failed", "error_code", errorCode(err, "outbox_failed"))
			}
			return
		}
	}
}

func (w *Worker) sweep(ctx context.Context) {
	if err := w.Sessions.DeleteExpired(ctx); err != nil && ctx.Err() == nil {
		w.Logger.Info("sweep failed", "error_code", errorCode(err, "sweep_failed"))
	}
}

// retryDelay is the wait after the given number of failed attempts.
func retryDelay(attempts int) time.Duration {
	d := retryFirst
	for range attempts - 1 {
		d *= 2
		if d >= retryMax {
			return retryMax
		}
	}
	return d
}

// coded is an error from the sender that names its own code.
type coded interface {
	error
	ErrorCode() string
}

// permanent is an error from the sender that says whether a retry can
// succeed.
type permanent interface {
	error
	Permanent() bool
}

func errorCode(err error, fallback string) string {
	if c, ok := errors.AsType[coded](err); ok {
		return c.ErrorCode()
	}
	return store.ErrorCode(err, fallback)
}
