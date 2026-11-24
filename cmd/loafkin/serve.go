package main

import (
	"context"
	"crypto/rand"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"loafkin.com/internal/route"
	"loafkin.com/internal/store"
)

// D-41: the read timeout leaves room for a photo upload on a slow phone
// connection. Live streams extend their own write deadline.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 20 * time.Second
	healthTimeout     = 2 * time.Second
)

// contentSecurityPolicy loads everything from loafkin's own origin and
// allows no inline script or style, no eval, no plugins, no framing and
// no <base> (D-32).
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; " +
	"img-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'"

// serve runs the website until ctx ends, then shuts down in the order
// D-29 sets: stop accepting requests, end the live streams, stop the
// workers, and close the database pool, which run's deferred Close does.
func serve(ctx context.Context, cfg config, pool *pgxpool.Pool, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fail("startup failed", "listen_failed")
	}

	srv := &http.Server{
		Handler:           newHandler(store.New(pool), logger),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		// net/http's own messages carry client addresses and raw error
		// text; recoverPanics logs the one that matters as a code.
		ErrorLog: log.New(io.Discard, "", 0),
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	logger.Info("serving", "addr", ln.Addr().String())

	select {
	case err := <-served:
		return fail("serve failed", store.ErrorCode(err, "serve_failed"))
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fail("shutdown incomplete", store.ErrorCode(err, "shutdown_failed"))
	}
	logger.Info("stopped")
	return nil
}

func newHandler(st *store.Store, logger *slog.Logger) http.Handler {
	// The deny handler answers with loafkin's own code and text, where Go's
	// default writes its own error text (D-33).
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setErrorCode(r, "cross_origin")
		writeText(w, http.StatusForbidden, "Forbidden")
	}))
	return logRequests(logger, securityHeaders(cop.Handler(routes(st))))
}

// routes builds the table of every route loafkin serves.
func routes(st *store.Store) *route.Table {
	t := route.NewTable()
	t.HandleFunc("GET /livez", route.Public, func(w http.ResponseWriter, r *http.Request) {
		writeText(w, http.StatusOK, "ok")
	})
	// healthz answers 503 while the database is out of reach. Nothing
	// restarts on it; a restart probe uses livez.
	t.HandleFunc("GET /healthz", route.Public, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()
		if err := st.Ping(ctx); err != nil {
			setErrorCode(r, store.ErrorCode(err, "db_unavailable"))
			writeText(w, http.StatusServiceUnavailable, "unavailable: database")
			return
		}
		writeText(w, http.StatusOK, "ok")
	})
	return t
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body+"\n")
}

func setSecurityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	h.Set("X-Content-Type-Options", "nosniff")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w.Header())
		next.ServeHTTP(w, r)
	})
}

type requestInfoKey struct{}

type requestInfo struct {
	id        string
	errorCode string
}

func requestInfoFrom(r *http.Request) *requestInfo {
	info, _ := r.Context().Value(requestInfoKey{}).(*requestInfo)
	if info == nil {
		return &requestInfo{}
	}
	return info
}

// setErrorCode records the code that the request's log line carries.
func setErrorCode(r *http.Request, code string) {
	requestInfoFrom(r).errorCode = code
}

// logRequests writes one line per request with the route pattern, the
// outcome, the duration and a request id, never the path, query, cookies,
// headers or body (D-34). It also recovers panics, so a panicking request
// gets its line too.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &requestInfo{id: rand.Text()[:16]}
		w.Header().Set("X-Request-Id", info.id)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		// No handler below copies the request, so ServeMux sets Pattern
		// on this one, and it is readable once ServeHTTP returns.
		r = r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info))

		defer func() {
			abort := false
			if v := recover(); v != nil {
				abort = answerPanic(sw, info, v)
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			attrs := []any{
				"route", route,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", info.id,
			}
			if info.errorCode != "" {
				attrs = append(attrs, "error_code", info.errorCode)
			}
			logger.Info("request", attrs...)
			if abort {
				panic(http.ErrAbortHandler)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

// answerPanic records a panic as a code, because a panic value can carry
// personal data (D-33). Before the response starts, it replaces whatever
// the handler set with a generic 500. After that, it reports that the
// connection must be aborted, since the client already has part of a
// response.
func answerPanic(sw *statusWriter, info *requestInfo, v any) (abort bool) {
	if v == http.ErrAbortHandler {
		info.errorCode = "aborted"
		return true
	}
	info.errorCode = "panic"
	if sw.wroteHeader {
		return true
	}
	h := sw.Header()
	clear(h)
	setSecurityHeaders(h)
	h.Set("X-Request-Id", info.id)
	writeText(sw, http.StatusInternalServerError, "Something went wrong. Request id: "+info.id)
	return false
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status, w.wroteHeader = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection, which live
// streams need to flush and set deadlines (D-19).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
