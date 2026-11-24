package route_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"loafkin.com/internal/route"
)

var denied = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
})

// guarded marks the response, so a test sees that the guard ran.
func guarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Guarded", "yes")
		next.ServeHTTP(w, r)
	})
}

func TestHandlerRunsOnlyBehindAGuardedPermission(t *testing.T) {
	tests := []struct {
		name        string
		permission  route.Permission
		guards      map[route.Permission]route.Guard
		wantStatus  int
		wantRan     bool
		wantGuarded string
	}{
		{"public needs no guard", route.Public, nil, http.StatusTeapot, true, ""},
		{"member through its guard", route.Member, map[route.Permission]route.Guard{route.Member: guarded}, http.StatusTeapot, true, "yes"},
		{"member without a guard", route.Member, nil, http.StatusNotFound, false, ""},
		{"undeclared", 0, map[route.Permission]route.Guard{0: guarded}, http.StatusNotFound, false, ""},
		{"below Public", -1, nil, http.StatusNotFound, false, ""},
		{"after the last permission", route.Member + 1, map[route.Permission]route.Guard{route.Member + 1: guarded}, http.StatusNotFound, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			table := route.NewTable(denied, tt.guards)
			table.HandleFunc("GET /x", tt.permission, func(w http.ResponseWriter, r *http.Request) {
				ran = true
				w.WriteHeader(http.StatusTeapot)
			})

			rec := httptest.NewRecorder()
			table.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			if rec.Code != tt.wantStatus || ran != tt.wantRan || rec.Header().Get("Guarded") != tt.wantGuarded {
				t.Errorf("GET /x behind permission %d: status %d, handler ran %v, guarded %q; want %d, %v, %q",
					tt.permission, rec.Code, ran, rec.Header().Get("Guarded"), tt.wantStatus, tt.wantRan, tt.wantGuarded)
			}
		})
	}
}

func TestRoutesListsEveryRegistration(t *testing.T) {
	table := route.NewTable(denied, nil)
	noop := func(http.ResponseWriter, *http.Request) {}
	table.HandleFunc("GET /a", route.Public, noop)
	table.HandleFunc("POST /b", 0, noop)

	got := table.Routes()
	want := []route.Route{
		{Pattern: "GET /a", Permission: route.Public},
		{Pattern: "POST /b", Permission: 0},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Routes() = %v, want %v", got, want)
	}
}

func TestErrorCodeReachesTheRequestsInfo(t *testing.T) {
	info := &route.Info{ID: "id"}
	r := route.WithInfo(httptest.NewRequest(http.MethodGet, "/", nil), info)

	route.SetErrorCode(r.WithContext(r.Context()), "some_code")

	if info.ErrorCode != "some_code" {
		t.Errorf("ErrorCode = %q after SetErrorCode on a copy of the request, want %q", info.ErrorCode, "some_code")
	}
}
