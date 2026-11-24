// Package route keeps every route with the permission it declares
// (D-22), because http.ServeMux cannot list its patterns. It also holds
// what each request's log line carries, so every layer that answers a
// request can record its error code.
package route

import (
	"context"
	"io"
	"net/http"
	"slices"
)

// Permission is what a caller needs to reach a route. The zero value
// declares nothing, and such a route is denied.
type Permission int

const (
	// Public routes answer anyone, signed in or not.
	Public Permission = iota + 1
	// Member routes answer a signed-in member.
	Member

	permissionEnd // stays last, so Declared covers every permission above
)

// Declared reports whether p is a permission loafkin knows.
func (p Permission) Declared() bool {
	return p >= Public && p < permissionEnd
}

// Route is one registered pattern and its permission.
type Route struct {
	Pattern    string
	Permission Permission
}

// Guard wraps a route's handler with the check its permission needs.
type Guard func(http.Handler) http.Handler

// Table routes requests through a ServeMux and records every route.
type Table struct {
	mux    *http.ServeMux
	denied http.Handler
	guards map[Permission]Guard
	routes []Route
}

// NewTable returns an empty table. Each permission other than Public
// passes through its guard. A route whose permission is undeclared or
// has no guard answers through denied.
func NewTable(denied http.Handler, guards map[Permission]Guard) *Table {
	return &Table{mux: http.NewServeMux(), denied: denied, guards: guards}
}

// Handle registers h for pattern behind permission p.
func (t *Table) Handle(pattern string, p Permission, h http.Handler) {
	t.routes = append(t.routes, Route{Pattern: pattern, Permission: p})
	t.mux.Handle(pattern, t.guard(p, h))
}

func (t *Table) guard(p Permission, h http.Handler) http.Handler {
	if p == Public {
		return h
	}
	if g := t.guards[p]; p.Declared() && g != nil {
		return g(h)
	}
	return t.denied
}

func (t *Table) HandleFunc(pattern string, p Permission, h func(http.ResponseWriter, *http.Request)) {
	t.Handle(pattern, p, http.HandlerFunc(h))
}

// Routes returns every registered route in registration order.
func (t *Table) Routes() []Route {
	return slices.Clone(t.routes)
}

func (t *Table) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.mux.ServeHTTP(w, r)
}

// Info is what a request's log line carries besides its route, status
// and duration.
type Info struct {
	ID        string
	ErrorCode string
}

type infoKey struct{}

// WithInfo returns r carrying info, which later layers fill in.
func WithInfo(r *http.Request, info *Info) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), infoKey{}, info))
}

// InfoFrom returns the Info that r carries, or a detached one.
func InfoFrom(r *http.Request) *Info {
	if info, _ := r.Context().Value(infoKey{}).(*Info); info != nil {
		return info
	}
	return &Info{}
}

// SetErrorCode records the code that the request's log line carries.
func SetErrorCode(r *http.Request, code string) {
	InfoFrom(r).ErrorCode = code
}

// WriteText answers with a plain-text body that no cache keeps.
func WriteText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body+"\n")
}
