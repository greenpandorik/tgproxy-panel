package api

import (
	"crypto/subtle"
	"net/http"
)

// csrfCheck enforces double-submit: cookie value must equal X-CSRF-Token header on unsafe methods.
func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(csrfCookie)
		h := r.Header.Get("X-CSRF-Token")
		if err != nil || h == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) != 1 {
			writeError(w, http.StatusForbidden, "csrf", "missing or invalid CSRF token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
