package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type fakeTelemt struct {
	mu           sync.Mutex
	users        map[string]string // username -> secret
	attrs        map[string]map[string]any
	userWrites   map[string][]string // username -> raw bodies of POST/PATCH for that user
	config       map[string]any
	writes       []string // "METHOD path" of every mutating call, in order
	patches      []string // raw bodies of PATCH /v1/config
	reloads      int
	reloadStates []string
	ready        bool
	failOn       string // "METHOD path" prefix that must answer 500
	upstreams    map[string]any
	authSeen     map[string]bool
	srv          *httptest.Server
}

func newFakeTelemt(t *testing.T, decoyDir string) *fakeTelemt {
	t.Helper()
	f := &fakeTelemt{
		users:      map[string]string{"node": "00000000000000000000000000000000"},
		attrs:      map[string]map[string]any{"node": {}},
		userWrites: map[string][]string{},
		config: map[string]any{
			"general": map[string]any{
				"log_level": "normal",
				"links":     map[string]any{"public_host": "n1.example.com", "public_port": float64(8443)},
			},
			"censorship": map[string]any{"tls_domain": "n1.example.com", "mask": true, "unknown_sni_action": "mask"},
			"server": map[string]any{"listeners": []any{
				map[string]any{"ip": "0.0.0.0", "port": float64(8443), "synlimit": "nftables"},
				map[string]any{"ip": "127.0.0.1", "port": float64(18080), "transport": "web"},
			}},
			"web": map[string]any{
				"enabled": true,
				"carrier": "https",
				"vhosts": []any{map[string]any{
					"host":        "n1.example.com",
					"public_addr": "203.0.113.7:443",
					"decoy":       map[string]any{"mode": "static_directory", "directory": decoyDir, "index": "index.html"},
					"profiles":    []any{map[string]any{"user": "node", "secret_mode": "plain"}},
				}},
			},
		},
		reloadStates: []string{"succeeded"},
		ready:        true,
		authSeen:     map[string]bool{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTelemt) ok(w http.ResponseWriter, data any) {
	raw, _ := json.Marshal(data)
	_, _ = w.Write([]byte(`{"ok":true,"data":` + string(raw) + `,"revision":"rev"}`))
}

func (f *fakeTelemt) fail(w http.ResponseWriter, status int, code, msg string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"` + code + `","message":"` + msg + `"}}`))
}

func (f *fakeTelemt) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	route := r.Method + " " + r.URL.Path
	f.authSeen[r.Header.Get("Authorization")] = true
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, route)
	}
	if f.failOn != "" && strings.HasPrefix(route, f.failOn) {
		f.fail(w, http.StatusInternalServerError, "internal_error", "injected failure")
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch {
	case route == "GET /v1/health":
		f.ok(w, map[string]any{"status": "ok", "read_only": false})
	case route == "GET /v1/health/ready":
		if !f.ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			f.ok(w, map[string]any{"ready": false, "status": "not_ready", "reason": "admission_closed"})
			return
		}
		f.ok(w, map[string]any{"ready": true, "status": "ready", "admission_open": true})
	case route == "GET /v1/system/info":
		f.ok(w, map[string]any{"version": "3.5.5", "config_hash": "rev", "uptime_seconds": 42.0})
	case route == "GET /v1/users":
		out := []any{}
		for name := range f.users {
			u := map[string]any{"username": name, "enabled": true, "in_runtime": true, "current_connections": 2, "total_octets": 100, "active_unique_ips": 1}
			for k, v := range f.attrs[name] {
				u[k] = v
			}
			out = append(out, u)
		}
		f.ok(w, out)
	case route == "POST /v1/users":
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		name, _ := req["username"].(string)
		secret, _ := req["secret"].(string)
		if _, dup := f.users[name]; dup {
			f.fail(w, http.StatusConflict, "user_exists", "exists")
			return
		}
		f.users[name] = secret
		f.userWrites[name] = append(f.userWrites[name], string(body))
		attrs := map[string]any{}
		for k, v := range req {
			if k == "username" || k == "secret" || v == nil {
				continue
			}
			attrs[k] = v
		}
		f.attrs[name] = attrs
		w.WriteHeader(http.StatusCreated)
		f.ok(w, map[string]any{"user": map[string]any{"username": name, "enabled": true}, "secret": secret})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/v1/users/"):
		name := strings.TrimPrefix(r.URL.Path, "/v1/users/")
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		f.userWrites[name] = append(f.userWrites[name], string(body))
		if s, okv := req["secret"].(string); okv {
			f.users[name] = s
		}
		if f.attrs[name] == nil {
			f.attrs[name] = map[string]any{}
		}
		for k, v := range req {
			switch {
			case k == "secret":
			case v == nil: // JSON Merge Patch: null removes the per-user override
				delete(f.attrs[name], k)
			default:
				f.attrs[name][k] = v
			}
		}
		f.ok(w, map[string]any{"username": name, "enabled": true})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/users/"):
		name := strings.TrimPrefix(r.URL.Path, "/v1/users/")
		for _, p := range f.profileUsers() {
			if p == name {
				f.fail(w, http.StatusBadRequest, "bad_request", "user is referenced by a web profile")
				return
			}
		}
		delete(f.users, name)
		delete(f.attrs, name)
		f.ok(w, map[string]any{"username": name, "in_runtime": false})
	case route == "GET /v1/config":
		f.ok(w, f.config)
	case route == "PATCH /v1/config":
		f.patches = append(f.patches, string(body))
		var patch map[string]any
		if err := json.Unmarshal(body, &patch); err != nil {
			f.fail(w, http.StatusBadRequest, "bad_request", "invalid json")
			return
		}
		if len(patch) == 0 {
			f.fail(w, http.StatusBadRequest, "bad_request", "empty patch")
			return
		}
		changed := make([]string, 0, len(patch))
		for section, v := range patch {
			switch section {
			case "web", "general", "censorship", "server":
			default:
				f.fail(w, http.StatusBadRequest, "section_not_editable", "not editable: "+section)
				return
			}
			body, okv := v.(map[string]any)
			if !okv {
				f.fail(w, http.StatusBadRequest, "bad_request", "section is not a table")
				return
			}
			cur, _ := f.config[section].(map[string]any)
			if cur == nil {
				cur = map[string]any{}
				f.config[section] = cur
			}
			mergePatch(cur, body)
			changed = append(changed, section)
		}
		reloadRequired, _ := f.config["__force_runtime_reload"].(bool)
		f.ok(w, map[string]any{"revision": "rev2", "changed": changed, "runtime_reload_required": reloadRequired})
	case route == "POST /v1/system/reload":
		f.reloads++
		w.WriteHeader(http.StatusAccepted)
		f.ok(w, map[string]any{"reload_id": 1, "state": "accepted", "mode": "drain"})
	case strings.HasPrefix(r.URL.Path, "/v1/system/reload/"):
		state := f.reloadStates[0]
		if len(f.reloadStates) > 1 {
			f.reloadStates = f.reloadStates[1:]
		}
		f.ok(w, map[string]any{"reload_id": 1, "state": state})
	case route == "GET /v1/runtime/connections/summary":
		f.ok(w, map[string]any{"enabled": true, "data": map[string]any{
			"totals": map[string]any{"current_connections": 7, "active_users": 1},
			"top":    map[string]any{"limit": 10, "by_connections": []any{map[string]any{"username": "k1", "current_connections": 5, "total_octets": 4096}}},
		}})
	case route == "GET /v1/stats/upstreams":
		if f.upstreams != nil {
			f.ok(w, f.upstreams)
			return
		}
		f.ok(w, defaultUpstreams())
	case r.URL.Path == "/metrics":
		_, _ = w.Write([]byte("telemt_connections_total 5\n"))
	default:
		f.fail(w, http.StatusNotFound, "not_found", "no route")
	}
}

func mergePatch(dst, patch map[string]any) {
	for k, v := range patch {
		sub, isTable := v.(map[string]any)
		cur, curIsTable := dst[k].(map[string]any)
		if isTable && curIsTable {
			mergePatch(cur, sub)
			continue
		}
		dst[k] = v
	}
}

// listeners returns the current server.listeners array.
func (f *fakeTelemt) listeners() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	server, _ := f.config["server"].(map[string]any)
	out, _ := server["listeners"].([]any)
	return out
}

// section returns one top-level config section.
func (f *fakeTelemt) section(name string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, _ := f.config[name].(map[string]any)
	return out
}

// currentUsers returns the usernames the fake currently holds.
func (f *fakeTelemt) currentUsers() map[string]string {
	users, _, _, _ := f.snapshot()
	return users
}

// setReloadStates fixes the sequence GET /v1/system/reload/{id} reports, one state per poll.
func (f *fakeTelemt) setReloadStates(states ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloadStates = states
}

// profileUsers returns the usernames referenced by the first vhost's profiles.
func (f *fakeTelemt) profileUsers() []string {
	web, _ := f.config["web"].(map[string]any)
	vhosts, _ := web["vhosts"].([]any)
	if len(vhosts) == 0 {
		return nil
	}
	vh, _ := vhosts[0].(map[string]any)
	profiles, _ := vh["profiles"].([]any)
	var out []string
	for _, p := range profiles {
		m, _ := p.(map[string]any)
		if u, okv := m["user"].(string); okv {
			out = append(out, u)
		}
	}
	return out
}

func (f *fakeTelemt) snapshot() (users map[string]string, writes []string, patches []string, reloads int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	users = map[string]string{}
	for k, v := range f.users {
		users[k] = v
	}
	return users, append([]string(nil), f.writes...), append([]string(nil), f.patches...), f.reloads
}

func (f *fakeTelemt) resetWrites() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes, f.patches, f.reloads = nil, nil, 0
	f.userWrites = map[string][]string{}
}

// bodies returns the raw create/patch bodies the agent sent for one user since the last reset.
func (f *fakeTelemt) bodies(user string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.userWrites[user]...)
}

func (f *fakeTelemt) setAttrs(user string, attrs map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attrs[user] = attrs
}

func telemtHandler(t *testing.T, ex *fakeExec) (*Handler, Config, *fakeTelemt) {
	t.Helper()
	dir := t.TempDir()
	siteDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ft := newFakeTelemt(t, siteDir)
	cfg := Config{
		Engine: EngineTelemt, StateDir: filepath.Join(dir, "state"),
		SiteDir: filepath.Join(dir, "unused-site"), TelemtSiteDir: siteDir,
		TelemtAPI: ft.srv.URL, TelemtAPIToken: "tok-abc", TelemtMetricsURL: ft.srv.URL + "/metrics",
		TelemtConfigPath: filepath.Join(dir, "telemt.toml"),
		HealthWait:       2 * time.Second,
	}
	// init-node records the node's own user, so the first apply does not re-set its secret.
	if err := writeTelemtState(cfg.StateDir, telemtState{SecretHashes: map[string]string{"node": secretFingerprint(secretNode)}}); err != nil {
		t.Fatal(err)
	}
	return NewHandler(cfg, ex, slog.New(slog.DiscardHandler)), cfg, ft
}

func profiles(pairs ...string) []*agentv1.Profile {
	out := make([]*agentv1.Profile, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, &agentv1.Profile{Name: pairs[i], Secret: pairs[i+1], Enabled: true})
	}
	return out
}

const (
	secretNode = "00000000000000000000000000000000"
	secretK1   = "11111111111111111111111111111111"
	secretK2   = "22222222222222222222222222222222"
)

func TestTelemtApplyCreatesUserAndPatchesProfiles(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      profiles("node", secretNode, "k1", secretK1),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if res.RestartedRelay || res.RestartedMtproxy {
		t.Fatalf("telemt applies must not restart anything: %+v", res)
	}
	users, writes, patches, reloads := ft.snapshot()
	if users["k1"] != secretK1 {
		t.Fatalf("user not created: %v", users)
	}
	if reloads != 0 {
		t.Fatalf("profile-only change must not reload: %d", reloads)
	}
	if len(patches) != 1 {
		t.Fatalf("expected exactly one config patch, got %v", patches)
	}
	if writes[0] != "POST /v1/users" || writes[1] != "PATCH /v1/config" {
		t.Fatalf("users must be created before the profile patch: %v", writes)
	}

	var patch struct {
		Web struct {
			Vhosts []map[string]any `json:"vhosts"`
		} `json:"web"`
	}
	if err := json.Unmarshal([]byte(patches[0]), &patch); err != nil {
		t.Fatal(err)
	}
	if len(patch.Web.Vhosts) != 1 {
		t.Fatalf("patch vhosts: %s", patches[0])
	}
	vh := patch.Web.Vhosts[0]
	if vh["host"] != "n1.example.com" || vh["public_addr"] != "203.0.113.7:443" || vh["decoy"] == nil {
		t.Fatalf("vhost fields lost: %s", patches[0])
	}
	got, _ := vh["profiles"].([]any)
	if len(got) != 2 {
		t.Fatalf("profiles: %s", patches[0])
	}
	for _, p := range got {
		m := p.(map[string]any)
		if m["secret_mode"] != "plain" {
			t.Fatalf("secret_mode: %s", patches[0])
		}
	}
	if strings.Contains(patches[0], secretK1) {
		t.Fatalf("secrets must not appear in the web config patch: %s", patches[0])
	}
	if strings.Contains(res.Log, secretK1) || strings.Contains(res.Log, "tok-abc") {
		t.Fatalf("apply log leaks a secret: %s", res.Log)
	}
}

func TestTelemtApplyIsIdempotent(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	req := &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      profiles("node", secretNode, "k1", secretK1),
		Site:          &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "index.html", Content: []byte("old")}}},
	}
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("first apply: %s", res.Log)
	}
	ft.resetWrites()
	res := h.Apply(context.Background(), req)
	if !res.Ok {
		t.Fatalf("second apply: %s", res.Log)
	}
	_, writes, patches, reloads := ft.snapshot()
	if len(writes) != 0 || len(patches) != 0 || reloads != 0 {
		t.Fatalf("second apply wrote: %v %v %d\n%s", writes, patches, reloads, res.Log)
	}
}

func TestTelemtApplyDeletesUndesiredUsersAfterTheProfilePatch(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	if res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	}); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	ft.resetWrites()
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	users, writes, _, _ := ft.snapshot()
	if _, still := users["k1"]; still {
		t.Fatalf("user not deleted: %v", users)
	}
	if len(writes) != 2 || writes[0] != "PATCH /v1/config" || writes[1] != "DELETE /v1/users/k1" {
		t.Fatalf("profile patch must precede the delete: %v", writes)
	}
}

func TestTelemtApplyPatchesChangedSecret(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	if res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	}); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	ft.resetWrites()
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK2),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	users, writes, patches, _ := ft.snapshot()
	if users["k1"] != secretK2 {
		t.Fatalf("secret not rotated: %v", users)
	}
	if len(writes) != 1 || writes[0] != "PATCH /v1/users/k1" {
		t.Fatalf("writes: %v", writes)
	}
	if len(patches) != 0 {
		t.Fatalf("unchanged profile membership must not patch the config: %v", patches)
	}
}

func TestTelemtApplyResetsSecretsWhenTheStateFileIsLost(t *testing.T) {
	h, cfg, ft := telemtHandler(t, &fakeExec{})
	if err := os.Remove(telemtStatePath(cfg.StateDir)); err != nil {
		t.Fatal(err)
	}
	// Without a recorded fingerprint the agent cannot tell a rotated secret from an unchanged one.
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	_, writes, _, _ := ft.snapshot()
	if len(writes) != 1 || writes[0] != "PATCH /v1/users/node" {
		t.Fatalf("writes: %v", writes)
	}
}

func TestTelemtApplyDeploysSiteAndReloads(t *testing.T) {
	h, cfg, ft := telemtHandler(t, &fakeExec{})
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{
			{Path: "index.html", Content: []byte("new")},
			{Path: "a/s.css", Content: []byte("p{}")},
		}},
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	idx, _ := os.ReadFile(filepath.Join(cfg.TelemtSiteDir, "index.html"))
	css, _ := os.ReadFile(filepath.Join(cfg.TelemtSiteDir, "a", "s.css"))
	if string(idx) != "new" || string(css) != "p{}" {
		t.Fatalf("decoy site not deployed: %q %q", idx, css)
	}
	_, _, patches, reloads := ft.snapshot()
	if reloads != 1 {
		t.Fatalf("a changed decoy snapshot needs exactly one reload, got %d", reloads)
	}
	if len(patches) != 0 {
		t.Fatalf("site-only apply must not patch the config: %v", patches)
	}
}

func TestTelemtApplyReloadsWhenTelemtAsksForIt(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.mu.Lock()
	ft.config["__force_runtime_reload"] = true
	ft.mu.Unlock()
	// The fake reports runtime_reload_required only when this marker is set.
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if _, _, _, reloads := ft.snapshot(); reloads != 1 {
		t.Fatalf("runtime_reload_required must trigger a reload, got %d", reloads)
	}
}

func TestTelemtApplyRollsBackCreatedUsersOnConfigFailure(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	ft.mu.Lock()
	ft.failOn = "PATCH /v1/config"
	ft.mu.Unlock()
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	})
	if res.Ok {
		t.Fatal("apply must fail when the config patch is rejected")
	}
	if !res.RolledBack {
		t.Fatalf("rollback of a create-only apply must succeed: %s", res.Log)
	}
	users, _, _, _ := ft.snapshot()
	if _, still := users["k1"]; still {
		t.Fatalf("created user not removed on rollback: %v", users)
	}
	if !strings.Contains(res.Log, "internal_error") {
		t.Fatalf("log must carry the API error: %s", res.Log)
	}
}

func TestTelemtApplyReportsIrreversibleRollback(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	if res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	}); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	ft.resetWrites()
	ft.mu.Lock()
	ft.failOn = "PATCH /v1/config"
	ft.mu.Unlock()
	// A secret rotation cannot be undone: the previous secret is not recoverable from the API.
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK2, "k3", secretK1),
	})
	if res.Ok {
		t.Fatal("apply must fail")
	}
	if res.RolledBack {
		t.Fatalf("a rotated secret makes the rollback incomplete: %s", res.Log)
	}
}

func TestTelemtApplyRejectsBadProfiles(t *testing.T) {
	h, _, _ := telemtHandler(t, &fakeExec{})
	for name, req := range map[string]*agentv1.ApplyRequest{
		"no profiles": {ApplyProfiles: true},
		"bad secret":  {ApplyProfiles: true, Profiles: profiles("k1", "nothex")},
		"bad name":    {ApplyProfiles: true, Profiles: profiles("bad name", secretK1)},
		"no index":    {Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "a.css", Content: []byte("p{}")}}}},
		"escaping path": {
			Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{
				{Path: "index.html", Content: []byte("x")}, {Path: "../evil", Content: []byte("x")},
			}},
		},
	} {
		if res := h.Apply(context.Background(), req); res.Ok {
			t.Fatalf("%s: expected failure", name)
		}
	}
}

func TestTelemtHealth(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{"telemt": true, "caddy": true}}
	h, _, _ := telemtHandler(t, ex)
	rep := h.Health(context.Background())
	if !rep.RelayActive || !rep.CaddyActive {
		t.Fatalf("units: %+v", rep)
	}
	if !rep.MtproxyActive {
		t.Fatal("telemt is a single process, so the mtproxy leg must report active")
	}
	if !rep.Healthz || !rep.Readyz {
		t.Fatalf("api probes: %+v", rep)
	}
	if rep.TproxyVersion != "telemt 3.5.5" {
		t.Fatalf("version: %q", rep.TproxyVersion)
	}
	if rep.ProfileCount != 1 {
		t.Fatalf("profile count: %d", rep.ProfileCount)
	}
	if !ex.has("systemctl is-active telemt") {
		t.Fatalf("unit not probed: %v", ex.calls)
	}
}

func TestTelemtHealthWhenNotReady(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{}}
	h, _, ft := telemtHandler(t, ex)
	ft.mu.Lock()
	ft.ready = false
	ft.mu.Unlock()
	rep := h.Health(context.Background())
	if rep.RelayActive || rep.Readyz {
		t.Fatalf("expected inactive and not ready: %+v", rep)
	}
	if !rep.Healthz {
		t.Fatal("the API is still alive, so healthz must stay true")
	}
}

func TestTelemtHealthWhenTheAPIIsUnreachable(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{"telemt": true, "caddy": true}}
	h, _, ft := telemtHandler(t, ex)
	ft.srv.Close() // every control API call now fails with connection refused

	rep := h.Health(context.Background())
	if !rep.RelayActive {
		t.Fatal("systemd still reports the unit active, so RelayActive must stay true")
	}
	if rep.Healthz || rep.Readyz {
		t.Fatalf("probes must be false when the API is down: %+v", rep)
	}
	if rep.TproxyVersion != "" {
		t.Fatalf("no version may be invented when /v1/system/info is unreachable: %q", rep.TproxyVersion)
	}
	if rep.ProfileCount != 0 {
		t.Fatalf("profile count: %d", rep.ProfileCount)
	}
	if rep.AgentVersion != Version {
		t.Fatalf("agent version: %q", rep.AgentVersion)
	}
}

func defaultUpstreams() map[string]any {
	return map[string]any{
		"enabled": true,
		"zero":    map[string]any{"connect_success_total": 58, "connect_fail_total": 1, "unrelated": 7},
		"upstreams": []any{map[string]any{
			"route_kind": "direct", "healthy": true, "fails": 2, "last_check_age_secs": 29,
			"effective_latency_ms": 41.25, "weight": 1,
			"dc": []any{
				map[string]any{"dc": 1, "latency_ema_ms": 197.9, "ip_preference": "prefer_v4"},
				map[string]any{"dc": 2, "latency_ema_ms": 36.5, "ip_preference": "prefer_v4"},
				map[string]any{"dc": 4, "latency_ema_ms": nil, "ip_preference": "prefer_v6"},
			},
		}},
	}
}

func (f *fakeTelemt) setUpstreams(data map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstreams = data
}

func TestTelemtHealthCarriesDcConnectivity(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{"telemt": true, "caddy": true}}
	h, _, _ := telemtHandler(t, ex)
	rep := h.Health(context.Background())
	if !rep.DcDataAvailable {
		t.Fatalf("dc data must be available when telemt serves /v1/stats/upstreams: %+v", rep)
	}
	if !rep.UpstreamHealthy || rep.UpstreamFails != 2 || rep.EffectiveLatencyMs != 41.25 || rep.UpstreamLastCheckAgeSecs != 29 {
		t.Fatalf("upstream fields: %+v", rep)
	}
	if rep.ConnectSuccessTotal != 58 || rep.ConnectFailTotal != 1 {
		t.Fatalf("connect counters: %+v", rep)
	}
	if len(rep.Dcs) != 3 {
		t.Fatalf("dcs: %+v", rep.Dcs)
	}
	if d := rep.Dcs[0]; d.Dc != 1 || d.LatencyMs != 197.9 || !d.Known || d.IpPreference != "prefer_v4" {
		t.Fatalf("dc 1: %+v", d)
	}
	if d := rep.Dcs[1]; d.Dc != 2 || d.LatencyMs != 36.5 || !d.Known {
		t.Fatalf("dc 2: %+v", d)
	}
	// null latency_ema_ms is "not measured yet": known=false and never a made-up 0 ms reading.
	if d := rep.Dcs[2]; d.Dc != 4 || d.Known || d.LatencyMs != 0 || d.IpPreference != "prefer_v6" {
		t.Fatalf("dc 4 with a null EMA: %+v", d)
	}
	// The rest of the report is untouched by the extra call.
	if !rep.Healthz || !rep.Readyz || rep.TproxyVersion != "telemt 3.5.5" || rep.ProfileCount != 1 {
		t.Fatalf("base report: %+v", rep)
	}
}

func TestTelemtHealthWhenUpstreamStatsFail(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{"telemt": true, "caddy": true}}
	h, _, ft := telemtHandler(t, ex)
	ft.mu.Lock()
	ft.failOn = "GET /v1/stats/upstreams"
	ft.mu.Unlock()
	rep := h.Health(context.Background())
	if rep.DcDataAvailable || len(rep.Dcs) != 0 || rep.UpstreamHealthy || rep.EffectiveLatencyMs != 0 {
		t.Fatalf("a failed stats call must leave the DC fields empty and unavailable: %+v", rep)
	}
	// The heartbeat itself is intact: the stats call is best-effort.
	if !rep.RelayActive || !rep.Healthz || !rep.Readyz || rep.TproxyVersion != "telemt 3.5.5" || rep.ProfileCount != 1 {
		t.Fatalf("base report must survive a stats failure: %+v", rep)
	}
}

func TestTelemtHealthWhenUpstreamStatsDisabled(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{"telemt": true, "caddy": true}}
	h, _, ft := telemtHandler(t, ex)
	// telemt still fills the counters when tracking is off; none of it may be reported.
	ft.setUpstreams(map[string]any{
		"enabled": false, "zero": map[string]any{"connect_success_total": 5, "connect_fail_total": 0},
		"upstreams": []any{map[string]any{"route_kind": "direct", "healthy": true, "dc": []any{map[string]any{"dc": 1, "latency_ema_ms": 10.0}}}},
	})
	rep := h.Health(context.Background())
	if rep.DcDataAvailable || len(rep.Dcs) != 0 || rep.ConnectSuccessTotal != 0 || rep.UpstreamHealthy {
		t.Fatalf("enabled=false must report no DC data at all: %+v", rep)
	}
	if !rep.Healthz || rep.ProfileCount != 1 {
		t.Fatalf("base report: %+v", rep)
	}
}

func TestTelemtStatsAndMetrics(t *testing.T) {
	h, _, _ := telemtHandler(t, &fakeExec{})
	resp := h.Handle(context.Background(), &agentv1.Request{Body: &agentv1.Request_Stats{Stats: &agentv1.StatsRequest{}}})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	vals := resp.GetStats().Values
	if vals["users"] != "1" || vals["connections_total"] != "7" {
		t.Fatalf("stats: %v", vals)
	}
	if vals["user.node.connections"] != "2" || vals["user.node.octets"] != "100" || vals["user.node.active_ips"] != "1" {
		t.Fatalf("per-user stats: %v", vals)
	}
	if vals["user.k1.connections"] != "5" || vals["user.k1.octets"] != "4096" {
		t.Fatalf("summary top rows must be merged in: %v", vals)
	}

	m := h.Handle(context.Background(), &agentv1.Request{Body: &agentv1.Request_Metrics{Metrics: &agentv1.MetricsRequest{}}})
	if m.Error != "" || !strings.Contains(m.GetMetrics().Text, "telemt_connections_total 5") {
		t.Fatalf("metrics: %+v", m)
	}
}

func TestTelemtRestartRelayRestartsTelemt(t *testing.T) {
	ex := &fakeExec{}
	h, _, _ := telemtHandler(t, ex)
	resp := h.Handle(context.Background(), &agentv1.Request{Body: &agentv1.Request_RestartRelay{RestartRelay: &agentv1.RestartRelayRequest{}}})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if !ex.has("systemctl restart telemt") {
		t.Fatalf("calls: %v", ex.calls)
	}
}

func TestTelemtLogsUnitIsAllowed(t *testing.T) {
	if !allowedUnits["telemt"] {
		t.Fatal("telemt must be tailable")
	}
}

func TestLoadConfigTelemtEngine(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "api.token")
	if err := os.WriteFile(tokenFile, []byte("deadbeef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"TGWP_PANEL_URL": "https://panel.test", "TGWP_TOKEN": "t",
		"TGWP_ENGINE": "telemt", "TGWP_TELEMT_API_TOKEN_FILE": tokenFile,
	}
	cfg, err := LoadConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Engine != EngineTelemt || cfg.TelemtAPIToken != "deadbeef" {
		t.Fatalf("config: %+v", cfg)
	}
	if cfg.TelemtAPI != DefaultTelemtAPI || cfg.TelemtSiteDir != DefaultTelemtSiteDir || cfg.TelemtBin != DefaultTelemtBin {
		t.Fatalf("defaults: %+v", cfg)
	}

	env["TGWP_ENGINE"] = "nope"
	if _, err := LoadConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("unknown engine must be rejected")
	}

	env["TGWP_ENGINE"] = ""
	cfg, err = LoadConfig(func(k string) string { return env[k] })
	if err != nil || cfg.Engine != EngineTProxy {
		t.Fatalf("default engine: %+v %v", cfg.Engine, err)
	}
}

// limitedProfile is a key profile carrying the full telemt policy the panel can express.
func limitedProfile(name, secret string, expires time.Time) *agentv1.Profile {
	p := &agentv1.Profile{
		Name: name, Secret: secret, Enabled: true,
		DataQuotaBytes: 5 << 30, RateLimitUpBps: 1_000_000, RateLimitDownBps: 2_000_000,
		MaxUniqueIps: 3, MaxTcpConns: 64,
	}
	if !expires.IsZero() {
		p.ExpiresAtUnix = expires.Unix()
	}
	return p
}

func TestTelemtApplySendsLimitsOnCreate(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	exp := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      append(profiles("node", secretNode), limitedProfile("k1", secretK1, exp)),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	got := ft.bodies("k1")
	if len(got) != 1 {
		t.Fatalf("expected exactly one create call, got %v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0]), &body); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]float64{
		"data_quota_bytes": 5 << 30, "rate_limit_up_bps": 1_000_000, "rate_limit_down_bps": 2_000_000,
		"max_unique_ips": 3, "max_tcp_conns": 64,
	} {
		if body[field] != want {
			t.Fatalf("%s = %v, want %v (body %s)", field, body[field], want, got[0])
		}
	}
	if body["enabled"] != true {
		t.Fatalf("enabled = %v (body %s)", body["enabled"], got[0])
	}
	ts, _ := body["expiration_rfc3339"].(string)
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil || !parsed.Equal(exp) {
		t.Fatalf("expiration_rfc3339 = %q, want %s", ts, exp.Format(time.RFC3339))
	}
}

func TestTelemtApplyDisablesProfile(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	req := &agentv1.ApplyRequest{ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1)}
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	ft.resetWrites()
	req.Profiles[1].Enabled = false
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	got := ft.bodies("k1")
	if len(got) != 1 || !strings.Contains(got[0], `"enabled":false`) {
		t.Fatalf("disable not pushed: %v", got)
	}
}

func TestTelemtApplyPatchesOnlyChangedLimits(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	exp := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	req := &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      append(profiles("node", secretNode), limitedProfile("k1", secretK1, exp)),
	}
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	// Same limits again: nothing may be written.
	ft.resetWrites()
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("second apply: %s", res.Log)
	}
	if _, writes, _, _ := ft.snapshot(); len(writes) != 0 {
		t.Fatalf("unchanged limits must not be patched: %v", writes)
	}
	// Raise the quota: exactly one PATCH carrying the new value.
	req.Profiles[1].DataQuotaBytes = 10 << 30
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("third apply: %s", res.Log)
	}
	_, writes, patches, _ := ft.snapshot()
	if len(writes) != 1 || writes[0] != "PATCH /v1/users/k1" {
		t.Fatalf("writes: %v", writes)
	}
	if len(patches) != 0 {
		t.Fatalf("a limit change must not touch the web config: %v", patches)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(ft.bodies("k1")[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body["data_quota_bytes"] != float64(10<<30) {
		t.Fatalf("quota not patched: %s", ft.bodies("k1")[0])
	}
}

func TestTelemtApplyClearsRemovedLimits(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	exp := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	req := &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles:      append(profiles("node", secretNode), limitedProfile("k1", secretK1, exp)),
	}
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("setup apply: %s", res.Log)
	}
	ft.resetWrites()
	// The operator cleared every limit and the expiry on the key.
	req.Profiles[1] = &agentv1.Profile{Name: "k1", Secret: secretK1, Enabled: true}
	if res := h.Apply(context.Background(), req); !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	raw := ft.bodies("k1")
	if len(raw) != 1 {
		t.Fatalf("expected one patch, got %v", raw)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw[0]), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"data_quota_bytes", "rate_limit_up_bps", "rate_limit_down_bps", "max_unique_ips", "max_tcp_conns", "expiration_rfc3339"} {
		v, present := body[field]
		if !present || v != nil {
			t.Fatalf("%s must be sent as null, got %v (body %s)", field, v, raw[0])
		}
	}
}

func TestTelemtApplySkipsPatchWhenLimitsAlreadyMatch(t *testing.T) {
	h, cfg, ft := telemtHandler(t, &fakeExec{})
	exp := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	ft.setAttrs("node", map[string]any{
		"data_quota_bytes": float64(5 << 30), "rate_limit_up_bps": float64(1_000_000),
		"rate_limit_down_bps": float64(2_000_000), "max_unique_ips": float64(3),
		"max_tcp_conns": float64(64), "expiration_rfc3339": exp.UTC().Format(time.RFC3339),
		"enabled": true,
	})
	// The state file knows the secret but nothing about limits.
	if err := writeTelemtState(cfg.StateDir, telemtState{SecretHashes: map[string]string{"node": secretFingerprint(secretNode)}}); err != nil {
		t.Fatal(err)
	}
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: []*agentv1.Profile{limitedProfile("node", secretNode, exp)},
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if got := ft.bodies("node"); len(got) != 0 {
		t.Fatalf("matching limits must not be patched: %v", got)
	}
}

func TestTelemtApplyLogAlwaysSaysNoRestart(t *testing.T) {
	h, _, _ := telemtHandler(t, &fakeExec{})
	req := &agentv1.ApplyRequest{ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1)}
	first := h.Apply(context.Background(), req)
	second := h.Apply(context.Background(), req)
	for _, res := range []*agentv1.ApplyResult{first, second} {
		if !res.Ok || !strings.Contains(res.Log, "no restart") {
			t.Fatalf("log must state that nothing was restarted: %q", res.Log)
		}
	}
}

func (f *fakeTelemt) forceRuntimeReload() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config["__force_runtime_reload"] = true
}

// withConfig rebuilds the handler around a modified Config, keeping the state the fixture already seeded.
func withConfig(t *testing.T, cfg Config, ex *fakeExec) *Handler {
	t.Helper()
	return NewHandler(cfg, ex, slog.New(slog.DiscardHandler))
}

func TestTelemtReloadWaitsOutADrainLongerThanHealthWait(t *testing.T) {
	ex := &fakeExec{}
	_, cfg, ft := telemtHandler(t, ex)
	cfg.HealthWait = 20 * time.Millisecond
	cfg.ReloadWait = 10 * time.Second
	h := withConfig(t, cfg, ex)
	ft.forceRuntimeReload()
	ft.setReloadStates("accepted", "activating", "draining", "draining", "draining", "draining", "draining", "succeeded")

	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	})
	if !res.Ok {
		t.Fatalf("a drain longer than HealthWait must not fail the apply: %s", res.Log)
	}
	if res.RolledBack {
		t.Fatalf("apply rolled back: %s", res.Log)
	}
	if !strings.Contains(res.Log, "runtime reload 1 succeeded") {
		t.Fatalf("log must record the successful reload: %q", res.Log)
	}
	if _, ok := ft.currentUsers()["k1"]; !ok {
		t.Fatal("the created user must survive a slow drain")
	}
}

func TestTelemtReloadStillDrainingAtTheDeadlineIsNotAFailure(t *testing.T) {
	ex := &fakeExec{}
	_, cfg, ft := telemtHandler(t, ex)
	cfg.ReloadWait = 300 * time.Millisecond
	h := withConfig(t, cfg, ex)
	ft.forceRuntimeReload()
	ft.setReloadStates("draining") // never leaves the drain

	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	})
	if !res.Ok || res.RolledBack {
		t.Fatalf("a reload still draining at the deadline must not roll back: %s", res.Log)
	}
	if !strings.Contains(res.Log, "still draining after") || !strings.Contains(res.Log, "new generation active") {
		t.Fatalf("log must say the new generation is active: %q", res.Log)
	}
	if _, ok := ft.currentUsers()["k1"]; !ok {
		t.Fatal("the created user must survive a reload that is still draining")
	}
}

// C1: a reload that actually failed is still an error, and the apply rolls back.
func TestTelemtReloadFailureRollsBack(t *testing.T) {
	ex := &fakeExec{}
	_, cfg, ft := telemtHandler(t, ex)
	cfg.ReloadWait = 5 * time.Second
	h := withConfig(t, cfg, ex)
	ft.forceRuntimeReload()
	ft.setReloadStates("accepted", "failed")

	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1),
	})
	if res.Ok {
		t.Fatalf("a failed reload must fail the apply: %s", res.Log)
	}
	if _, ok := ft.currentUsers()["k1"]; ok {
		t.Fatalf("the created user must be removed by the rollback: %s", res.Log)
	}
}

// C2: changing tls_domain/classic_port in the panel reaches the node.
func TestTelemtApplyMovesTheFakeTLSListener(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
		TlsDomain: "front.example.com", ClassicPort: 9443,
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if !res.RestartedRelay {
		t.Fatalf("a listener move must report a relay restart: %s", res.Log)
	}

	_, _, patches, _ := ft.snapshot()
	var sawCensorship, sawListeners, sawLinks bool
	for _, raw := range patches {
		var p map[string]any
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("patch is not json: %v", err)
		}
		if c, ok := p["censorship"].(map[string]any); ok {
			if c["tls_domain"] != "front.example.com" {
				t.Fatalf("wrong tls_domain patch: %s", raw)
			}
			if len(c) != 1 {
				t.Fatalf("only tls_domain may be patched, tables deep-merge: %s", raw)
			}
			sawCensorship = true
		}
		if srv, ok := p["server"].(map[string]any); ok {
			ls, _ := srv["listeners"].([]any)
			if len(ls) != 2 {
				t.Fatalf("the listener array must be sent whole: %s", raw)
			}
			sawListeners = true
		}
		if g, ok := p["general"].(map[string]any); ok {
			l, _ := g["links"].(map[string]any)
			if l["public_port"] != float64(9443) {
				t.Fatalf("general.links.public_port must follow the listener: %s", raw)
			}
			sawLinks = true
		}
	}
	if !sawCensorship || !sawListeners || !sawLinks {
		t.Fatalf("missing patches (censorship=%v listeners=%v links=%v): %v", sawCensorship, sawListeners, sawLinks, patches)
	}

	// The node's live config must now hold the new listener, with the WEB listener intact.
	ls := ft.listeners()
	fake, _ := ls[0].(map[string]any)
	web, _ := ls[1].(map[string]any)
	if fake["port"] != float64(9443) || fake["synlimit"] != "nftables" {
		t.Fatalf("fake-tls listener not moved correctly: %#v", fake)
	}
	if web["port"] != float64(18080) || web["transport"] != "web" {
		t.Fatalf("web listener must survive the move untouched: %#v", web)
	}
	if ft.section("censorship")["tls_domain"] != "front.example.com" {
		t.Fatalf("tls_domain not applied: %#v", ft.section("censorship"))
	}

	if !ex.has("systemctl restart telemt") {
		t.Fatalf("telemt must be restarted for a process-owned listener change: %v", ex.list())
	}
}

func TestTelemtApplyRewritesThePublicAddr(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
		TlsDomain: "n1.example.com", ClassicPort: 8443, PublicIp: "104.239.66.129",
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if !res.RestartedRelay {
		t.Fatalf("a public_addr change must report a relay restart: %s", res.Log)
	}
	_, _, patches, _ := ft.snapshot()
	if len(patches) != 1 {
		t.Fatalf("exactly one config patch expected (web.vhosts), got %v", patches)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(patches[0]), &p); err != nil {
		t.Fatalf("patch is not json: %v", err)
	}
	web, _ := p["web"].(map[string]any)
	vh, _ := web["vhosts"].([]any)
	if len(vh) != 1 {
		t.Fatalf("the vhost array must be sent whole: %s", patches[0])
	}
	got, _ := vh[0].(map[string]any)
	if got["public_addr"] != "104.239.66.129:443" || got["host"] != "n1.example.com" || got["decoy"] == nil || got["profiles"] == nil {
		t.Fatalf("vhost must keep everything but public_addr: %#v", got)
	}
	live, _ := ft.section("web")["vhosts"].([]any)
	liveVhost, _ := live[0].(map[string]any)
	if liveVhost["public_addr"] != "104.239.66.129:443" {
		t.Fatalf("public_addr not applied: %#v", liveVhost)
	}
	if !ex.has("systemctl restart telemt") {
		t.Fatalf("telemt must be restarted for a public_addr change: %v", ex.list())
	}
	if !strings.Contains(res.Log, "web.vhosts.public_addr 203.0.113.7:443 -> 104.239.66.129:443") {
		t.Fatalf("log must name the change: %s", res.Log)
	}

	// Steady state: the same value again is a no-op.
	ft.resetWrites()
	ex2 := &fakeExec{}
	h.exec = ex2
	res = h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
		TlsDomain: "n1.example.com", ClassicPort: 8443, PublicIp: "104.239.66.129",
	})
	if !res.Ok || res.RestartedRelay {
		t.Fatalf("unchanged public ip must be a no-op: ok=%v restarted=%v log=%s", res.Ok, res.RestartedRelay, res.Log)
	}
	if _, _, patches, _ := ft.snapshot(); len(patches) != 0 {
		t.Fatalf("no config patch expected, got %v", patches)
	}
	if ex2.has("systemctl restart telemt") {
		t.Fatalf("no restart expected: %v", ex2.list())
	}
}

// A malformed public IP from the panel is refused before anything is touched.
func TestTelemtApplyRejectsBadPublicIP(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode), PublicIp: "not-an-ip",
	})
	if res.Ok || !strings.Contains(res.Log, `invalid public ip "not-an-ip"`) {
		t.Fatalf("bad public ip must fail the apply: ok=%v log=%s", res.Ok, res.Log)
	}
	if _, _, patches, _ := ft.snapshot(); len(patches) != 0 {
		t.Fatalf("no config patch expected, got %v", patches)
	}
	if ex.has("systemctl restart telemt") {
		t.Fatalf("no restart expected: %v", ex.list())
	}
}

func TestTelemtApplyRestoresPublicAddrWhenTheRestartFails(t *testing.T) {
	ex := &fakeExec{failNth: "restart telemt", failNthCount: 1, failMsg: "job failed"}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode), PublicIp: "104.239.66.129",
	})
	if res.Ok {
		t.Fatalf("a failed restart must fail the apply: %s", res.Log)
	}
	if !res.RolledBack {
		t.Fatalf("the public_addr change must be rolled back: %s", res.Log)
	}
	live, _ := ft.section("web")["vhosts"].([]any)
	liveVhost, _ := live[0].(map[string]any)
	if liveVhost["public_addr"] != "203.0.113.7:443" {
		t.Fatalf("public_addr not restored: %#v", liveVhost)
	}
	var restarts int
	for _, c := range ex.list() {
		if strings.Contains(c, "restart telemt") {
			restarts++
		}
	}
	if restarts != 2 {
		t.Fatalf("expected the apply's restart and the rollback's, got %d: %v", restarts, ex.list())
	}
}

// C2: an apply that carries the listener values telemt already has changes nothing and restarts nothing.
func TestTelemtApplyLeavesMatchingListenersAlone(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
		TlsDomain: "n1.example.com", ClassicPort: 8443, PublicIp: "203.0.113.7",
	})
	if !res.Ok || res.RestartedRelay {
		t.Fatalf("unchanged listeners must be a no-op: ok=%v restarted=%v log=%s", res.Ok, res.RestartedRelay, res.Log)
	}
	if _, _, patches, _ := ft.snapshot(); len(patches) != 0 {
		t.Fatalf("no config patch expected, got %v", patches)
	}
	if ex.has("systemctl restart telemt") {
		t.Fatalf("no restart expected: %v", ex.list())
	}
}

func TestTelemtApplyRestoresListenersWhenTheRestartFails(t *testing.T) {
	ex := &fakeExec{failNth: "restart telemt", failNthCount: 1, failMsg: "job failed"}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
		TlsDomain: "front.example.com", ClassicPort: 9443,
	})
	if res.Ok {
		t.Fatalf("a failed restart must fail the apply: %s", res.Log)
	}
	if !res.RolledBack {
		t.Fatalf("the listener change must be rolled back: %s", res.Log)
	}
	ls := ft.listeners()
	fake, _ := ls[0].(map[string]any)
	if fake["port"] != float64(8443) {
		t.Fatalf("listener port not restored: %#v", fake)
	}
	if ft.section("censorship")["tls_domain"] != "n1.example.com" {
		t.Fatalf("tls_domain not restored: %#v", ft.section("censorship"))
	}
	links, _ := ft.section("general")["links"].(map[string]any)
	if links["public_port"] != float64(8443) {
		t.Fatalf("general.links.public_port not restored: %#v", links)
	}
}

const sponsorTag = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestTelemtApplySetsSponsorChannelAdTag(t *testing.T) {
	ex := &fakeExec{}
	h, _, ft := telemtHandler(t, ex)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1), AdTag: sponsorTag,
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if res.RestartedRelay {
		t.Fatalf("a hot middle-proxy toggle must not restart telemt: %s", res.Log)
	}
	if ft.section("general")["use_middle_proxy"] != true {
		t.Fatalf("general.use_middle_proxy not enabled: %#v", ft.section("general"))
	}
	_, _, patches, _ := ft.snapshot()
	var sawGeneral bool
	for _, raw := range patches {
		var p map[string]any
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("patch is not json: %v", err)
		}
		if g, ok := p["general"].(map[string]any); ok {
			if g["use_middle_proxy"] != true {
				t.Fatalf("wrong general patch: %s", raw)
			}
			sawGeneral = true
		}
	}
	if !sawGeneral {
		t.Fatalf("missing general.use_middle_proxy patch: %v", patches)
	}
	for _, name := range []string{"node", "k1"} {
		body := ft.bodies(name)
		if len(body) == 0 || !strings.Contains(body[len(body)-1], `"user_ad_tag":"`+sponsorTag+`"`) {
			t.Fatalf("user %s missing sponsor tag: %v", name, body)
		}
	}
	if !strings.Contains(res.Log, "general.use_middle_proxy false -> true") {
		t.Fatalf("log must name the change: %s", res.Log)
	}

	// Steady state: the same tag again touches nothing.
	ft.resetWrites()
	res = h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode, "k1", secretK1), AdTag: sponsorTag,
	})
	if !res.Ok || res.RestartedRelay {
		t.Fatalf("an unchanged tag must be a no-op: ok=%v restarted=%v log=%s", res.Ok, res.RestartedRelay, res.Log)
	}
	if _, _, patches, _ := ft.snapshot(); len(patches) != 0 {
		t.Fatalf("no config patch expected, got %v", patches)
	}
}

func TestTelemtApplyClearsSponsorChannelAdTag(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode), AdTag: sponsorTag,
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	ft.resetWrites()

	res = h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode),
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if ft.section("general")["use_middle_proxy"] != false {
		t.Fatalf("general.use_middle_proxy not disabled: %#v", ft.section("general"))
	}
	body := ft.bodies("node")
	if len(body) == 0 || !strings.Contains(body[len(body)-1], `"user_ad_tag":null`) {
		t.Fatalf("sponsor tag must be explicitly cleared: %v", body)
	}
	if !strings.Contains(res.Log, "general.use_middle_proxy true -> false") {
		t.Fatalf("log must name the change: %s", res.Log)
	}
}

// A malformed ad tag from the panel is refused before anything is touched.
func TestTelemtApplyRejectsBadAdTag(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true, Profiles: profiles("node", secretNode), AdTag: "not-a-tag",
	})
	if res.Ok || !strings.Contains(res.Log, "ad tag must be 32 lowercase hex characters") {
		t.Fatalf("bad ad tag must fail the apply: ok=%v log=%s", res.Ok, res.Log)
	}
	if _, _, patches, _ := ft.snapshot(); len(patches) != 0 {
		t.Fatalf("no config patch expected, got %v", patches)
	}
}
