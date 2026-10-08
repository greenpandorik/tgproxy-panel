package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestAPITokenPolicyUsesRegisteredEndpoint(t *testing.T) {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), Principal{UserID: uuid.New(), Role: RoleOwner, APITokenID: uuid.New(), APITokenScopes: []string{"nodes:read"}})))
		})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r = managementPolicyRouter{Router: r}
		r.Route("/nodes", func(r chi.Router) {
			r.Get("/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			r.Get("/new-sensitive", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			r.Get("/{id}/new-sensitive", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
		})
	})
	for path, want := range map[string]int{"/api/v1/nodes/" + uuid.NewString(): 204, "/api/v1/nodes/new-sensitive": 403, "/api/v1/nodes/" + uuid.NewString() + "/new-sensitive": 403} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("%s got %d want %d", path, w.Code, want)
		}
	}
}

func TestAPITokenCatalogSensitiveGETs(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range ManagementAPIRoutes() {
		if !strings.HasPrefix(r.Path, "/api/v1/") || strings.HasSuffix(r.Path, "/") || r.Scope == "" {
			t.Fatalf("bad catalog %+v", r)
		}
		key := r.Method + " " + r.Path
		if seen[key] {
			t.Fatal("duplicate catalog", key)
		}
		seen[key] = true
		if r.Method == "GET" && (strings.HasSuffix(r.Path, "/install-command") || strings.HasSuffix(r.Path, "/registration-secret") || strings.HasSuffix(r.Path, "/links") || strings.HasSuffix(r.Path, "/qr") || strings.HasSuffix(r.Path, "/logs")) {
			if !strings.HasSuffix(r.Scope, ":write") {
				t.Errorf("secret endpoint %+v", r)
			}
		}
	}
}
