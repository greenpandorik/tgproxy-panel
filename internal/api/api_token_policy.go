package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// APIRoute is the explicit automation policy and public documentation catalog.
// An endpoint absent from this catalog is unavailable to personal API tokens.
type APIRoute struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Scope     string `json:"scope"`
	Summary   string `json:"summary"`
	OwnerOnly bool   `json:"owner_only"`
}

var managementAPIRoutes = []APIRoute{
	{Method: "GET", Path: "/api/v1/nodes", Scope: "nodes:read", Summary: "List nodes", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}", Scope: "nodes:read", Summary: "Get node", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/probes", Scope: "nodes:read", Summary: "List node probes", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/health", Scope: "nodes:read", Summary: "Get node health", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/reliability", Scope: "nodes:read", Summary: "Get node reliability", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/blocklist", Scope: "nodes:read", Summary: "Get node blocklist", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/profiles", Scope: "nodes:read", Summary: "List node profiles", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/stats", Scope: "nodes:read", Summary: "Get node statistics", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/metrics", Scope: "nodes:read", Summary: "Get node metrics", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/jobs", Scope: "nodes:write", Summary: "List node apply jobs", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/web-policy", Scope: "nodes:read", Summary: "Get node web policy", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/web/carriers", Scope: "nodes:read", Summary: "Get node web carriers", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/telemt-update", Scope: "nodes:write", Summary: "List node update jobs", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/diagnostics", Scope: "nodes:write", Summary: "List node diagnostics", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/fleet/updates", Scope: "nodes:write", Summary: "List fleet updates", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/status/update", Scope: "nodes:read", Summary: "Get panel update status", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/preflight/dns", Scope: "nodes:write", Summary: "Check node DNS", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes", Scope: "nodes:write", Summary: "Create node", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/nodes/order", Scope: "nodes:write", Summary: "Reorder nodes", OwnerOnly: false},
	{Method: "PATCH", Path: "/api/v1/nodes/{id}", Scope: "nodes:write", Summary: "Update node", OwnerOnly: false},
	{Method: "DELETE", Path: "/api/v1/nodes/{id}", Scope: "nodes:write", Summary: "Delete node", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/install-command", Scope: "nodes:write", Summary: "Issue node install command", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/registration-secret", Scope: "nodes:write", Summary: "Get node registration secret", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/logs", Scope: "nodes:write", Summary: "Stream node logs", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/nodes/{id}/reliability", Scope: "nodes:write", Summary: "Update node reliability", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/nodes/{id}/blocklist", Scope: "nodes:write", Summary: "Update node blocklist", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/restart", Scope: "nodes:write", Summary: "Restart node", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/apply", Scope: "nodes:write", Summary: "Apply node configuration", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/web/lifecycle", Scope: "nodes:write", Summary: "Control node web lifecycle", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/nodes/{id}/web-policy", Scope: "nodes:write", Summary: "Update node web policy", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/check", Scope: "nodes:write", Summary: "Check node", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/telemt-update", Scope: "nodes:write", Summary: "Update node telemt", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/diagnostics/web", Scope: "nodes:write", Summary: "Run node web diagnostics", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/fleet/updates", Scope: "nodes:write", Summary: "Start fleet update", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/fleet/updates/{id}/stop", Scope: "nodes:write", Summary: "Stop fleet update", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys", Scope: "users:read", Summary: "List users", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys/summary", Scope: "users:read", Summary: "Get user summary", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys/{id}", Scope: "users:read", Summary: "Get user", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys/{id}/stats", Scope: "users:read", Summary: "Get user statistics", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys", Scope: "users:write", Summary: "Create user", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/batch", Scope: "users:write", Summary: "Create users in batch", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/import", Scope: "users:write", Summary: "Import users", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/bulk", Scope: "users:write", Summary: "Perform bulk user action", OwnerOnly: false},
	{Method: "PATCH", Path: "/api/v1/keys/{id}", Scope: "users:write", Summary: "Update user", OwnerOnly: false},
	{Method: "DELETE", Path: "/api/v1/keys/{id}", Scope: "users:write", Summary: "Delete user", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/{id}/revoke", Scope: "users:write", Summary: "Revoke user", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/{id}/rotate", Scope: "users:write", Summary: "Rotate user secret", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/{id}/disable", Scope: "users:write", Summary: "Disable user", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/{id}/enable", Scope: "users:write", Summary: "Enable user", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys/{id}/links", Scope: "users:write", Summary: "Get user proxy links", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys/{id}/qr", Scope: "users:write", Summary: "Get user proxy QR code", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/{id}/bindings", Scope: "users:write", Summary: "Bind user to node", OwnerOnly: false},
	{Method: "DELETE", Path: "/api/v1/keys/{id}/bindings/{node}", Scope: "users:write", Summary: "Unbind user from node", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/keys/{id}/subscription", Scope: "users:write", Summary: "Issue user subscription", OwnerOnly: false},
	{Method: "DELETE", Path: "/api/v1/keys/{id}/subscription", Scope: "users:write", Summary: "Revoke user subscription", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/keys/{id}/subscription/qr", Scope: "users:write", Summary: "Get user subscription QR code", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/monitoring/overview", Scope: "monitoring:read", Summary: "Get monitoring overview", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/monitoring/nodes/{id}/series", Scope: "monitoring:read", Summary: "Get node monitoring series", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/monitoring/web/carriers", Scope: "monitoring:read", Summary: "Get fleet web carriers", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/dashboard/summary", Scope: "monitoring:read", Summary: "Get dashboard summary", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/dashboard/trends", Scope: "monitoring:read", Summary: "Get dashboard trends", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/alerts", Scope: "monitoring:read", Summary: "List alerts", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/status/public", Scope: "monitoring:read", Summary: "Get public service status", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/alerts/resolve", Scope: "monitoring:write", Summary: "Resolve alerts", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/alerts/{id}/resolve", Scope: "monitoring:write", Summary: "Resolve alert", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/site-templates", Scope: "sites:read", Summary: "List site templates", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/site-templates/{id}", Scope: "sites:read", Summary: "Get site template", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/site", Scope: "sites:read", Summary: "Get node site", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/nodes/{id}/site/preview", Scope: "sites:read", Summary: "Preview node site", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/site-templates/import", Scope: "sites:write", Summary: "Import site template", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/site-templates", Scope: "sites:write", Summary: "Create site template", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/site-templates/validate", Scope: "sites:write", Summary: "Validate site template", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/site-templates/{id}/customize", Scope: "sites:write", Summary: "Customize site template", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/site-templates/{id}", Scope: "sites:write", Summary: "Update site template", OwnerOnly: false},
	{Method: "DELETE", Path: "/api/v1/site-templates/{id}", Scope: "sites:write", Summary: "Delete site template", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/site", Scope: "sites:write", Summary: "Assign node site", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/nodes/{id}/site/upstream", Scope: "sites:write", Summary: "Assign node upstream site", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/branding/profiles", Scope: "branding:read", Summary: "List branding profiles", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/branding/profiles", Scope: "branding:write", Summary: "Create branding profile", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/branding/profiles/{id}", Scope: "branding:write", Summary: "Update branding profile", OwnerOnly: false},
	{Method: "DELETE", Path: "/api/v1/branding/profiles/{id}", Scope: "branding:write", Summary: "Delete branding profile", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/branding/profiles/{id}/activate", Scope: "branding:write", Summary: "Activate branding profile", OwnerOnly: false},
	{Method: "POST", Path: "/api/v1/branding/profiles/{id}/upload", Scope: "branding:write", Summary: "Upload branding asset", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/settings", Scope: "settings:read", Summary: "Get panel settings", OwnerOnly: false},
	{Method: "GET", Path: "/api/v1/settings/subscription-page/preview", Scope: "settings:read", Summary: "Preview subscription page", OwnerOnly: false},
	{Method: "PUT", Path: "/api/v1/settings", Scope: "settings:write", Summary: "Update panel settings", OwnerOnly: true},
	{Method: "POST", Path: "/api/v1/settings/telegram/test", Scope: "settings:write", Summary: "Test Telegram notifications", OwnerOnly: true},
	{Method: "GET", Path: "/api/v1/audit", Scope: "audit:read", Summary: "List audit events", OwnerOnly: false},
}

// ManagementAPIRoutes returns a copy so documentation consumers cannot mutate policy.
func ManagementAPIRoutes() []APIRoute { return slices.Clone(managementAPIRoutes) }

func managementRoute(method, pattern string) (APIRoute, bool) {
	pattern = strings.TrimSuffix(pattern, "/")
	for _, route := range managementAPIRoutes {
		if route.Method == method && route.Path == pattern {
			return route, true
		}
	}
	return APIRoute{}, false
}

func tokenScopeAllowed(role, scope string) bool {
	if role != RoleOwner && role != RoleAdmin && role != RoleViewer {
		return false
	}
	return role != RoleViewer || strings.HasSuffix(scope, ":read")
}

// principalCanWrite controls sensitive response fields as well as route permissions.
func principalCanWrite(r *http.Request, resource string) bool {
	p, ok := PrincipalFrom(r.Context())
	return ok && (p.Role == RoleOwner || p.Role == RoleAdmin) &&
		(p.APITokenID == [16]byte{} || slices.Contains(p.APITokenScopes, resource+":write"))
}

// managementPolicyRouter places policy middleware on each registered endpoint,
// after chi has matched its complete route. Route and Group preserve this property
// for nested routers; With preserves existing role checks.
type managementPolicyRouter struct{ chi.Router }

func enforceAPITokenPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFrom(r.Context())
		if ok && p.APITokenID != uuid.Nil {
			route, allowed := managementRoute(r.Method, chi.RouteContext(r.Context()).RoutePattern())
			if !allowed || !slices.Contains(p.APITokenScopes, route.Scope) || !tokenScopeAllowed(p.Role, route.Scope) || (route.OwnerOnly && p.Role != RoleOwner) {
				forbidden(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (r managementPolicyRouter) With(m ...func(http.Handler) http.Handler) chi.Router {
	return managementPolicyRouter{r.Router.With(m...)}
}

func (r managementPolicyRouter) Route(pattern string, fn func(chi.Router)) chi.Router {
	sub := r.Router.Route(pattern, func(child chi.Router) { fn(managementPolicyRouter{child}) })
	return managementPolicyRouter{sub}
}

func (r managementPolicyRouter) Group(fn func(chi.Router)) chi.Router {
	sub := r.Router.Group(func(child chi.Router) { fn(managementPolicyRouter{child}) })
	return managementPolicyRouter{sub}
}

func (r managementPolicyRouter) Method(method, pattern string, h http.Handler) {
	r.Router.With(enforceAPITokenPolicy).Method(method, pattern, h)
}

func (r managementPolicyRouter) MethodFunc(method, pattern string, h http.HandlerFunc) {
	r.Method(method, pattern, h)
}

func (r managementPolicyRouter) Handle(pattern string, h http.Handler) {
	r.Router.With(enforceAPITokenPolicy).Handle(pattern, h)
}

func (r managementPolicyRouter) HandleFunc(pattern string, h http.HandlerFunc) { r.Handle(pattern, h) }

func (r managementPolicyRouter) Get(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodGet, pattern, h)
}

func (r managementPolicyRouter) Post(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodPost, pattern, h)
}

func (r managementPolicyRouter) Put(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodPut, pattern, h)
}

func (r managementPolicyRouter) Patch(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodPatch, pattern, h)
}

func (r managementPolicyRouter) Delete(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodDelete, pattern, h)
}

func (r managementPolicyRouter) Head(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodHead, pattern, h)
}

func (r managementPolicyRouter) Options(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodOptions, pattern, h)
}

func (r managementPolicyRouter) Connect(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodConnect, pattern, h)
}

func (r managementPolicyRouter) Trace(pattern string, h http.HandlerFunc) {
	r.Method(http.MethodTrace, pattern, h)
}

func (r managementPolicyRouter) Mount(pattern string, h http.Handler) {
	r.Router.Mount(pattern, enforceAPITokenPolicy(h))
}
