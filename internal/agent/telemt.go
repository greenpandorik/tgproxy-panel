package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

// telemtSecretMode is the only secret representation the panel issues for WEB profiles.
const telemtSecretMode = "plain"

type telemtDesired struct {
	name   string
	secret string
	policy telemtPolicy
}

// telemtPolicy is the per-user state the panel owns, in telemt's own units.
type telemtPolicy struct {
	DataQuotaBytes   uint64 `json:"data_quota_bytes"`
	RateLimitUpBps   uint64 `json:"rate_limit_up_bps"`
	RateLimitDownBps uint64 `json:"rate_limit_down_bps"`
	MaxUniqueIPs     uint64 `json:"max_unique_ips"`
	MaxTCPConns      uint64 `json:"max_tcp_conns"`
	Expiration       string `json:"expiration_rfc3339"`
	Enabled          bool   `json:"enabled"`
	AdTag            string `json:"ad_tag"`
}

func telemtPolicyFrom(p *agentv1.Profile) telemtPolicy {
	out := telemtPolicy{
		DataQuotaBytes: p.GetDataQuotaBytes(), RateLimitUpBps: p.GetRateLimitUpBps(),
		RateLimitDownBps: p.GetRateLimitDownBps(), MaxUniqueIPs: uint64(p.GetMaxUniqueIps()),
		MaxTCPConns: uint64(p.GetMaxTcpConns()), Enabled: p.GetEnabled(),
	}
	if v := p.GetExpiresAtUnix(); v != 0 {
		out.Expiration = time.Unix(v, 0).UTC().Format(time.RFC3339)
	}
	return out
}

func (p telemtPolicy) fingerprint() string {
	raw, err := json.Marshal(p)
	if err != nil { // a struct of scalars cannot fail to marshal
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (p telemtPolicy) createRequest(name, secret string) telemt.CreateUserRequest {
	req := telemt.CreateUserRequest{Username: name, Secret: secret, Enabled: &p.Enabled}
	if p.AdTag != "" {
		tag := p.AdTag
		req.UserAdTag = &tag
	}
	if p.DataQuotaBytes > 0 {
		req.DataQuotaBytes = &p.DataQuotaBytes
	}
	if p.RateLimitUpBps > 0 {
		req.RateLimitUpBps = &p.RateLimitUpBps
	}
	if p.RateLimitDownBps > 0 {
		req.RateLimitDownBps = &p.RateLimitDownBps
	}
	if p.MaxUniqueIPs > 0 {
		req.MaxUniqueIPs = &p.MaxUniqueIPs
	}
	if p.MaxTCPConns > 0 {
		req.MaxTCPConns = &p.MaxTCPConns
	}
	if p.Expiration != "" {
		req.ExpirationRFC3339 = &p.Expiration
	}
	return req
}

func (p telemtPolicy) patchRequest(secret *string) telemt.PatchUserRequest {
	req := telemt.PatchUserRequest{Secret: secret, Enabled: &p.Enabled}
	set := func(dst **uint64, v *uint64, field string) {
		if *v > 0 {
			*dst = v
			return
		}
		req.Clear = append(req.Clear, field)
	}
	set(&req.DataQuotaBytes, &p.DataQuotaBytes, "data_quota_bytes")
	set(&req.RateLimitUpBps, &p.RateLimitUpBps, "rate_limit_up_bps")
	set(&req.RateLimitDownBps, &p.RateLimitDownBps, "rate_limit_down_bps")
	set(&req.MaxUniqueIPs, &p.MaxUniqueIPs, "max_unique_ips")
	set(&req.MaxTCPConns, &p.MaxTCPConns, "max_tcp_conns")
	if p.Expiration != "" {
		req.ExpirationRFC3339 = &p.Expiration
	} else {
		req.Clear = append(req.Clear, "expiration_rfc3339")
	}
	if p.AdTag != "" {
		tag := p.AdTag
		req.UserAdTag = &tag
	} else {
		req.Clear = append(req.Clear, "user_ad_tag")
	}
	return req
}

// matches reports whether telemt already holds exactly this policy.
func (p telemtPolicy) matches(u telemt.User) bool {
	if u.DataQuotaBytes != p.DataQuotaBytes || u.RateLimitUpBps != p.RateLimitUpBps ||
		u.RateLimitDownBps != p.RateLimitDownBps || u.MaxUniqueIPs != p.MaxUniqueIPs ||
		u.MaxTCPConns != p.MaxTCPConns || u.Enabled != p.Enabled || u.UserAdTag != p.AdTag {
		return false
	}
	if (u.ExpirationRFC3339 == "") != (p.Expiration == "") {
		return false
	}
	if p.Expiration == "" {
		return true
	}
	have, err1 := time.Parse(time.RFC3339, u.ExpirationRFC3339)
	want, err2 := time.Parse(time.RFC3339, p.Expiration)
	return err1 == nil && err2 == nil && have.Equal(want)
}

// telemtDesiredFrom validates the desired state before a single API call is made.
func telemtDesiredFrom(req *agentv1.ApplyRequest) ([]telemtDesired, error) {
	if len(req.Profiles) == 0 {
		return nil, errors.New("at least one profile is required")
	}
	if req.AdTag != "" && !hexRe.MatchString(req.AdTag) {
		return nil, fmt.Errorf("ad tag must be 32 lowercase hex characters")
	}
	seen := map[string]bool{}
	out := make([]telemtDesired, 0, len(req.Profiles))
	for _, p := range req.Profiles {
		if !userRe.MatchString(p.Name) {
			return nil, fmt.Errorf("invalid profile name %q", p.Name)
		}
		if !hexRe.MatchString(p.Secret) {
			return nil, fmt.Errorf("profile %q: secret must be 32 lowercase hex characters", p.Name)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("duplicate profile %q", p.Name)
		}
		seen[p.Name] = true
		policy := telemtPolicyFrom(p)
		policy.AdTag = req.AdTag
		out = append(out, telemtDesired{name: p.Name, secret: p.Secret, policy: policy})
	}
	return out, nil
}

// telemtState remembers what each user was last given.
type telemtState struct {
	SecretHashes map[string]string `json:"secret_hashes"`
	LimitHashes  map[string]string `json:"limit_hashes"`
}

func secretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func telemtStatePath(stateDir string) string {
	return filepath.Join(stateDir, "telemt-users.json")
}

// readTelemtState returns the last recorded fingerprints, with both maps always non-nil.
func readTelemtState(stateDir string) telemtState {
	out := telemtState{SecretHashes: map[string]string{}, LimitHashes: map[string]string{}}
	raw, err := os.ReadFile(telemtStatePath(stateDir))
	if err != nil {
		return out
	}
	var s telemtState
	if err := json.Unmarshal(raw, &s); err != nil {
		return out
	}
	if s.SecretHashes != nil {
		out.SecretHashes = s.SecretHashes
	}
	if s.LimitHashes != nil {
		out.LimitHashes = s.LimitHashes
	}
	return out
}

func writeTelemtState(stateDir string, st telemtState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return writeAtomic(telemtStatePath(stateDir), append(raw, '\n'), 0o600)
}

// telemtVhosts extracts web.vhosts from a GET /v1/config body.
func telemtVhosts(cfg map[string]any) ([]any, error) {
	web, _ := cfg["web"].(map[string]any)
	if web == nil {
		return nil, errors.New("telemt config has no [web] section; run init-node --engine telemt")
	}
	vhosts, _ := web["vhosts"].([]any)
	if len(vhosts) == 0 {
		return nil, errors.New("telemt config has no [[web.vhosts]] entry")
	}
	if _, ok := vhosts[0].(map[string]any); !ok {
		return nil, errors.New("telemt config web.vhosts[0] is not a table")
	}
	return vhosts, nil
}

func vhostProfileUsers(vhost map[string]any) []string {
	profiles, _ := vhost["profiles"].([]any)
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		m, _ := p.(map[string]any)
		if u, ok := m["user"].(string); ok {
			out = append(out, u)
		}
	}
	return out
}

func copyVhosts(vhosts []any) ([]any, error) {
	raw, err := json.Marshal(vhosts)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("telemt config web.vhosts is empty")
	}
	if _, ok := out[0].(map[string]any); !ok {
		return nil, errors.New("telemt config web.vhosts[0] is not a table")
	}
	return out, nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

// telemtRollback tracks what an apply changed so a failure can undo as much as possible.
type telemtRollback struct {
	created           []string // users this apply created
	prevVhosts        []any    // web.vhosts as they were before the profile patch (nil = not patched)
	siteBackup        string   // directory holding the previous decoy site ("" = site untouched)
	prevListeners     map[string]any
	publicAddrChanged bool
	prevMiddleProxy   *bool    // nil = untouched
	irreversible      []string // mutations telemt cannot undo from the information we hold
}

// telemtFakeTLSListener returns the index of the Fake-TLS listener inside `server.listeners`.
func telemtFakeTLSListener(listeners []any) int {
	for i, l := range listeners {
		m, _ := l.(map[string]any)
		if m == nil {
			continue
		}
		if t, _ := m["transport"].(string); t == "" || t == "mtproxy" {
			return i
		}
	}
	return -1
}

// jsonNumber reads a number out of a decoded JSON object.
func jsonNumber(v any) (uint32, bool) {
	switch n := v.(type) {
	case float64:
		if n < 0 || n > 65535 {
			return 0, false
		}
		return uint32(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i < 0 || i > 65535 {
			return 0, false
		}
		return uint32(i), true
	}
	return 0, false
}

// cloneJSON copies a decoded JSON value so a patch we build cannot alias the snapshot kept for rollback.
func cloneJSON[T any](v T) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (h *Handler) reconcileTelemtListeners(ctx context.Context, lg *applyLog, req *agentv1.ApplyRequest, rb *telemtRollback) (bool, error) {
	wantDomain, wantPort := req.GetTlsDomain(), req.GetClassicPort()
	if wantDomain == "" && wantPort == 0 {
		return false, nil
	}
	if wantDomain != "" && !validHostname(wantDomain) {
		return false, fmt.Errorf("invalid tls domain %q", wantDomain)
	}
	if wantPort != 0 {
		if wantPort > 65535 {
			return false, fmt.Errorf("classic port %d out of range", wantPort)
		}
		if wantPort == telemtWebListenPort {
			return false, fmt.Errorf("classic port %d collides with the WEB listener", wantPort)
		}
	}

	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	censorship, _ := cfg["censorship"].(map[string]any)
	curDomain, _ := censorship["tls_domain"].(string)
	server, _ := cfg["server"].(map[string]any)
	listeners, _ := server["listeners"].([]any)
	idx := telemtFakeTLSListener(listeners)
	var curPort uint32
	if idx >= 0 {
		lm, _ := listeners[idx].(map[string]any)
		curPort, _ = jsonNumber(lm["port"])
	}

	domainChanged := wantDomain != "" && wantDomain != curDomain
	portChanged := wantPort != 0 && wantPort != curPort
	if !domainChanged && !portChanged {
		return false, nil
	}
	if portChanged && idx < 0 {
		return false, errors.New("telemt config has no Fake-TLS listener to move")
	}

	undo := map[string]any{}
	if domainChanged {
		undo["censorship"] = map[string]any{"tls_domain": curDomain}
	}
	general, _ := cfg["general"].(map[string]any)
	links, _ := general["links"].(map[string]any)
	_, haveLinkPort := links["public_port"]
	if portChanged {
		prev, err := cloneJSON(listeners)
		if err != nil {
			return false, err
		}
		undo["server"] = map[string]any{"listeners": prev}
		if haveLinkPort {
			undo["general"] = map[string]any{"links": map[string]any{"public_port": links["public_port"]}}
		}
	}
	rb.prevListeners = undo

	if domainChanged {
		if _, err := h.tm.PatchConfig(ctx, map[string]any{"censorship": map[string]any{"tls_domain": wantDomain}}, false); err != nil {
			return true, fmt.Errorf("patch censorship.tls_domain: %w", err)
		}
		lg.f("censorship.tls_domain %s -> %s", curDomain, wantDomain)
	}
	if portChanged {
		next, err := cloneJSON(listeners)
		if err != nil {
			return true, err
		}
		lm, _ := next[idx].(map[string]any)
		lm["port"] = wantPort
		if _, err := h.tm.PatchConfig(ctx, map[string]any{"server": map[string]any{"listeners": next}}, false); err != nil {
			return true, fmt.Errorf("patch server.listeners: %w", err)
		}
		lg.f("fake-tls listener port %d -> %d", curPort, wantPort)
		if haveLinkPort {
			if _, err := h.tm.PatchConfig(ctx, map[string]any{"general": map[string]any{"links": map[string]any{"public_port": wantPort}}}, false); err != nil {
				return true, fmt.Errorf("patch general.links.public_port: %w", err)
			}
			lg.f("general.links.public_port -> %d", wantPort)
		}
	}
	return true, nil
}

// telemtPublicAddr is the WEB vhost's public socket address for a panel-supplied IP; IPv6 needs brackets.
func telemtPublicAddr(ip string) (string, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", false
	}
	if parsed.To4() == nil {
		return "[" + ip + "]:443", true
	}
	return ip + ":443", true
}

func (h *Handler) reconcileTelemtPublicAddr(ctx context.Context, lg *applyLog, req *agentv1.ApplyRequest, rb *telemtRollback) (bool, error) {
	wantIP := req.GetPublicIp()
	if wantIP == "" {
		return false, nil
	}
	want, ok := telemtPublicAddr(wantIP)
	if !ok {
		return false, fmt.Errorf("invalid public ip %q", wantIP)
	}
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	web, _ := cfg["web"].(map[string]any)
	vhosts, _ := web["vhosts"].([]any)
	if len(vhosts) == 0 {
		return false, errors.New("telemt config has no web vhost")
	}
	vhost, _ := vhosts[0].(map[string]any)
	cur, _ := vhost["public_addr"].(string)
	if cur == want {
		return false, nil
	}
	prev, err := copyVhosts(vhosts)
	if err != nil {
		return false, err
	}
	next, err := copyVhosts(vhosts)
	if err != nil {
		return false, err
	}
	target, _ := next[0].(map[string]any)
	target["public_addr"] = want
	if rb.prevVhosts == nil {
		rb.prevVhosts = prev
	}
	rb.publicAddrChanged = true
	if _, err := h.tm.PatchConfig(ctx, map[string]any{"web": map[string]any{"vhosts": next}}, false); err != nil {
		return true, fmt.Errorf("patch web.vhosts.public_addr: %w", err)
	}
	lg.f("web.vhosts.public_addr %s -> %s", cur, want)
	return true, nil
}

func (h *Handler) reconcileTelemtMiddleProxy(ctx context.Context, lg *applyLog, req *agentv1.ApplyRequest, rb *telemtRollback) (changed, restart bool, err error) {
	want := req.GetAdTag() != ""
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return false, false, err
	}
	general, _ := cfg["general"].(map[string]any)
	cur, _ := general["use_middle_proxy"].(bool)
	if cur == want {
		return false, false, nil
	}
	out, err := h.tm.PatchConfig(ctx, map[string]any{"general": map[string]any{"use_middle_proxy": want}}, true)
	if err != nil {
		return false, false, err
	}
	prev := cur
	rb.prevMiddleProxy = &prev
	lg.f("general.use_middle_proxy %v -> %v", cur, want)
	if len(out.DeferredProcessFields) > 0 {
		lg.f("telemt deferred %s until a process restart", strings.Join(out.DeferredProcessFields, ", "))
	}
	return true, out.RestartRequired || out.ProcessRestartRequired, nil
}

// restartTelemt restarts the unit and waits for the control API to report ready again.
func (h *Handler) restartTelemt(ctx context.Context) error {
	if out, err := h.exec.Run(ctx, "systemctl", "restart", "telemt"); err != nil {
		return fmt.Errorf("restart telemt: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return h.waitTelemtReady(ctx)
}

func (h *Handler) applyTelemt(ctx context.Context, req *agentv1.ApplyRequest) *agentv1.ApplyResult {
	lg := &applyLog{}
	res := &agentv1.ApplyResult{}
	fail := func(err error) *agentv1.ApplyResult {
		lg.f("error: %v", err)
		res.Ok, res.Log = false, lg.b.String()
		return res
	}
	if h.tm == nil {
		return fail(errors.New("telemt engine is not configured"))
	}

	var desired []telemtDesired
	if req.ApplyProfiles {
		var err error
		if desired, err = telemtDesiredFrom(req); err != nil {
			return fail(err)
		}
	}
	if len(req.MtproxySecrets) > 0 {
		lg.f("note: telemt serves Fake-TLS from the same users; mtproxy secrets ignored")
	}

	newSite, siteChanged, err := h.telemtSitePlan(req)
	if err != nil {
		return fail(err)
	}

	rb := &telemtRollback{}
	rollback := func(cause error) *agentv1.ApplyResult {
		lg.f("error: %v", cause)
		lg.f("rolling back")
		restored := len(rb.irreversible) == 0
		if !restored {
			lg.f("cannot be undone: %s", strings.Join(rb.irreversible, "; "))
		}
		step := func(desc string, err error) {
			if err != nil {
				lg.f("rollback step failed: %s: %v", desc, err)
				restored = false
			}
		}
		if rb.prevVhosts != nil {
			patch := map[string]any{"web": map[string]any{"vhosts": rb.prevVhosts}}
			if _, err := h.tm.PatchConfig(ctx, patch, false); err != nil {
				step("restore web profiles", err)
			}
		}
		if rb.prevMiddleProxy != nil {
			patch := map[string]any{"general": map[string]any{"use_middle_proxy": *rb.prevMiddleProxy}}
			if _, err := h.tm.PatchConfig(ctx, patch, true); err != nil {
				step("restore general.use_middle_proxy", err)
			}
		}
		for _, name := range rb.created {
			// telemt answers 409 last_user_forbidden when a delete would empty [access.users].
			step("delete user "+name, h.tm.DeleteUser(ctx, name))
		}
		if rb.siteBackup != "" {
			step("restore decoy site", h.swapSiteDir(rb.siteBackup))
			if _, err := h.tm.Reload(ctx, telemt.ReloadRequest{}); err != nil {
				step("reload after site restore", err)
			}
		}
		restart := rb.publicAddrChanged
		if rb.prevListeners != nil {
			if _, err := h.tm.PatchConfig(ctx, rb.prevListeners, false); err != nil {
				step("restore listeners", err)
			} else {
				restart = true
			}
		}
		if restart {
			step("restart telemt after listener restore", h.restartTelemt(ctx))
		}
		res.Ok, res.RolledBack = false, restored
		if !restored {
			lg.f("rollback incomplete; the panel must re-apply the desired state")
		}
		res.Log = lg.b.String()
		return res
	}

	changed := false
	reloadNeeded := false
	if req.ApplyProfiles {
		n, reload, err := h.reconcileTelemtUsers(ctx, lg, desired, rb)
		if err != nil {
			return rollback(err)
		}
		changed = changed || n
		reloadNeeded = reloadNeeded || reload
	}

	if siteChanged {
		backup := filepath.Join(h.cfg.StateDir, "backup", time.Now().UTC().Format("20060102T150405Z")+"-"+randSuffix())
		if err := os.MkdirAll(backup, 0o700); err != nil {
			return rollback(err)
		}
		if err := copyDir(h.siteDir(), filepath.Join(backup, "site")); err != nil {
			return rollback(fmt.Errorf("backup: %w", err))
		}
		if err := h.writeSiteDir(newSite); err != nil {
			return rollback(err)
		}
		rb.siteBackup = filepath.Join(backup, "site")
		lg.f("decoy site deployed (%d files)", len(newSite))
		changed, reloadNeeded = true, true
	}

	listenersChanged, err := h.reconcileTelemtListeners(ctx, lg, req, rb)
	if err != nil {
		return rollback(err)
	}
	addrChanged, err := h.reconcileTelemtPublicAddr(ctx, lg, req, rb)
	if err != nil {
		return rollback(err)
	}
	mpChanged, mpRestart, err := h.reconcileTelemtMiddleProxy(ctx, lg, req, rb)
	if err != nil {
		return rollback(err)
	}
	restartNeeded := listenersChanged || addrChanged || mpRestart
	changed = changed || restartNeeded || mpChanged

	if !changed {
		lg.f("nothing changed (no restart)")
		res.Ok, res.Log = true, lg.b.String()
		return res
	}

	if restartNeeded {
		lg.f("restarting telemt: the Fake-TLS listener and the vhost address are process-owned")
		if err := h.restartTelemt(ctx); err != nil {
			return rollback(err)
		}
		res.RestartedRelay = true
		lg.f("telemt restarted and ready")
	} else {
		if reloadNeeded {
			if err := h.telemtReload(ctx, lg); err != nil {
				return rollback(err)
			}
		}
		if err := h.waitTelemtReady(ctx); err != nil {
			return rollback(err)
		}
		lg.f("telemt ready (no restart)")
	}
	if n, err := h.pruneBackups(keptBackups); err != nil {
		lg.f("backup prune failed: %v", err)
	} else if n > 0 {
		lg.f("pruned %d old backup(s), keeping the newest %d", n, keptBackups)
	}
	res.Ok, res.Log = true, lg.b.String()
	return res
}

// telemtSitePlan validates the requested bundle and reports whether the decoy differs from it.
func (h *Handler) telemtSitePlan(req *agentv1.ApplyRequest) (map[string][]byte, bool, error) {
	if req.Site == nil {
		return nil, false, nil
	}
	site := map[string][]byte{}
	for _, f := range req.Site.Files {
		p, err := safeSitePath(f.Path)
		if err != nil {
			return nil, false, err
		}
		site[p] = f.Content
	}
	if _, ok := site["index.html"]; !ok {
		return nil, false, errors.New("site bundle must contain index.html")
	}
	return site, !h.siteEquals(site), nil
}

func (h *Handler) reconcileTelemtUsers(ctx context.Context, lg *applyLog, desired []telemtDesired, rb *telemtRollback) (changed, reload bool, err error) {
	users, err := h.tm.ListUsers(ctx)
	if err != nil {
		return false, false, err
	}
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return false, false, err
	}
	vhosts, err := telemtVhosts(cfg)
	if err != nil {
		return false, false, err
	}

	existing := make(map[string]telemt.User, len(users))
	for _, u := range users {
		existing[u.Username] = u
	}
	state := readTelemtState(h.cfg.StateDir)
	next := telemtState{
		SecretHashes: make(map[string]string, len(desired)),
		LimitHashes:  make(map[string]string, len(desired)),
	}
	wanted := make(map[string]bool, len(desired))
	names := make([]string, 0, len(desired))

	for _, d := range desired {
		wanted[d.name] = true
		names = append(names, d.name)
		secretHash, policyHash := secretFingerprint(d.secret), d.policy.fingerprint()
		next.SecretHashes[d.name], next.LimitHashes[d.name] = secretHash, policyHash
		u, exists := existing[d.name]
		if !exists {
			if _, err := h.tm.CreateUser(ctx, d.policy.createRequest(d.name, d.secret)); err != nil {
				return changed, reload, err
			}
			rb.created = append(rb.created, d.name)
			changed = true
			lg.f("user %s created", d.name)
			continue
		}
		secretChanged := state.SecretHashes[d.name] != secretHash
		prev, recorded := state.LimitHashes[d.name]
		policyChanged := prev != policyHash
		if !recorded {
			policyChanged = !d.policy.matches(u)
		}
		if !secretChanged && !policyChanged {
			continue
		}
		var secret *string
		if secretChanged {
			s := d.secret
			secret = &s
		}
		if _, err := h.tm.PatchUser(ctx, d.name, d.policy.patchRequest(secret)); err != nil {
			return changed, reload, err
		}
		changed = true
		switch {
		case secretChanged && policyChanged:
			rb.irreversible = append(rb.irreversible, "secret and limits of user "+d.name)
			lg.f("user %s secret and limits updated", d.name)
		case secretChanged:
			rb.irreversible = append(rb.irreversible, "secret of user "+d.name)
			lg.f("user %s secret updated", d.name)
		default:
			rb.irreversible = append(rb.irreversible, "limits of user "+d.name)
			lg.f("user %s limits updated", d.name)
		}
	}

	vhost, _ := vhosts[0].(map[string]any)
	if !sameSet(vhostProfileUsers(vhost), names) {
		prev, err := copyVhosts(vhosts)
		if err != nil {
			return changed, reload, err
		}
		list, err := copyVhosts(vhosts)
		if err != nil {
			return changed, reload, err
		}
		target, _ := list[0].(map[string]any)
		profiles := make([]any, 0, len(names))
		for _, n := range names {
			profiles = append(profiles, map[string]any{"user": n, "secret_mode": telemtSecretMode})
		}
		target["profiles"] = profiles
		out, err := h.tm.PatchConfig(ctx, map[string]any{"web": map[string]any{"vhosts": list}}, false)
		if err != nil {
			return changed, reload, err
		}
		rb.prevVhosts = prev
		changed = true
		reload = reload || out.RuntimeReloadRequired
		lg.f("web profiles updated (%d)", len(names))
		if len(out.DeferredProcessFields) > 0 {
			lg.f("warning: telemt deferred %s until a process restart", strings.Join(out.DeferredProcessFields, ", "))
		}
	}

	for _, u := range users {
		if wanted[u.Username] {
			continue
		}
		if err := h.tm.DeleteUser(ctx, u.Username); err != nil {
			return changed, reload, err
		}
		rb.irreversible = append(rb.irreversible, "deleted user "+u.Username)
		changed = true
		lg.f("user %s deleted", u.Username)
	}

	if changed {
		if err := writeTelemtState(h.cfg.StateDir, next); err != nil {
			lg.f("warning: telemt state not recorded: %v (the next apply will re-set every secret)", err)
		}
	}
	return changed, reload, nil
}

// writeSiteDir stages the bundle next to the decoy directory and swaps it in atomically.
func (h *Handler) writeSiteDir(files map[string][]byte) error {
	staging := h.siteDir() + ".new"
	_ = os.RemoveAll(staging)
	for p, content := range files {
		full := filepath.Join(staging, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
	}
	return h.swapSiteDir(staging)
}

// reloadWait is the budget telemtReload polls for.
func (h *Handler) reloadWait() time.Duration {
	if h.cfg.ReloadWait > 0 {
		return h.cfg.ReloadWait
	}
	return DefaultReloadWait
}

func (h *Handler) telemtReload(ctx context.Context, lg *applyLog) error {
	acc, err := h.tm.Reload(ctx, telemt.ReloadRequest{})
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	budget := h.reloadWait()
	deadline := time.Now().Add(budget)
	for {
		st, err := h.tm.ReloadStatus(ctx, acc.ReloadID)
		if err != nil {
			return fmt.Errorf("reload status: %w", err)
		}
		if st.Terminal() {
			if st.State != "succeeded" {
				return fmt.Errorf("reload %d %s: %s", acc.ReloadID, st.State, st.Error)
			}
			lg.f("runtime reload %d succeeded (drain, no restart)", acc.ReloadID)
			if len(st.DeferredProcessFields) > 0 {
				lg.f("warning: telemt deferred %s until a process restart", strings.Join(st.DeferredProcessFields, ", "))
			}
			return nil
		}
		if time.Now().After(deadline) {
			if st.State == "draining" {
				lg.f("reload %d still draining after %s, new generation active", acc.ReloadID, budget)
				return nil
			}
			return fmt.Errorf("reload %d did not finish in time (state %s)", acc.ReloadID, st.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (h *Handler) waitTelemtReady(ctx context.Context) error {
	deadline := time.Now().Add(h.cfg.HealthWait)
	var last error
	for {
		rd, err := h.tm.Ready(ctx)
		if err == nil && rd.Ready {
			return nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("not ready: %s", rd.Reason)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("telemt did not become ready in time: %w", last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// healthTelemt reports a telemt node.
func (h *Handler) healthTelemt(ctx context.Context) *agentv1.HealthReport {
	rep := &agentv1.HealthReport{
		RelayActive: h.unitActive(ctx, "telemt"), MtproxyActive: true, CaddyActive: h.unitActive(ctx, "caddy"),
		AgentVersion:  Version,
		UptimeSeconds: readUptime(), CpuPercent: readLoadPercent(), MemUsedPercent: readMemPercent(),
		DiskUsedPercent: diskPercent(h.siteDir()),
	}
	if h.tm == nil {
		return rep
	}
	if _, err := h.tm.Health(ctx); err == nil {
		rep.Healthz = true
	}
	if rd, err := h.tm.Ready(ctx); err == nil {
		rep.Readyz = rd.Ready
	}
	if info, err := h.tm.SystemInfo(ctx); err == nil && info.Version != "" {
		rep.TproxyVersion = "telemt " + info.Version
	}
	if users, err := h.tm.ListUsers(ctx); err == nil {
		rep.ProfileCount = int32(len(users))
	}
	if st, err := h.tm.UpstreamsStats(ctx); err == nil && st.Enabled {
		fillDcConnectivity(rep, st)
	}
	return rep
}

// fillDcConnectivity copies telemt's upstream health view into the report.
func fillDcConnectivity(rep *agentv1.HealthReport, st telemt.UpstreamsStats) {
	rep.DcDataAvailable = true
	rep.ConnectSuccessTotal, rep.ConnectFailTotal = st.Zero.ConnectSuccessTotal, st.Zero.ConnectFailTotal
	if len(st.Upstreams) == 0 {
		return
	}
	u := st.Upstreams[0]
	rep.UpstreamHealthy, rep.UpstreamFails = u.Healthy, int32(u.Fails)
	rep.EffectiveLatencyMs, rep.UpstreamLastCheckAgeSecs = u.EffectiveLatencyMs, u.LastCheckAgeSecs
	for _, d := range u.DC {
		dc := &agentv1.DcLatency{Dc: int32(d.DC), IpPreference: d.IPPreference}
		if d.LatencyEmaMs != nil {
			dc.Known, dc.LatencyMs = true, *d.LatencyEmaMs
		}
		rep.Dcs = append(rep.Dcs, dc)
	}
}

// telemtProfiles reports the users telemt has, so the panel's GetProfiles works on both engines.
func (h *Handler) telemtProfiles(ctx context.Context) ([]*agentv1.Profile, error) {
	if h.tm == nil {
		return nil, errors.New("telemt engine is not configured")
	}
	users, err := h.tm.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*agentv1.Profile, 0, len(users))
	for _, u := range users {
		out = append(out, &agentv1.Profile{Name: u.Username})
	}
	return out, nil
}

func (h *Handler) telemtStats(ctx context.Context) (map[string]string, error) {
	if h.tm == nil {
		return nil, errors.New("telemt engine is not configured")
	}
	users, err := h.tm.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{"users": strconv.Itoa(len(users))}
	for _, u := range users {
		out["user."+u.Username+".connections"] = strconv.FormatUint(u.CurrentConnections, 10)
		out["user."+u.Username+".octets"] = strconv.FormatUint(u.TotalOctets, 10)
		out["user."+u.Username+".active_ips"] = strconv.FormatUint(u.ActiveUniqueIPs, 10)
	}
	if sum, err := h.tm.ConnectionsSummary(ctx); err == nil && sum.Data != nil {
		out["connections_total"] = strconv.FormatUint(sum.Data.Totals.CurrentConnections, 10)
		out["active_users"] = strconv.Itoa(sum.Data.Totals.ActiveUsers)
		for _, row := range sum.Data.Top.ByConnections {
			out["user."+row.Username+".connections"] = strconv.FormatUint(row.CurrentConnections, 10)
			if row.TotalOctets > 0 {
				out["user."+row.Username+".octets"] = strconv.FormatUint(row.TotalOctets, 10)
			}
		}
		for _, row := range sum.Data.Top.ByThroughput {
			if row.TotalOctets > 0 {
				out["user."+row.Username+".octets"] = strconv.FormatUint(row.TotalOctets, 10)
			}
		}
	}
	return out, nil
}
