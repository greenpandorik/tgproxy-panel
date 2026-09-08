package telemt

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recorder captures what the client actually put on the wire so the tests assert the
// contract (path, method, auth header, JSON body) rather than the client's own view.
type recorder struct {
	method, path, auth, body string
}

func newServer(t *testing.T, h http.HandlerFunc) (*Client, *[]recorder) {
	t.Helper()
	var calls []recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, recorder{method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization"), body: string(b)})
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "tok-123")
	c.MetricsURL = srv.URL + "/metrics"
	return c, &calls
}

func ok(w http.ResponseWriter, data string) {
	_, _ = w.Write([]byte(`{"ok":true,"data":` + data + `,"revision":"rev-1"}`))
}

func TestListUsersSendsAuthAndDecodesEnvelope(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		ok(w, `[{"username":"k1","enabled":true,"data_quota_bytes":1024,"current_connections":2,"total_octets":99,"unknown_field":{"x":1}}]`)
	})
	users, err := c.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Username != "k1" || !users[0].Enabled || users[0].DataQuotaBytes != 1024 {
		t.Fatalf("users: %+v", users)
	}
	if users[0].CurrentConnections != 2 || users[0].TotalOctets != 99 {
		t.Fatalf("runtime fields: %+v", users[0])
	}
	got := (*calls)[0]
	if got.method != http.MethodGet || got.path != "/v1/users" || got.auth != "tok-123" {
		t.Fatalf("request: %+v", got)
	}
}

func TestCreateUserBodyAndSecret(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		ok(w, `{"user":{"username":"k1","enabled":true},"secret":"aa11bb22cc33dd44ee55ff6600778899"}`)
	})
	u, err := c.CreateUser(context.Background(), CreateUserRequest{Username: "k1", Secret: "aa11bb22cc33dd44ee55ff6600778899"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "k1" || u.Secret != "aa11bb22cc33dd44ee55ff6600778899" {
		t.Fatalf("user: %+v", u)
	}
	got := (*calls)[0]
	if got.method != http.MethodPost || got.path != "/v1/users" {
		t.Fatalf("request: %+v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatal(err)
	}
	if body["username"] != "k1" || body["secret"] != "aa11bb22cc33dd44ee55ff6600778899" {
		t.Fatalf("body: %s", got.body)
	}
	// Absent optional fields must not be sent at all: telemt treats an explicit null as
	// "remove this override", so an omitempty slip would silently wipe limits.
	if _, present := body["max_tcp_conns"]; present {
		t.Fatalf("unset optional field sent: %s", got.body)
	}
	if _, present := body["enabled"]; present {
		t.Fatalf("unset enabled sent: %s", got.body)
	}
}

func TestPatchUserOmitsUnsetAndNullsCleared(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		ok(w, `{"username":"k1","enabled":true}`)
	})
	secret := "11111111111111111111111111111111"
	quota := uint64(500)
	if _, err := c.PatchUser(context.Background(), "k1", PatchUserRequest{
		Secret: &secret, DataQuotaBytes: &quota, Clear: []string{"max_unique_ips"},
	}); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.method != http.MethodPatch || got.path != "/v1/users/k1" {
		t.Fatalf("request: %+v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatal(err)
	}
	if body["secret"] != secret || body["data_quota_bytes"].(float64) != 500 {
		t.Fatalf("body: %s", got.body)
	}
	v, present := body["max_unique_ips"]
	if !present || v != nil {
		t.Fatalf("cleared field must be an explicit null: %s", got.body)
	}
	if _, present := body["rate_limit_up_bps"]; present {
		t.Fatalf("unset field sent: %s", got.body)
	}
}

func TestUserMutationsUseTheRightRoutes(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/rotate-secret"):
			ok(w, `{"user":{"username":"k1","enabled":true},"secret":"22222222222222222222222222222222"}`)
		case r.Method == http.MethodDelete:
			ok(w, `{"username":"k1","in_runtime":false}`)
		default:
			ok(w, `{"username":"k1","enabled":true}`)
		}
	})
	ctx := context.Background()
	u, err := c.RotateSecret(ctx, "k1", "22222222222222222222222222222222")
	if err != nil || u.Secret != "22222222222222222222222222222222" {
		t.Fatalf("rotate: %+v %v", u, err)
	}
	if _, err := c.Enable(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Disable(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteUser(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /v1/users/k1/rotate-secret",
		"POST /v1/users/k1/enable",
		"POST /v1/users/k1/disable",
		"DELETE /v1/users/k1",
	}
	for i, w := range want {
		got := (*calls)[i].method + " " + (*calls)[i].path
		if got != w {
			t.Fatalf("call %d: got %q want %q", i, got, w)
		}
	}
}

func TestUsernameIsPathEscaped(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) { ok(w, `{"username":"a b","enabled":true}`) })
	if _, err := c.Enable(context.Background(), "a b"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v1/users/a%20b/enable" {
		t.Fatalf("path: %q", (*calls)[0].path)
	}
}

func TestGetAndPatchConfig(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			ok(w, `{"web":{"enabled":true,"vhosts":[{"host":"n.test","profiles":[{"user":"k1","secret_mode":"plain"}]}]}}`)
			return
		}
		ok(w, `{"revision":"rev-2","changed":["web"],"runtime_reload_required":true,"deferred_process_fields":[]}`)
	})
	ctx := context.Background()
	cfg, rev, err := c.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rev != "rev-1" {
		t.Fatalf("revision: %q", rev)
	}
	web, _ := cfg["web"].(map[string]any)
	if web == nil || web["enabled"] != true {
		t.Fatalf("config: %+v", cfg)
	}
	res, err := c.PatchConfig(ctx, map[string]any{"web": map[string]any{"enabled": true}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision != "rev-2" || !res.RuntimeReloadRequired || len(res.Changed) != 1 {
		t.Fatalf("patch result: %+v", res)
	}
	if (*calls)[1].method != http.MethodPatch || (*calls)[1].path != "/v1/config?reload=drain&timeout_secs=30" {
		t.Fatalf("patch request: %+v", (*calls)[1])
	}
	if (*calls)[1].body != `{"web":{"enabled":true}}` {
		t.Fatalf("patch body: %s", (*calls)[1].body)
	}
}

func TestPatchConfigWithoutReload(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) { ok(w, `{"revision":"rev-2"}`) })
	if _, err := c.PatchConfig(context.Background(), map[string]any{"web": map[string]any{}}, false); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v1/config" {
		t.Fatalf("path: %q", (*calls)[0].path)
	}
}

func TestHealthReadySystemInfoAndSummary(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			ok(w, `{"status":"ok","read_only":false}`)
		case "/v1/health/ready":
			// telemt answers 503 with a *success* envelope when it is simply not ready yet.
			w.WriteHeader(http.StatusServiceUnavailable)
			ok(w, `{"ready":false,"status":"not_ready","reason":"no_healthy_upstreams","admission_open":true}`)
		case "/v1/system/info":
			ok(w, `{"version":"3.5.5","config_hash":"abc","uptime_seconds":12.5}`)
		case "/v1/runtime/connections/summary":
			ok(w, `{"enabled":true,"data":{"totals":{"current_connections":7,"active_users":2},"top":{"limit":10,"by_connections":[{"username":"k1","current_connections":5,"total_octets":42}]}}}`)
		}
	})
	ctx := context.Background()
	if h, err := c.Health(ctx); err != nil || h.Status != "ok" {
		t.Fatalf("health: %+v %v", h, err)
	}
	rd, err := c.Ready(ctx)
	if err != nil {
		t.Fatalf("ready must not error on a 503 success envelope: %v", err)
	}
	if rd.Ready || rd.Reason != "no_healthy_upstreams" {
		t.Fatalf("ready: %+v", rd)
	}
	info, err := c.SystemInfo(ctx)
	if err != nil || info.Version != "3.5.5" || info.ConfigHash != "abc" {
		t.Fatalf("info: %+v %v", info, err)
	}
	sum, err := c.ConnectionsSummary(ctx)
	if err != nil || sum.Data == nil {
		t.Fatalf("summary: %+v %v", sum, err)
	}
	if sum.Data.Totals.CurrentConnections != 7 || len(sum.Data.Top.ByConnections) != 1 {
		t.Fatalf("summary payload: %+v", sum.Data)
	}
}

func TestReloadAndStatus(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			ok(w, `{"reload_id":7,"state":"accepted","mode":"drain"}`)
			return
		}
		ok(w, `{"reload_id":7,"state":"succeeded","deferred_process_fields":["web.limits"]}`)
	})
	ctx := context.Background()
	acc, err := c.Reload(ctx, ReloadRequest{Mode: "drain", TimeoutSecs: 30})
	if err != nil || acc.ReloadID != 7 {
		t.Fatalf("reload: %+v %v", acc, err)
	}
	if (*calls)[0].path != "/v1/system/reload" || (*calls)[0].body != `{"mode":"drain","timeout_secs":30}` {
		t.Fatalf("reload request: %+v", (*calls)[0])
	}
	st, err := c.ReloadStatus(ctx, 7)
	if err != nil || st.State != "succeeded" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if (*calls)[1].path != "/v1/system/reload/7" {
		t.Fatalf("status path: %q", (*calls)[1].path)
	}
}

func TestErrorEnvelopeBecomesAPIError(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"user_exists","message":"user already exists"},"request_id":3}`))
	})
	_, err := c.CreateUser(context.Background(), CreateUserRequest{Username: "k1"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Status != http.StatusConflict || apiErr.Code != "user_exists" {
		t.Fatalf("api error: %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "user_exists") || !strings.Contains(apiErr.Error(), "409") {
		t.Fatalf("message: %q", apiErr.Error())
	}
}

func TestNonEnvelopeErrorStillMapsToAPIError(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("nope"))
	})
	_, err := c.ListUsers(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("want 401 APIError, got %v", err)
	}
}

func TestMetricsReturnsRawTextWithoutTheAPIToken(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("telemt_connections_total 5\n"))
	})
	text, err := c.Metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "telemt_connections_total 5") {
		t.Fatalf("metrics: %q", text)
	}
	// The metrics listener is a separate, whitelist-guarded port: the control API token
	// must never be sent there.
	if (*calls)[0].auth != "" {
		t.Fatalf("auth header leaked to metrics: %q", (*calls)[0].auth)
	}
}

func TestAPIErrorNeverEchoesTheToken(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"internal_error","message":"boom"}}`))
	})
	_, err := c.ListUsers(context.Background())
	if err == nil || strings.Contains(err.Error(), "tok-123") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

// upstreamsBody is the shape a production telemt 3.5.7 node answers on GET /v1/stats/upstreams,
// trimmed to the fields the panel reads plus a few it must ignore. DC 4 has no EMA yet (null).
const upstreamsBody = `{
  "enabled": true,
  "zero": {"connect_success_total": 58, "connect_fail_total": 0, "unrelated": 1},
  "upstreams": [{
    "route_kind": "direct", "healthy": true, "fails": 0, "last_check_age_secs": 29,
    "effective_latency_ms": 41.25, "weight": 1,
    "dc": [
      {"dc": 1, "latency_ema_ms": 197.9, "ip_preference": "prefer_v4"},
      {"dc": 2, "latency_ema_ms": 36.5, "ip_preference": "prefer_v4"},
      {"dc": 4, "latency_ema_ms": null, "ip_preference": "prefer_v6"}
    ]
  }]
}`

func TestUpstreamsStatsDecodesDcsAndKeepsNullLatencyUnknown(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stats/upstreams" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ok(w, upstreamsBody)
	})
	st, err := c.UpstreamsStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].method != http.MethodGet || (*calls)[0].auth != "tok-123" {
		t.Fatalf("call: %+v", (*calls)[0])
	}
	if !st.Enabled || st.Zero.ConnectSuccessTotal != 58 || st.Zero.ConnectFailTotal != 0 {
		t.Fatalf("top level: %+v", st)
	}
	if len(st.Upstreams) != 1 {
		t.Fatalf("upstreams: %+v", st.Upstreams)
	}
	u := st.Upstreams[0]
	if u.RouteKind != "direct" || !u.Healthy || u.Fails != 0 || u.LastCheckAgeSecs != 29 || u.EffectiveLatencyMs != 41.25 {
		t.Fatalf("upstream: %+v", u)
	}
	if len(u.DC) != 3 || u.DC[0].DC != 1 || u.DC[0].IPPreference != "prefer_v4" {
		t.Fatalf("dc list: %+v", u.DC)
	}
	if u.DC[0].LatencyEmaMs == nil || *u.DC[0].LatencyEmaMs != 197.9 || u.DC[1].LatencyEmaMs == nil || *u.DC[1].LatencyEmaMs != 36.5 {
		t.Fatalf("known latencies: %+v %+v", u.DC[0], u.DC[1])
	}
	// A null EMA is "not measured yet" and must stay distinguishable from a measured 0 ms.
	if u.DC[2].LatencyEmaMs != nil {
		t.Fatalf("null latency_ema_ms must decode as nil, got %v", *u.DC[2].LatencyEmaMs)
	}
	if u.DC[2].DC != 4 || u.DC[2].IPPreference != "prefer_v6" {
		t.Fatalf("dc 4: %+v", u.DC[2])
	}
}

func TestUpstreamsStatsDisabled(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		ok(w, `{"enabled":false,"upstreams":[]}`)
	})
	st, err := c.UpstreamsStats(context.Background())
	if err != nil || st.Enabled || len(st.Upstreams) != 0 {
		t.Fatalf("disabled: %+v %v", st, err)
	}
}
