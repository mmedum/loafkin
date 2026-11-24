package main

import (
	"slices"
	"testing"

	"loafkin.com/internal/route"
)

// undeclared returns the pattern of every route that declares no
// permission loafkin knows.
func undeclared(routes []route.Route) []string {
	var patterns []string
	for _, r := range routes {
		if !r.Permission.Declared() {
			patterns = append(patterns, r.Pattern)
		}
	}
	return patterns
}

func TestUndeclared(t *testing.T) {
	routes := []route.Route{
		{Pattern: "GET /open", Permission: route.Public},
		{Pattern: "POST /forgotten", Permission: 0},
	}
	got := undeclared(routes)
	if want := []string{"POST /forgotten"}; !slices.Equal(got, want) {
		t.Errorf("undeclared(%v) = %q, want %q", routes, got, want)
	}
}

// TestEveryRouteDeclaresAPermission holds D-22 over the full table.
func TestEveryRouteDeclaresAPermission(t *testing.T) {
	all := routes(nil).Routes()
	if bad := undeclared(all); len(bad) > 0 {
		t.Errorf("routes without a declared permission: %q", bad)
	}
	// GET /livez and GET /healthz at least.
	if len(all) < 2 {
		t.Errorf("read %d routes, want at least 2", len(all))
	}
}
