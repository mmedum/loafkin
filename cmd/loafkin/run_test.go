package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"loafkin.com/internal/store"
)

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestRunRefusesBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no command", nil},
		{"unknown command", []string{"serv"}},
		{"extra argument", []string{"serve", "now"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			err := run(t.Context(), tt.args, env(nil), &stderr)
			if !errors.Is(err, errUsage) {
				t.Errorf("run(%q) = %v, want errUsage", tt.args, err)
			}
			if !strings.HasPrefix(stderr.String(), "usage: loafkin") {
				t.Errorf("run(%q) wrote %q, want the usage text", tt.args, stderr.String())
			}
		})
	}
}

func TestRunNeedsADatabaseURL(t *testing.T) {
	var stderr bytes.Buffer
	err := run(t.Context(), []string{"migrate"}, env(nil), &stderr)
	if err == nil {
		t.Fatal("run(migrate) without a database URL = nil, want an error")
	}
	if got := logLines(t, &stderr); len(got) != 1 || got[0]["error_code"] != "config_missing" {
		t.Errorf("run(migrate) logged %v, want one line with error_code config_missing", got)
	}
}

// TestRunLogsNoConnectionDetails holds D-33 for a failed connection: the
// log carries a code, never pgx's error text with the host and user.
func TestRunLogsNoConnectionDetails(t *testing.T) {
	var stderr bytes.Buffer
	url := "postgres://canary-user@127.0.0.1:1/canary-db?connect_timeout=2"
	err := run(t.Context(), []string{"migrate"}, env(map[string]string{"LOAFKIN_DATABASE_URL": url}), &stderr)
	if err == nil {
		t.Fatal("run(migrate) against a closed port = nil, want an error")
	}
	if strings.Contains(stderr.String(), "canary") {
		t.Errorf("log carries connection details: %s", stderr.String())
	}
	if got := logLines(t, &stderr); len(got) != 1 || got[0]["error_code"] != "db_connect_failed" {
		t.Errorf("run(migrate) logged %v, want one line with error_code db_connect_failed", got)
	}
}

// unreachableStore has a pool whose every connection attempt fails.
func unreachableStore(t *testing.T) *store.Store {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), "postgres://loafkin@127.0.0.1:1/loafkin?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestHealth(t *testing.T) {
	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
		wantCode   any
	}{
		{"/livez", http.StatusOK, "ok\n", nil},
		{"/healthz", http.StatusServiceUnavailable, "unavailable: database\n", "db_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var logs bytes.Buffer
			h := newHandler(unreachableStore(t), slog.New(slog.NewJSONHandler(&logs, nil)))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
				t.Errorf("GET %s = %d %q, want %d %q", tt.path, rec.Code, rec.Body.String(), tt.wantStatus, tt.wantBody)
			}
			lines := logLines(t, &logs)
			if len(lines) != 1 || lines[0]["error_code"] != tt.wantCode {
				t.Errorf("GET %s logged %v, want one line with error_code %v", tt.path, lines, tt.wantCode)
			}
		})
	}
}

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	h := newHandler(unreachableStore(t), slog.New(slog.DiscardHandler))
	want := map[string]string{
		"Content-Security-Policy": "default-src 'none'; script-src 'self'; style-src 'self'; " +
			"img-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; " +
			"form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
	}
	for _, path := range []string{"/livez", "/no-such-page"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		for name, value := range want {
			if got := rec.Header().Get(name); got != value {
				t.Errorf("GET %s: %s = %q, want %q", path, name, got, value)
			}
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Errorf("GET %s: no X-Request-Id", path)
		}
	}
}

// TestCrossOriginPostIsRefused checks the refusal carries loafkin's own
// text and code, not Go's error text (D-14, D-33).
func TestCrossOriginPostIsRefused(t *testing.T) {
	var logs bytes.Buffer
	h := newHandler(unreachableStore(t), slog.New(slog.NewJSONHandler(&logs, nil)))
	req := httptest.NewRequest(http.MethodPost, "/livez", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || rec.Body.String() != "Forbidden\n" {
		t.Errorf("cross-site POST /livez = %d %q, want 403 %q", rec.Code, rec.Body.String(), "Forbidden\n")
	}
	if lines := logLines(t, &logs); len(lines) != 1 || lines[0]["error_code"] != "cross_origin" {
		t.Errorf("cross-site POST /livez logged %v, want one line with error_code cross_origin", lines)
	}
}

// TestPanicBeforeTheResponseDropsHandlerHeaders checks that the generic
// 500 does not carry a cookie the handler set before it panicked.
func TestPanicBeforeTheResponseDropsHandlerHeaders(t *testing.T) {
	var logs bytes.Buffer
	h := logRequests(slog.New(slog.NewJSONHandler(&logs, nil)), securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "__Host-session=half-made")
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("panicking handler answered %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("500 carries Set-Cookie %q, want none", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("500 carries Referrer-Policy %q, want no-referrer", got)
	}
	if lines := logLines(t, &logs); len(lines) != 1 || lines[0]["error_code"] != "panic" {
		t.Errorf("logged %v, want one line with error_code panic", lines)
	}
}

// TestPanicAfterTheResponseStartedAborts checks that a handler which
// panics mid-response gets its connection aborted and still a log line.
func TestPanicAfterTheResponseStartedAborts(t *testing.T) {
	var logs bytes.Buffer
	h := logRequests(slog.New(slog.NewJSONHandler(&logs, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "partial page")
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if v := recover(); v != http.ErrAbortHandler {
				t.Errorf("panic after writing recovered %v, want http.ErrAbortHandler", v)
			}
		}()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	if rec.Body.String() != "partial page" {
		t.Errorf("body = %q, want only the partial page", rec.Body.String())
	}
	if lines := logLines(t, &logs); len(lines) != 1 || lines[0]["error_code"] != "panic" {
		t.Errorf("logged %v, want one line with error_code panic", lines)
	}
}

// TestRequestLogCarriesNoPathOrSecrets drives a matched route, a member
// route whose session lookup fails, an unmatched one and a panic with
// canary values in the path, query, cookie, Authorization header and
// body (D-34).
func TestRequestLogCarriesNoPathOrSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom/{id}", func(http.ResponseWriter, *http.Request) { panic("canary-panic") })
	h := logRequests(logger, mux)
	live := newHandler(unreachableStore(t), logger)

	tests := []struct {
		handler    http.Handler
		method     string
		target     string
		wantRoute  string
		wantStatus float64
	}{
		{live, http.MethodGet, "/livez?token=canary-query", "GET /livez", 200},
		{live, http.MethodPost, "/sign-out?token=canary-query", "POST /sign-out", 500},
		{live, http.MethodGet, "/canary-path", "unmatched", 404},
		{h, http.MethodGet, "/boom/canary-id", "GET /boom/{id}", 500},
	}
	for _, tt := range tests {
		logs.Reset()
		req := httptest.NewRequest(tt.method, tt.target, strings.NewReader("canary-body"))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Cookie", "__Host-session=canary-cookie")
		req.Header.Set("Authorization", "Bearer canary-token")
		rec := httptest.NewRecorder()
		tt.handler.ServeHTTP(rec, req)

		if strings.Contains(logs.String(), "canary") {
			t.Errorf("%s %s: log carries a canary: %s", tt.method, tt.target, logs.String())
		}
		lines := logLines(t, &logs)
		if len(lines) != 1 || lines[0]["route"] != tt.wantRoute || lines[0]["status"] != tt.wantStatus {
			t.Errorf("%s %s logged %v, want one line with route %q and status %v",
				tt.method, tt.target, lines, tt.wantRoute, tt.wantStatus)
		}
		if strings.Contains(rec.Body.String(), "canary") {
			t.Errorf("%s %s: response carries a canary: %q", tt.method, tt.target, rec.Body.String())
		}
	}
}

func logLines(t *testing.T, r io.Reader) []map[string]any {
	t.Helper()
	var lines []map[string]any
	dec := json.NewDecoder(r)
	for dec.More() {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("decoding a log line: %v", err)
		}
		lines = append(lines, line)
	}
	return lines
}

// syncBuffer lets the test read what run logs while run is still writing.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestServeAnswersAndStops runs the real binary path against the scratch
// database: migrate, serve, answer health, and stop cleanly (D-29).
func TestServeAnswersAndStops(t *testing.T) {
	url := os.Getenv("LOAFKIN_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LOAFKIN_TEST_DATABASE_URL is not set")
	}
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(t.Context())
	var logs syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve"}, env(map[string]string{
			"LOAFKIN_DATABASE_URL": url,
			"LOAFKIN_ADDR":         addr,
		}), &logs)
	}()

	resp := getWhenUp(t, "http://"+addr+"/healthz")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Errorf("GET /healthz = %d %q, want 200 %q", resp.StatusCode, body, "ok\n")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run(serve) after cancel = %v, want nil; logs:\n%s", err, logs.String())
		}
	case <-time.After(shutdownTimeout + 5*time.Second):
		t.Fatal("run(serve) did not stop after cancel")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func getWhenUp(t *testing.T, url string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: server did not come up: %v", url, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
