package api

import "net/http"

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				unauthorized(w)
				return
			}
			if !allowed[p.Role] {
				forbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writers is the role set allowed to mutate resources.
var writers = []string{RoleOwner, RoleAdmin}
