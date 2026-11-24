package route_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"loafkin.com/internal/route"
)

var denied = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
})

func TestHandlerRunsOnlyBehindADeclaredPermission(t *testing.T) {
	tests := []struct {
		name       string
		permission route.Permission
		wantStatus int
		wantRan    bool
	}{
		{"public", route.Public, http.StatusTeapot, true},
		{"undeclared", 0, http.StatusNotFound, false},
		{"below Public", -1, http.StatusNotFound, false},
		{"after the last permission", route.Public + 1, http.StatusNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			table := route.NewTable(denied)
			table.HandleFunc("GET /x", tt.permission, func(w http.ResponseWriter, r *http.Request) {
				ran = true
				w.WriteHeader(http.StatusTeapot)
			})

			rec := httptest.NewRecorder()
			table.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			if rec.Code != tt.wantStatus || ran != tt.wantRan {
				t.Errorf("GET /x behind permission %d: status %d, handler ran %v; want %d, %v",
					tt.permission, rec.Code, ran, tt.wantStatus, tt.wantRan)
			}
		})
	}
}

func TestRoutesListsEveryRegistration(t *testing.T) {
	table := route.NewTable(denied)
	noop := func(http.ResponseWriter, *http.Request) {}
	table.HandleFunc("GET /a", route.Public, noop)
	table.HandleFunc("POST /b", 0, noop)

	got := table.Routes()
	want := []route.Route{
		{Pattern: "GET /a", Permission: route.Public},
		{Pattern: "POST /b", Permission: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("Routes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Routes()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
