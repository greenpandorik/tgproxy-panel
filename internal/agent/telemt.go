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

// telemtDesired is one panel-managed key as it must exist on the node: its secret plus the
// policy (limits, expiry, enabled) the panel wants telemt to enforce for it.
type telemtDesired struct {
	name   string
	secret string
	policy telemtPolicy
}

// telemtPolicy is the per-user state the panel owns, in telemt's own units. A zero numeric
// field means "no limit of this kind": telemt expresses that as the absence of the per-user
// override, so it is pushed as an explicit null, never as 0 (which would be a real limit of
// zero). Expiration is empty when the key never expires.
type telemtPolicy struct {
	DataQuotaBytes   uint64 `json:"data_quota_bytes"`
	RateLimitUpBps   uint64 `json:"rate_limit_up_bps"`
	RateLimitDownBps uint64 `json:"rate_limit_down_bps"`
	MaxUniqueIPs     uint64 `json:"max_unique_ips"`
	MaxTCPConns      uint64 `json:"max_tcp_conns"`
	Expiration       string `json:"expiration_rfc3339"`
	Enabled          bool   `json:"enabled"`
	// AdTag is not part of the panel's per-profile policy: it is the node-level sponsor-channel
	// tag (ApplyRequest.AdTag), copied onto every profile's policy identically because telemt
	// expresses it per user. Empty means "no sponsor channel", not "no opinion".
	AdTag string `json:"ad_tag"`
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

// fingerprint is the value recorded in the state file: it changes exactly when the pushed
// policy changes, so a steady state costs no API writes.
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

// patchRequest sends the whole policy, not just the differing fields: telemt applies a JSON
// merge patch, so listing every field is what makes the node converge on the panel's state
// instead of keeping an override the panel has since dropped.
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

// matches reports whether telemt already holds exactly this policy. It is the fallback for
// the one case the fingerprint file cannot answer - no record for this user yet, after an
// agent upgrade or a lost state file - so an unchanged node is not re-patched needlessly.
// Timestamps are compared as instants: telemt is free to normalise the offset it echoes back.
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

// telemtState remembers what each user was last given. The control API never returns a secret
// in a list view, so without SecretHashes the agent could not tell a rotated key from an
// unchanged one and would re-PATCH every user on every apply. LimitHashes does the same for
// the per-user policy, which the API does report but only field by field. Only fingerprints
// are stored - never a secret.
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

// readTelemtState returns the last recorded fingerprints, with both maps always non-nil. A
// missing or unreadable file yields empty maps, which makes the next apply re-set every secret
// — wrong-but-safe, never stale.
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

// copyVhosts clones the decoded vhost array so the patch we build cannot alias the snapshot we
// keep for rollback.
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
	created    []string // users this apply created
	prevVhosts []any    // web.vhosts as they were before the profile patch (nil = not patched)
	siteBackup string   // directory holding the previous decoy site ("" = site untouched)
	// prevListeners is a ready-made config patch that puts `[censorship] tls_domain`,
	// `[[server.listeners]]` and `[general.links] public_port` back exactly as they were
	// (nil = the listeners were not touched). It is built before the first listener patch
	// goes out, so a failure half-way through still restores every field.
	prevListeners map[string]any
	// publicAddrChanged records that web.vhosts[0].public_addr was rewritten; the vhost array
	// itself goes back through prevVhosts, but a restart is needed for it to take effect.
	publicAddrChanged bool
	// prevMiddleProxy is general.use_middle_proxy as it was before this apply touched it
	// (nil = not touched).
	prevMiddleProxy *bool
	irreversible    []string // mutations telemt cannot undo from the information we hold
}

// telemtFakeTLSListener returns the index of the Fake-TLS listener inside `server.listeners`.
// telemt marks the loopback WEB listener with `transport = "web"`; the Fake-TLS one carries
// either no transport at all (the panel's own template) or the explicit `mtproxy`.
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

// jsonNumber reads a number out of a decoded JSON object. encoding/json gives float64 for a
// plain decode and json.Number when the decoder is set to use it, so both are accepted.
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

// cloneJSON copies a decoded JSON value so a patch we build cannot alias the snapshot kept for
// rollback.
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

// reconcileTelemtListeners rewrites the node's Fake-TLS listener when the panel's tls_domain or
// classic_port differ from what telemt currently holds.
//
// Both are editable through the control API - `censorship` is an editable section and
// `server.listeners` is the one nested field allowed under `server` - but a listener move is
// process-owned: telemt persists it and reports it as deferred until the process restarts. The
// caller therefore restarts telemt when this returns true. An empty domain and a zero port mean
// "the panel has no opinion" (a tproxy node, or an older panel), and nothing is touched.
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

	// The undo patch is assembled first: after this point every field we are about to change
	// can be put back, even if the second patch is the one that fails.
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
		// A table deep-merges, so only the one field travels; the rest of [censorship]
		// (mask, unknown_sni_action, tls_emulation) is left exactly as it is.
		if _, err := h.tm.PatchConfig(ctx, map[string]any{"censorship": map[string]any{"tls_domain": wantDomain}}, false); err != nil {
			return true, fmt.Errorf("patch censorship.tls_domain: %w", err)
		}
		lg.f("censorship.tls_domain %s -> %s", curDomain, wantDomain)
	}
	if portChanged {
		// Arrays replace wholesale, so the whole listener array goes back - including the
		// loopback WEB listener, which must survive the move untouched.
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
			// [general.links] public_port is what telemt puts into the links it prints;
			// leaving it behind would make its own output disagree with the listener.
			if _, err := h.tm.PatchConfig(ctx, map[string]any{"general": map[string]any{"links": map[string]any{"public_port": wantPort}}}, false); err != nil {
				return true, fmt.Errorf("patch general.links.public_port: %w", err)
			}
			lg.f("general.links.public_port -> %d", wantPort)
		}
	}
	return true, nil
}

// telemtPublicAddr is the WEB vhost's public socket address for a panel-supplied IP; IPv6
// needs brackets. The second value is false when the address is not an IP at all.
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

// reconcileTelemtPublicAddr rewrites `web.vhosts[0].public_addr` when the panel's public IP
// differs from the address telemt currently holds. The panel is where an operator corrects
// the address after the installer detected the wrong side of a NAT, and telemt names it in
// the vhost. An empty value means "the panel has no opinion" (a tproxy node, or an older
// panel), and nothing is touched. Arrays replace wholesale, so the whole vhost list goes back
// with only the one field changed; the previous list is kept for rollback unless the profile
// step already recorded an earlier one, which restores this field too. The caller restarts
// telemt when this returns true, the same way it does for a listener move.
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

// reconcileTelemtMiddleProxy turns telemt's middle-proxy mode on or off to match whether the
// panel wants a sponsor channel on this node. Unlike the listener/address reconcilers, empty is
// authoritative here ("no sponsor channel"), not "no opinion" - so this always compares against
// the node's current setting, on a tproxy node included (which will simply never differ, since
// the panel never sets AdTag for one).
//
// `general` is unlike `censorship`/`server.listeners`/`web.vhosts`: nothing here says it is
// process-owned, so this asks for an immediate draining reload and then trusts telemt's own
// answer (restart_required / process_restart_required / deferred_process_fields) rather than
// assuming either way. If telemt does defer it, the caller restarts the same way a listener
// move would.
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

// restartTelemt restarts the unit and waits for the control API to report ready again. A
// listener move is process-owned, so the config patch alone would leave telemt answering on the
// old socket until something else restarted it.
func (h *Handler) restartTelemt(ctx context.Context) error {
	if out, err := h.exec.Run(ctx, "systemctl", "restart", "telemt"); err != nil {
		return fmt.Errorf("restart telemt: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return h.waitTelemtReady(ctx)
}

// applyTelemt reconciles the node through the control API: users, then WEB profiles, then the
// removals, then the decoy site, then the Fake-TLS listener. Only the last of those restarts
// anything — telemt applies vhosts and profiles from its config watcher and a changed decoy
// snapshot needs no more than a draining runtime reload, but a listener move is process-owned
// and telemt defers it until the process comes back.
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
		// Profiles first: telemt rejects a config that references a missing user, so the
		// created users may only go after the profile list no longer mentions them.
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
			// telemt answers 409 last_user_forbidden when a delete would empty
			// [access.users]. It cannot happen here: the desired set the panel sends
			// always contains the node's own `default` profile, which this apply did not
			// create and therefore never deletes.
			step("delete user "+name, h.tm.DeleteUser(ctx, name))
		}
		if rb.siteBackup != "" {
			step("restore decoy site", h.swapSiteDir(rb.siteBackup))
			if _, err := h.tm.Reload(ctx, telemt.ReloadRequest{}); err != nil {
				step("reload after site restore", err)
			}
		}
		// Listeners last: putting them back needs a restart, which also re-reads everything
		// the steps above have already written. A restored public_addr needs that restart
		// too, even when no listener moved.
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

	// The Fake-TLS listener is the one thing the panel can change that telemt cannot apply
	// without a restart, so it is reconciled last: everything above is already on disk when
	// the process comes back.
	listenersChanged, err := h.reconcileTelemtListeners(ctx, lg, req, rb)
	if err != nil {
		return rollback(err)
	}
	// The WEB vhost's public address is treated the same way: it is what telemt advertises
	// for the vhost, and a restart is the one way to be sure a changed value is in effect.
	addrChanged, err := h.reconcileTelemtPublicAddr(ctx, lg, req, rb)
	if err != nil {
		return rollback(err)
	}
	// The sponsor channel toggle is not process-owned, but telemt's own answer decides whether
	// this apply still needs the restart the other two might already be asking for.
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
		// A restart re-reads the whole config, so it supersedes the runtime reload the rest
		// of this apply would otherwise have asked for.
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

// reconcileTelemtUsers creates and updates users, rewrites the vhost profile list and removes
// the users the panel no longer wants, in the order telemt's config validation requires. It
// reports whether anything changed and whether telemt asked for a runtime reload.
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
		// A recorded fingerprint settles the policy without a comparison; without one (agent
		// upgrade, lost state file) telemt's own report of the user decides, so a node that
		// already holds the wanted limits is not patched for nothing.
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

	// The profile list must be rewritten before any delete: telemt validates the complete
	// resulting config and refuses one whose WEB profile points at a missing user.
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
		// Arrays replace wholesale, so the whole vhost object goes back including host,
		// public_addr and decoy.
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

// reloadWait is the budget telemtReload polls for. It is its own value rather than HealthWait
// because a draining reload is non-terminal for up to telemt.ReloadDrainSecs; a zero (a Config
// built by hand, as tests do) falls back to the default so the budget can never be shorter
// than the drain window by accident.
func (h *Handler) reloadWait() time.Duration {
	if h.cfg.ReloadWait > 0 {
		return h.cfg.ReloadWait
	}
	return DefaultReloadWait
}

// telemtReload activates a new runtime generation, draining the old one so live sessions are
// not cut, and waits for the operation to reach a terminal state.
//
// A reload that is still `draining` when the budget runs out is *not* a failure. Activation
// precedes the drain (API.md: "activates a new generation and lets old sessions finish until
// timeout_secs"), so by then the new generation is already serving and the only thing still
// outstanding is the old generation's sessions winding down. Rolling back at that point would
// reverse a change telemt has already applied. Only a terminal non-success state - `failed`,
// `rolled_back`, a cancelled operation - means the generation never took, and only those are
// reported as an error for the caller to roll back.
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

// healthTelemt reports a telemt node. MtproxyActive is always true: telemt serves Fake-TLS and
// WEB from one process, so there is no separate MTProxy leg to be down, and the panel's node
// health must not read that as a fault.
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
	// The version is left empty when the API cannot be reached, so the panel is never told a
	// made-up version for a node whose telemt is simply unreachable.
	if info, err := h.tm.SystemInfo(ctx); err == nil && info.Version != "" {
		rep.TproxyVersion = "telemt " + info.Version
	}
	if users, err := h.tm.ListUsers(ctx); err == nil {
		rep.ProfileCount = int32(len(users))
	}
	// DC connectivity is best-effort: a failed call (or telemt not tracking upstreams) leaves
	// the fields zero with dc_data_available=false and never costs the heartbeat itself.
	if st, err := h.tm.UpstreamsStats(ctx); err == nil && st.Enabled {
		fillDcConnectivity(rep, st)
	}
	return rep
}

// fillDcConnectivity copies telemt's upstream health view into the report. The panel only
// configures one (direct) route, so the first upstream is the one described; a DC whose EMA is
// still null is reported with known=false rather than a latency of 0.
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
// Secrets are never returned by the list endpoint, so the profiles carry names only.
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

// telemtStats flattens the user list and the runtime-edge summary into the engine-agnostic
// stats map the panel already consumes.
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
		// active_unique_ips is the only source for a key's IP spread: the Prometheus endpoint
		// caps its per-user series, and the summary view carries no IP counts at all.
		out["user."+u.Username+".active_ips"] = strconv.FormatUint(u.ActiveUniqueIPs, 10)
	}
	// The summary is best-effort: it is disabled when runtime_edge_enabled is off, and the
	// per-user rows there are a top-N view that can be fresher than the disk-first user list.
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
