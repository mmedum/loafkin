package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	all := routes(nil, nil).Routes()
	if bad := undeclared(all); len(bad) > 0 {
		t.Errorf("routes without a declared permission: %q", bad)
	}
	// GET /livez, GET /healthz and POST /sign-out at least.
	if len(all) < 3 {
		t.Errorf("read %d routes, want at least 3", len(all))
	}
}

func TestUndeclaredRouteAnswersLikeAMissingOne(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ran := false
	table := route.NewTable(http.HandlerFunc(denyUndeclared), nil)
	table.HandleFunc("GET /forgotten", 0, func(http.ResponseWriter, *http.Request) { ran = true })

	rec := httptest.NewRecorder()
	logRequests(logger, table).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/forgotten", nil))

	if rec.Code != http.StatusNotFound || rec.Body.String() != "Not Found\n" || ran {
		t.Errorf("GET /forgotten: status %d, body %q, handler ran %v; want 404, %q, false",
			rec.Code, rec.Body.String(), ran, "Not Found\n")
	}
	if lines := logLines(t, &logs); len(lines) != 1 || lines[0]["error_code"] != "permission_undeclared" {
		t.Errorf("GET /forgotten logged %v, want one line with error_code permission_undeclared", lines)
	}
}
