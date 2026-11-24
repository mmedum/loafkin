// Package web serves loafkin's pages.
package web

import (
	"context"
	"errors"
	"net/http"

	"loafkin.com/internal/domain"
	"loafkin.com/internal/route"
	"loafkin.com/internal/store"
)

// sessionCookie is the __Host- cookie of D-13: Secure, host-only and
// for the whole site.
const sessionCookie = "__Host-session"

type memberKey struct{}

// MemberFrom returns the member that RequireMember signed in.
func MemberFrom(r *http.Request) domain.Member {
	m, _ := r.Context().Value(memberKey{}).(domain.Member)
	return m
}

// SetSessionCookie gives the browser the token of its new session.
func SetSessionCookie(w http.ResponseWriter, token string) {
	setSessionCookie(w, token, int(domain.SessionLifetime.Seconds()))
}

func clearSessionCookie(w http.ResponseWriter) {
	setSessionCookie(w, "", -1)
}

// setSessionCookie sets or clears the cookie. A browser clears it only
// when the attributes match the ones it was set with.
func setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// RequireMember is the guard of route.Member. A request without a live
// session answers 404, as a page the caller may not see does (D-33).
func RequireMember(sessions *domain.Sessions) route.Guard {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookie)
			if err != nil {
				route.Fail(w, r, http.StatusNotFound, "signed_out")
				return
			}
			// CrossOriginProtection lets through a request that carries
			// neither header, so a cookie alone may not change state (D-14).
			if !safeMethod(r.Method) && r.Header.Get("Sec-Fetch-Site") == "" && r.Header.Get("Origin") == "" {
				route.Fail(w, r, http.StatusForbidden, "cross_origin")
				return
			}
			m, err := sessions.Resume(r.Context(), cookie.Value)
			if errors.Is(err, domain.ErrNoSession) {
				clearSessionCookie(w)
				route.Fail(w, r, http.StatusNotFound, "signed_out")
				return
			}
			if err != nil {
				route.Fail(w, r, http.StatusInternalServerError, store.ErrorCode(err, "session_failed"))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), memberKey{}, m)))
		})
	}
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// Register adds the page routes to t.
func Register(t *route.Table, sessions *domain.Sessions) {
	t.HandleFunc("POST /sign-out", route.Member, func(w http.ResponseWriter, r *http.Request) {
		if err := sessions.End(r.Context(), MemberFrom(r)); err != nil {
			route.Fail(w, r, http.StatusInternalServerError, store.ErrorCode(err, "sign_out_failed"))
			return
		}
		clearSessionCookie(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}
