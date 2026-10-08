package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/store/db"
)

type tokenMeta struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}
type tokenIssued struct {
	Token    string    `json:"token"`
	APIToken tokenMeta `json:"api_token"`
}

func issueToken(t *testing.T, c *apitest.Client, scopes ...string) tokenIssued {
	t.Helper()
	resp := c.Post("/api/v1/api-tokens", map[string]any{"name": "  Service Bot  ", "expires_in_days": 30, "scopes": scopes})
	if resp.StatusCode != 201 {
		code, status := errCodeOf(t, resp)
		t.Fatalf("create token: %d/%s want 201", status, code)
	}
	var out tokenIssued
	c.JSON(resp, &out)
	return out
}

func tokenStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if got := statusOf(t, resp); got != want {
		t.Fatalf("status %d want %d", got, want)
	}
}

func TestAPITokenOneTimeSecretAndScopeAuth(t *testing.T) {
	h := apitest.New(t)
	ownerID := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	resp := c.Post("/api/v1/api-tokens", map[string]any{"name": "  Service Bot  ", "expires_in_days": 30, "scopes": []string{"nodes:read", "nodes:read"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create token: got %d want 201", statusOf(t, resp))
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("secret response must be no-store")
	}
	var issued tokenIssued
	c.JSON(resp, &issued)
	if !strings.HasPrefix(issued.Token, "tgwp_") || len(issued.Token) < 45 || issued.APIToken.Name != "Service Bot" || len(issued.APIToken.Scopes) != 1 || issued.APIToken.LastUsedAt != nil {
		t.Fatalf("invalid creation %+v", issued)
	}
	var owner uuid.UUID
	var digest []byte
	var raw string
	if err := h.Store.Pool.QueryRow(context.Background(), "SELECT admin_user_id, token_hash, row_to_json(t)::text FROM api_tokens t WHERE id=$1", issued.APIToken.ID).Scan(&owner, &digest, &raw); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(issued.Token))
	if owner != ownerID || string(digest) != string(hash[:]) || strings.Contains(raw, issued.Token) {
		t.Fatal("secret persisted or wrong digest")
	}
	var list struct {
		Items []tokenMeta `json:"items"`
		Total int         `json:"total"`
	}
	r := c.Get("/api/v1/api-tokens")
	var body map[string]json.RawMessage
	c.JSON(r, &body)
	b, _ := json.Marshal(body)
	if strings.Contains(string(b), issued.Token) || strings.Contains(string(b), "token_hash") {
		t.Fatal("listing leaks credentials")
	}
	_ = json.Unmarshal(b, &list)
	if len(list.Items) != 1 || list.Total != 1 {
		t.Fatalf("list %+v", list)
	}
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+issued.Token)
	bearer.SendCSRF = false
	tokenStatus(t, bearer.Get("/api/v1/nodes"), 200)
	tokenStatus(t, bearer.Get("/api/v1/keys"), 403)
	tokenStatus(t, bearer.Post("/api/v1/nodes", map[string]any{}), 403)
	var last time.Time
	if err := h.Store.Pool.QueryRow(context.Background(), "SELECT last_used_at FROM api_tokens WHERE id=$1", issued.APIToken.ID).Scan(&last); err != nil {
		t.Fatal(err)
	}
	tokenStatus(t, bearer.Get("/api/v1/nodes"), 200)
	var again time.Time
	_ = h.Store.Pool.QueryRow(context.Background(), "SELECT last_used_at FROM api_tokens WHERE id=$1", issued.APIToken.ID).Scan(&again)
	if !again.Equal(last) {
		t.Fatal("last use not throttled")
	}
}

func TestAPITokenCredentialsNeverFallBackToCookies(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	issued := issueToken(t, c, "nodes:read")
	for _, headers := range [][]string{{"Bearer invalid"}, {"Bearer"}, {"Basic a"}, {""}, {"Bearer " + issued.Token, "Bearer " + issued.Token}, {"Bearer " + issued.Token + ", Bearer " + issued.Token}} {
		c.Headers["Authorization"] = headers
		c.SendCSRF = false
		tokenStatus(t, c.Get("/api/v1/nodes"), 401)
		tokenStatus(t, c.Post("/api/v1/nodes", map[string]any{}), 401)
	}
	c.Headers.Del("Authorization")
	tokenStatus(t, c.Post("/api/v1/api-tokens", map[string]any{}), 403)
	c.SetHeader("Authorization", "Bearer "+issued.Token)
	tokenStatus(t, c.Get("/api/v1/api-tokens"), 403)
	tokenStatus(t, c.Get("/api/v1/auth/me"), 403)
}

func TestAPITokenRoleExpiryRevocationAndDeletion(t *testing.T) {
	h := apitest.New(t)
	id := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	issued := issueToken(t, c, "nodes:read", "nodes:write")
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+issued.Token)
	bearer.SendCSRF = false
	// Valid scoped mutation reaches validation without browser CSRF.
	tokenStatus(t, bearer.Post("/api/v1/nodes", map[string]any{}), 422)
	if err := h.Store.Q.UpdateAdminRole(context.Background(), db.UpdateAdminRoleParams{ID: id, Role: db.AdminRoleViewer}); err != nil {
		t.Fatal(err)
	}
	tokenStatus(t, bearer.Post("/api/v1/nodes", map[string]any{}), 403)
	tokenStatus(t, bearer.Get("/api/v1/nodes"), 200)
	tokenStatus(t, c.Delete("/api/v1/api-tokens/"+issued.APIToken.ID.String()), 204)
	tokenStatus(t, c.Delete("/api/v1/api-tokens/"+issued.APIToken.ID.String()), 204)
	tokenStatus(t, bearer.Get("/api/v1/nodes"), 401)
	expired := issueToken(t, c, "nodes:read")
	_, err := h.Store.Pool.Exec(context.Background(), "UPDATE api_tokens SET expires_at=now()-interval '1 second' WHERE id=$1", expired.APIToken.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenStatus(t, h.Anonymous().SetHeader("Authorization", "Bearer "+expired.Token).Get("/api/v1/nodes"), 401)
	deleted := issueToken(t, c, "nodes:read")
	if err := h.Store.Q.DeleteAdmin(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	tokenStatus(t, h.Anonymous().SetHeader("Authorization", "Bearer "+deleted.Token).Get("/api/v1/nodes"), 401)
}

func TestAPITokenOwnershipViewerScopesAndValidation(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	h.CreateAdmin("view", "pass-123456", "viewer")
	owner := h.Login("root", "pass-123456")
	viewer := h.Login("view", "pass-123456")
	issued := issueToken(t, owner, "nodes:read")
	tokenStatus(t, viewer.Delete("/api/v1/api-tokens/"+issued.APIToken.ID.String()), 404)
	var list struct {
		Items []tokenMeta `json:"items"`
	}
	viewer.JSON(viewer.Get("/api/v1/api-tokens"), &list)
	if len(list.Items) != 0 {
		t.Fatal("cross-account listing")
	}
	var scopes struct {
		Scopes []struct {
			ID string `json:"id"`
		} `json:"scopes"`
		MaxDays   int `json:"max_expires_in_days"`
		MaxTokens int `json:"max_tokens"`
	}
	viewer.JSON(viewer.Get("/api/v1/api-tokens/scopes"), &scopes)
	if scopes.MaxDays != 365 || scopes.MaxTokens != 50 || len(scopes.Scopes) == 0 {
		t.Fatal("bad scope contract")
	}
	for _, s := range scopes.Scopes {
		if strings.HasSuffix(s.ID, ":write") {
			t.Fatal("viewer offered write scope")
		}
	}
	for _, body := range []map[string]any{{"name": "", "expires_in_days": 30, "scopes": []string{"nodes:read"}}, {"name": strings.Repeat("я", 81), "expires_in_days": 30, "scopes": []string{"nodes:read"}}, {"name": "x", "expires_in_days": 0, "scopes": []string{"nodes:read"}}, {"name": "x", "expires_in_days": 366, "scopes": []string{"nodes:read"}}, {"name": "x", "expires_in_days": 30, "scopes": []string{}}, {"name": "x", "expires_in_days": 30, "scopes": []string{"unknown:read"}}} {
		tokenStatus(t, owner.Post("/api/v1/api-tokens", body), 422)
	}
	tokenStatus(t, owner.Post("/api/v1/api-tokens", map[string]any{"name": "x", "expires_in_days": 1.5, "scopes": []string{"nodes:read"}}), 400)
	tokenStatus(t, viewer.Post("/api/v1/api-tokens", map[string]any{"name": "x", "expires_in_days": 30, "scopes": []string{"nodes:write"}}), 422)
	_ = issueToken(t, viewer, "nodes:read")
}

func TestAPITokenPasswordRotationAndAudit(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	issued := issueToken(t, c, "sites:write")
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+issued.Token)
	bearer.SendCSRF = false
	tokenStatus(t, bearer.Post("/api/v1/site-templates", map[string]any{"name": "Token site", "html": "<html><body>Hello</body></html>"}), 201)
	var meta string
	if err := h.Store.Pool.QueryRow(context.Background(), "SELECT meta::text FROM audit_log WHERE action='site_template.create'").Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta, issued.APIToken.ID.String()) || strings.Contains(meta, issued.Token) {
		t.Fatal("missing token attribution or secret leak")
	}
	tokenStatus(t, c.Post("/api/v1/me/password", map[string]string{"current": "pass-123456", "new": "pass-654321"}), 204)
	tokenStatus(t, bearer.Get("/api/v1/site-templates"), 401)
}

func TestAPITokenConcurrentLimit(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	seed := issueToken(t, c, "nodes:read")
	_, err := h.Store.Pool.Exec(context.Background(), "INSERT INTO api_tokens (admin_user_id,name,prefix,token_hash,scopes,expires_at) SELECT admin_user_id,'bulk','tgwp_test',digest(i::text,'sha256'),scopes,expires_at FROM api_tokens CROSS JOIN generate_series(1,48) i WHERE id=$1", seed.APIToken.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := c.Post("/api/v1/api-tokens", map[string]any{"name": "Concurrent", "expires_in_days": 30, "scopes": []string{"nodes:read"}})
			statuses <- statusOf(t, resp)
		}()
	}
	wg.Wait()
	close(statuses)
	created := 0
	for code := range statuses {
		if code == 201 {
			created++
		} else if code != 409 {
			t.Errorf("limit status %d", code)
		}
	}
	if created != 1 {
		t.Fatalf("created %d want 1", created)
	}
}

func TestAPITokenEveryManagementRouteDeniesInsufficientScope(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	issued := issueToken(t, c, "audit:read")
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+issued.Token)
	bearer.SendCSRF = false
	repl := strings.NewReplacer("{id}", uuid.New().String(), "{node}", uuid.New().String(), "{token}", "x", "{file}", "x", "{platform}", "linux-amd64")
	// Explicit public/dedicated authentication endpoints are exercised by existing RBAC tests.
	err := chi.Walk(h.Router(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if !strings.HasPrefix(route, "/api/v1/") || publicMutations[key] || publicReads[key] || nodeTokenReads[key] || route == "/api/v1/audit" {
			return nil
		}
		tokenStatus(t, bearer.Do(method, repl.Replace(route), map[string]any{}), 403)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPITokenReadResponsesHideSecretsAndWriteGETs(t *testing.T) {
	h, c, n := ownerWithNode(t)
	var created struct {
		ID     uuid.UUID `json:"id"`
		Secret string    `json:"secret"`
	}
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "Scoped user", "type": "SHARED", "sub_slug": "scoped-subscription-secret", "node_ids": []uuid.UUID{n.ID}}), &created)
	if created.ID == uuid.Nil || created.Secret == "" {
		t.Fatal("key fixture missing")
	}
	tokenStatus(t, c.Post("/api/v1/keys/"+created.ID.String()+"/subscription", map[string]any{}), 200)
	_, err := h.Store.Pool.Exec(context.Background(), "UPDATE nodes SET last_check=$2 WHERE id=$1", n.ID, []byte(`{"results":[{"name":"handshake","status":"fail","detail":"sensitive node probe credential"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	read := issueToken(t, c, "users:read", "nodes:read")
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+read.Token)
	for _, path := range []string{"/api/v1/keys", "/api/v1/keys/" + created.ID.String(), "/api/v1/nodes", "/api/v1/nodes/" + n.ID.String()} {
		resp := bearer.Get(path)
		var payload json.RawMessage
		bearer.JSON(resp, &payload)
		if resp.StatusCode != 200 {
			t.Fatalf("read %s %d", path, resp.StatusCode)
		}
		raw := string(payload)
		if strings.Contains(raw, created.Secret) || strings.Contains(raw, "sensitive node probe credential") || strings.Contains(raw, "/s/") || strings.Contains(raw, "scoped-subscription-secret") {
			t.Fatalf("read leak %s: %s", path, raw)
		}
	}
	for _, path := range []string{"/api/v1/nodes/" + n.ID.String() + "/install-command", "/api/v1/nodes/" + n.ID.String() + "/registration-secret", "/api/v1/nodes/" + n.ID.String() + "/jobs", "/api/v1/nodes/" + n.ID.String() + "/diagnostics", "/api/v1/nodes/" + n.ID.String() + "/telemt-update", "/api/v1/fleet/updates", "/api/v1/nodes/" + n.ID.String() + "/logs", "/api/v1/keys/" + created.ID.String() + "/links", "/api/v1/keys/" + created.ID.String() + "/qr", "/api/v1/keys/" + created.ID.String() + "/subscription/qr"} {
		tokenStatus(t, bearer.Get(path), 403)
	}
	write := issueToken(t, c, "users:read", "users:write", "nodes:write")
	writer := h.Anonymous().SetHeader("Authorization", "Bearer "+write.Token)
	var result map[string]any
	writer.JSON(writer.Get("/api/v1/keys/"+created.ID.String()), &result)
	if result["secret"] != created.Secret {
		t.Fatal("write scope must reveal user credential")
	}
	tokenStatus(t, writer.Get("/api/v1/nodes/"+n.ID.String()+"/registration-secret"), 200)
}

func TestAPITokenFullScopesStillDenySessionOnlyRoutes(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	full := issueToken(t, c, "nodes:read", "nodes:write", "users:read", "users:write", "monitoring:read", "monitoring:write", "sites:read", "sites:write", "branding:read", "branding:write", "settings:read", "settings:write", "audit:read")
	bearer := h.Anonymous().SetHeader("Authorization", "Bearer "+full.Token)
	bearer.SendCSRF = false
	for _, tc := range []struct{ method, path string }{{"GET", "/api/v1/api-tokens"}, {"GET", "/api/v1/api-tokens/scopes"}, {"POST", "/api/v1/api-tokens"}, {"DELETE", "/api/v1/api-tokens/" + uuid.NewString()}, {"POST", "/api/v1/me/password"}, {"POST", "/api/v1/auth/totp/setup"}, {"POST", "/api/v1/auth/totp/confirm"}, {"POST", "/api/v1/auth/totp/disable"}, {"GET", "/api/v1/admins"}, {"POST", "/api/v1/admins"}, {"DELETE", "/api/v1/admins/" + uuid.NewString()}, {"GET", "/api/v1/backups"}, {"POST", "/api/v1/backups"}, {"GET", "/api/v1/subscription-service"}, {"PUT", "/api/v1/subscription-service"}, {"POST", "/api/v1/subscription-service/token"}, {"DELETE", "/api/v1/subscription-service/token"}} {
		tokenStatus(t, bearer.Do(tc.method, tc.path, map[string]any{}), 403)
	}
}

func TestAPITokenScopesRespectOwnerOnlySettings(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("admin", "pass-123456", "admin")
	c := h.Login("admin", "pass-123456")
	var out struct {
		Scopes []struct {
			ID string `json:"id"`
		} `json:"scopes"`
	}
	c.JSON(c.Get("/api/v1/api-tokens/scopes"), &out)
	for _, scope := range out.Scopes {
		if scope.ID == "settings:write" {
			t.Fatal("admin offered unusable owner-only scope")
		}
	}
	tokenStatus(t, c.Post("/api/v1/api-tokens", map[string]any{"name": "Settings", "expires_in_days": 30, "scopes": []string{"settings:write"}}), 422)
}

// waitForAPITokenBlockedQuery confirms the HTTP request has authenticated and is
// waiting at the account lock, rather than relying on a scheduler-dependent delay.
func waitForAPITokenBlockedQuery(t *testing.T, h *apitest.Harness, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := h.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1", "%"+needle+"%").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not observe blocked query %s", needle)
}

func TestAPITokenPasswordRotationRejectsQueuedOldSessionIssuance(t *testing.T) {
	h := apitest.New(t)
	owner := h.CreateAdmin("root", "pass-123456", "owner")
	passwordClient := h.Login("root", "pass-123456")
	createClient := h.Login("root", "pass-123456")
	old := issueToken(t, createClient, "nodes:read")
	ctx := context.Background()
	lock, err := h.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err := lock.Exec(ctx, "SELECT id FROM admin_users WHERE id=$1 FOR UPDATE", owner); err != nil {
		t.Fatal(err)
	}
	passwordDone := make(chan int, 1)
	go func() {
		passwordDone <- statusOf(t, passwordClient.Post("/api/v1/me/password", map[string]string{"current": "pass-123456", "new": "pass-654321"}))
	}()
	waitForAPITokenBlockedQuery(t, h, "UPDATE admin_users SET password_hash")
	createDone := make(chan int, 1)
	go func() {
		createDone <- statusOf(t, createClient.Post("/api/v1/api-tokens", map[string]any{"name": "Queued token", "expires_in_days": 30, "scopes": []string{"nodes:read"}}))
	}()
	waitForAPITokenBlockedQuery(t, h, "LockAPITokenOwner")
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if status := <-passwordDone; status != 204 {
		t.Fatalf("password status %d want204", status)
	}
	if status := <-createDone; status != 401 {
		t.Fatalf("pre-rotation session issued a token after rotation: status %d want401", status)
	}
	tokenStatus(t, h.Anonymous().SetHeader("Authorization", "Bearer "+old.Token).Get("/api/v1/nodes"), 401)
	var count int
	if err := h.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM api_tokens WHERE admin_user_id=$1 AND revoked_at IS NULL", owner).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d tokens survived password rotation", count)
	}
}

func TestAPITokenQueuedIssuanceUsesCurrentAccountRole(t *testing.T) {
	h := apitest.New(t)
	owner := h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	ctx := context.Background()
	lock, err := h.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err := lock.Exec(ctx, "UPDATE admin_users SET role='viewer' WHERE id=$1", owner); err != nil {
		t.Fatal(err)
	}
	createDone := make(chan int, 1)
	go func() {
		createDone <- statusOf(t, c.Post("/api/v1/api-tokens", map[string]any{"name": "Queued writer token", "expires_in_days": 30, "scopes": []string{"nodes:write"}}))
	}()
	waitForAPITokenBlockedQuery(t, h, "LockAPITokenOwner")
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if status := <-createDone; status != 422 {
		t.Fatalf("scope validation used stale account role: status %d want422", status)
	}
}

func TestAPITokenReadSearchDoesNotMatchHiddenSubscriptionSlug(t *testing.T) {
	h, c, n := ownerWithNode(t)
	const fixtureSlug = "synthetic-private-subscription"
	var created struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "Visible user", "type": "SHARED", "sub_slug": fixtureSlug, "node_ids": []uuid.UUID{n.ID}}), &created)
	if created.ID == uuid.Nil {
		t.Fatal("missing key fixture")
	}
	read := issueToken(t, c, "users:read")
	write := issueToken(t, c, "users:read", "users:write")
	for _, tc := range []struct {
		name   string
		client *apitest.Client
		query  string
		want   int
	}{{"read hidden slug", h.Anonymous().SetHeader("Authorization", "Bearer "+read.Token), fixtureSlug, 0}, {"read visible label", h.Anonymous().SetHeader("Authorization", "Bearer "+read.Token), "Visible%20user", 1}, {"write hidden slug", h.Anonymous().SetHeader("Authorization", "Bearer "+write.Token), fixtureSlug, 1}, {"session hidden slug", c, fixtureSlug, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			var result struct {
				Items []struct {
					ID uuid.UUID `json:"id"`
				} `json:"items"`
				Total int `json:"total"`
			}
			resp := tc.client.Get("/api/v1/keys?q=" + tc.query)
			tc.client.JSON(resp, &result)
			if resp.StatusCode != 200 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if len(result.Items) != tc.want || result.Total != tc.want {
				t.Fatalf("list/count exposed hidden-only match: %d items, total%d want%d", len(result.Items), result.Total, tc.want)
			}
		})
	}
}
