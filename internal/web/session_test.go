package web_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"loafkin.com/internal/domain"
	"loafkin.com/internal/route"
	"loafkin.com/internal/store"
	"loafkin.com/internal/testdb"
	"loafkin.com/internal/web"
)

type fixture struct {
	table    *route.Table
	sessions *domain.Sessions
	account  int64
	token    string
}

// newFixture serves the page routes plus GET /me, a member route that
// answers with the member's account id.
func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := testdb.Open(t)
	sessions := domain.NewSessions(store.New(pool), time.Now)
	table := route.NewTable(http.NotFoundHandler(), map[route.Permission]route.Guard{
		route.Member: web.RequireMember(sessions),
	})
	table.HandleFunc("GET /me", route.Member, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, web.MemberFrom(r).AccountID)
	})
	web.Register(table, sessions)

	account := testdb.NewAccount(t, pool)
	token, err := sessions.Start(t.Context(), account)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return fixture{table, sessions, account, token}
}

// serve sends req through the table and returns the response with the
// error code the request recorded.
func (f fixture) serve(req *http.Request) (*httptest.ResponseRecorder, string) {
	info := &route.Info{ID: "test"}
	rec := httptest.NewRecorder()
	f.table.ServeHTTP(rec, route.WithInfo(req, info))
	return rec, info.ErrorCode
}

func withSession(req *http.Request, token string) *http.Request {
	req.AddCookie(&http.Cookie{Name: "__Host-session", Value: token})
	return req
}

func TestMemberRouteNeedsALiveSession(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name        string
		req         *http.Request
		wantStatus  int
		wantBody    string
		wantCode    string
		wantCleared bool
	}{
		{"live session", withSession(httptest.NewRequest(http.MethodGet, "/me", nil), f.token),
			http.StatusOK, fmt.Sprint(f.account), "", false},
		{"no cookie", httptest.NewRequest(http.MethodGet, "/me", nil),
			http.StatusNotFound, "Not Found\n", "signed_out", false},
		{"unknown token", withSession(httptest.NewRequest(http.MethodGet, "/me", nil), "NOTATOKENNOTATOKENNOTATOK"),
			http.StatusNotFound, "Not Found\n", "signed_out", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, code := f.serve(tt.req)
			cleared := len(rec.Result().Cookies()) == 1 && rec.Result().Cookies()[0].MaxAge < 0
			if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody || code != tt.wantCode || cleared != tt.wantCleared {
				t.Errorf("GET /me: %d %q, code %q, cookie cleared %v; want %d %q, code %q, cleared %v",
					rec.Code, rec.Body.String(), code, cleared, tt.wantStatus, tt.wantBody, tt.wantCode, tt.wantCleared)
			}
		})
	}
}

// A cookie alone may not change state: CrossOriginProtection passes a
// request with neither Sec-Fetch-Site nor Origin (D-14).
func TestCookieAloneCannotChangeState(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name, header, value string
		wantStatus          int
		wantCode            string
	}{
		{"neither header", "", "", http.StatusForbidden, "cross_origin"},
		{"Sec-Fetch-Site", "Sec-Fetch-Site", "same-origin", http.StatusSeeOther, ""},
		{"Origin", "Origin", "http://example.com", http.StatusSeeOther, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := f.sessions.Start(t.Context(), f.account)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			req := withSession(httptest.NewRequest(http.MethodPost, "/sign-out", nil), token)
			if tt.header != "" {
				req.Header.Set(tt.header, tt.value)
			}
			rec, code := f.serve(req)
			if rec.Code != tt.wantStatus || code != tt.wantCode {
				t.Errorf("POST /sign-out with %s: %d, code %q; want %d, code %q", tt.name, rec.Code, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestSignOutEndsTheSessionAndClearsTheCookie(t *testing.T) {
	f := newFixture(t)
	req := withSession(httptest.NewRequest(http.MethodPost, "/sign-out", nil), f.token)
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	rec, _ := f.serve(req)

	if got, want := rec.Header().Get("Set-Cookie"), "__Host-session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax"; rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" || got != want {
		t.Errorf("POST /sign-out: %d to %q, Set-Cookie %q; want 303 to %q, %q", rec.Code, rec.Header().Get("Location"), got, "/", want)
	}
	if _, err := f.sessions.Resume(t.Context(), f.token); !errors.Is(err, domain.ErrNoSession) {
		t.Errorf("Resume after sign-out: err %v, want ErrNoSession", err)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	web.SetSessionCookie(rec, "tok")
	want := "__Host-session=tok; Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax"
	if got := rec.Header().Get("Set-Cookie"); got != want {
		t.Errorf("Set-Cookie = %q, want %q", got, want)
	}
}
