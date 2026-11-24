// Package route keeps every route with the permission it declares
// (D-22), because http.ServeMux cannot list its patterns.
package route

import (
	"net/http"
	"slices"
)

// Permission is what a caller needs to reach a route. The zero value
// declares nothing, and such a route answers 404.
type Permission int

const (
	// Public routes answer anyone, signed in or not.
	Public Permission = iota + 1
)

// Declared reports whether p is a permission loafkin knows.
func (p Permission) Declared() bool {
	return p == Public
}

// Route is one registered pattern and its permission.
type Route struct {
	Pattern    string
	Permission Permission
}

// Table routes requests through a ServeMux and records every route.
type Table struct {
	mux    *http.ServeMux
	routes []Route
}

func NewTable() *Table {
	return &Table{mux: http.NewServeMux()}
}

// Handle registers h for pattern behind permission p. A route whose
// permission is not declared never reaches its handler.
func (t *Table) Handle(pattern string, p Permission, h http.Handler) {
	t.routes = append(t.routes, Route{Pattern: pattern, Permission: p})
	if !p.Declared() {
		h = http.HandlerFunc(http.NotFound)
	}
	t.mux.Handle(pattern, h)
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
