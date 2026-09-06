package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type profileJSON struct {
	Name        string          `json:"name"`
	Secret      string          `json:"secret"`
	Backend     string          `json:"backend"`
	CarrierMode string          `json:"carrier_mode,omitempty"`
	Limits      json.RawMessage `json:"limits,omitempty"`
}

func limitsJSON(l *agentv1.ProfileLimits) json.RawMessage {
	if l == nil {
		return nil
	}
	m := map[string]int32{}
	set := func(k string, v int32) {
		if v > 0 {
			m[k] = v
		}
	}
	set("max_sessions", l.MaxSessions)
	set("max_streams", l.MaxStreams)
	set("max_backend_dials_in_flight", l.MaxBackendDialsInFlight)
	set("new_sessions_per_minute", l.NewSessionsPerMinute)
	set("new_sessions_burst", l.NewSessionsBurst)
	set("new_streams_per_minute", l.NewStreamsPerMinute)
	set("new_streams_burst", l.NewStreamsBurst)
	set("max_streams_per_session", l.MaxStreamsPerSession)
	set("max_pending_per_session", l.MaxPendingPerSession)
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

// RenderProfilesJSON produces the relay profiles file (compact JSON).
func RenderProfilesJSON(ps []*agentv1.Profile) ([]byte, error) {
	if len(ps) == 0 {
		return nil, errors.New("at least one profile is required")
	}
	out := struct {
		Profiles []profileJSON `json:"profiles"`
	}{}
	for _, p := range ps {
		backend := p.Backend
		if backend == "" {
			backend = "127.0.0.1:2398"
		}
		out.Profiles = append(out.Profiles, profileJSON{Name: p.Name, Secret: p.Secret, Backend: backend, CarrierMode: p.CarrierMode, Limits: limitsJSON(p.Limits)})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// existingMTProxySecrets recovers the secret list currently in effect from an mtproxy.env file, so a
// re-apply of the same secrets can be recognised as a no-op regardless of the file's prior format.
func existingMTProxySecrets(env []byte) []string {
	for _, line := range strings.Split(string(env), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "MTPROXY_SECRETS="); ok {
			var out []string
			for _, tok := range strings.Fields(v) {
				if tok == "-S" {
					continue
				}
				out = append(out, tok)
			}
			return out
		}
	}
	for _, line := range strings.Split(string(env), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "MTPROXY_SECRET="); ok && v != "" {
			return []string{v}
		}
	}
	return nil
}

// RenderMTProxyEnv keeps non-secret lines from the existing env and rewrites secret lines.
func RenderMTProxyEnv(existing []byte, secrets []string) []byte {
	var buf bytes.Buffer
	for _, line := range strings.Split(string(existing), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "MTPROXY_SECRET=") || strings.HasPrefix(t, "MTPROXY_SECRETS=") {
			continue
		}
		buf.WriteString(line + "\n")
	}
	if !bytes.Contains(buf.Bytes(), []byte("MTPROXY_WORKERS=")) {
		buf.WriteString("MTPROXY_WORKERS=1\n")
	}
	if !bytes.Contains(buf.Bytes(), []byte("MTPROXY_MAX_CONNECTIONS=")) {
		buf.WriteString("MTPROXY_MAX_CONNECTIONS=4096\n")
	}
	first := ""
	if len(secrets) > 0 {
		first = secrets[0]
	}
	buf.WriteString("MTPROXY_SECRET=" + first + "\n")
	flags := make([]string, 0, len(secrets))
	for _, s := range secrets {
		flags = append(flags, "-S "+s)
	}
	buf.WriteString("MTPROXY_SECRETS=" + strings.Join(flags, " ") + "\n")
	return buf.Bytes()
}

type applyLog struct{ b strings.Builder }

func (l *applyLog) f(format string, a ...any) { l.b.WriteString(fmt.Sprintf(format, a...) + "\n") }

// chownLogged records a failed ownership change in the apply log. The apply
// continues: the files are already written with the right mode and root owns
// them, so a missing group only weakens defence in depth.
func (h *Handler) chownLogged(ctx context.Context, lg *applyLog, path, owner string) {
	if err := h.chown(ctx, path, owner); err != nil {
		lg.f("warning: %v (continuing; file is root-owned with the correct mode)", err)
	}
}

// Apply validates, backs up, writes, restarts and verifies; on failure it restores the backup.
func (h *Handler) Apply(ctx context.Context, req *agentv1.ApplyRequest) *agentv1.ApplyResult {
	if h.cfg.Engine == EngineTelemt {
		return h.applyTelemt(ctx, req)
	}
	lg := &applyLog{}
	res := &agentv1.ApplyResult{}
	fail := func(err error) *agentv1.ApplyResult {
		lg.f("error: %v", err)
		res.Ok = false
		res.Log = lg.b.String()
		return res
	}

	// The timestamp has one-second granularity, so two applies in the same second would
	// share a directory and the second would silently overwrite the first's backup. The
	// random suffix keeps them distinct.
	backupDir := filepath.Join(h.cfg.StateDir, "backup", time.Now().UTC().Format("20060102T150405Z")+"-"+randSuffix())
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fail(err)
	}

	var newProfiles, newEnv []byte
	profilesChanged, envChanged := false, false
	if req.ApplyProfiles {
		var err error
		if newProfiles, err = RenderProfilesJSON(req.Profiles); err != nil {
			return fail(err)
		}
		oldProfiles, _ := os.ReadFile(h.cfg.ProfilesPath)
		profilesChanged = !bytes.Equal(bytes.TrimRight(oldProfiles, "\n"), bytes.TrimRight(newProfiles, "\n"))
		oldEnv, _ := os.ReadFile(h.cfg.MTProxyEnvPath)
		newEnv = RenderMTProxyEnv(oldEnv, req.MtproxySecrets)
		canonicalOldEnv := RenderMTProxyEnv(oldEnv, existingMTProxySecrets(oldEnv))
		envChanged = !bytes.Equal(canonicalOldEnv, newEnv)
		if profilesChanged {
			candidate := filepath.Join(h.cfg.StateDir, "candidate-profiles.json")
			if err := writeAtomic(candidate, newProfiles, 0o600); err != nil {
				return fail(err)
			}
			out, err := h.exec.Run(ctx, h.cfg.TProxyBin, "-config", h.cfg.ConfigPath, "-profiles-file", candidate, "-check")
			_ = os.Remove(candidate)
			if err != nil {
				return fail(fmt.Errorf("relay -check rejected profiles: %s", strings.TrimSpace(string(out))))
			}
			lg.f("relay -check ok")
		}
	}

	var newSite map[string][]byte
	siteChanged := false
	if req.Site != nil {
		newSite = map[string][]byte{}
		for _, f := range req.Site.Files {
			p, err := safeSitePath(f.Path)
			if err != nil {
				return fail(err)
			}
			newSite[p] = f.Content
		}
		if _, ok := newSite["index.html"]; !ok {
			return fail(errors.New("site bundle must contain index.html"))
		}
		siteChanged = !h.siteEquals(newSite)
	}

	if !profilesChanged && !envChanged && !siteChanged {
		lg.f("nothing changed")
		res.Ok, res.Log = true, lg.b.String()
		return res
	}

	// Backup current state. A read error for a file that exists is fatal — a backup we could not
	// read is not usable for rollback, so abort before any mutation. A missing file is fine to
	// skip (there was nothing to back up).
	for _, p := range []string{h.cfg.ProfilesPath, h.cfg.MTProxyEnvPath} {
		data, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fail(fmt.Errorf("backup: %w", err))
		}
		if err := os.WriteFile(filepath.Join(backupDir, filepath.Base(p)), data, 0o600); err != nil {
			return fail(fmt.Errorf("backup: %w", err))
		}
	}
	if siteChanged {
		if err := copyDir(h.siteDir(), filepath.Join(backupDir, "site")); err != nil {
			return fail(fmt.Errorf("backup: %w", err))
		}
	}
	lg.f("backup written to %s", backupDir)

	// rollback restores the pre-apply backup and reports whether every restore step actually
	// succeeded. RolledBack must only be true when the node was genuinely returned to its prior
	// state — a silently-discarded restore error must never be reported as a successful rollback.
	rollback := func(cause error) *agentv1.ApplyResult {
		lg.f("rolling back: %v", cause)
		restored := true
		step := func(desc string, err error) {
			if err != nil {
				lg.f("rollback step failed: %s: %v", desc, err)
				restored = false
			}
		}

		if data, err := os.ReadFile(filepath.Join(backupDir, filepath.Base(h.cfg.ProfilesPath))); err == nil {
			if err := writeAtomic(h.cfg.ProfilesPath, data, 0o400); err != nil {
				step("restore profiles.json", err)
			} else {
				h.chownLogged(ctx, lg, h.cfg.ProfilesPath, "root:tproxy")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			step("read profiles.json backup", err)
		}
		if data, err := os.ReadFile(filepath.Join(backupDir, filepath.Base(h.cfg.MTProxyEnvPath))); err == nil {
			if err := writeAtomic(h.cfg.MTProxyEnvPath, data, 0o640); err != nil {
				step("restore mtproxy.env", err)
			} else {
				h.chownLogged(ctx, lg, h.cfg.MTProxyEnvPath, "root:mtproxy")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			step("read mtproxy.env backup", err)
		}
		if siteChanged {
			step("restore site", h.swapSiteDir(filepath.Join(backupDir, "site")))
		}
		if envChanged {
			if out, err := h.exec.Run(ctx, "systemctl", "restart", "mtproxy"); err != nil {
				step("restart mtproxy", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err))
			}
		}
		if out, err := h.exec.Run(ctx, "systemctl", "restart", "tproxy-server"); err != nil {
			step("restart tproxy-server", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err))
		}
		// Health is re-checked best-effort for diagnostics only: it depends on the external
		// admin endpoint and does not by itself indicate whether the restore succeeded, so a
		// failure here is logged but does not flip restored to false.
		if err := h.waitHealthy(ctx); err != nil {
			lg.f("rollback step failed: wait healthy: %v", err)
		}

		res.Ok, res.RolledBack = false, restored
		if !restored {
			lg.f("rollback FAILED; manual intervention needed")
		}
		res.Log = lg.b.String()
		return res
	}

	if profilesChanged {
		if err := writeAtomic(h.cfg.ProfilesPath, newProfiles, 0o400); err != nil {
			return rollback(err)
		}
		h.chownLogged(ctx, lg, h.cfg.ProfilesPath, "root:tproxy")
		lg.f("profiles.json written (%d profiles)", len(req.Profiles))
	}
	if envChanged {
		if err := writeAtomic(h.cfg.MTProxyEnvPath, newEnv, 0o640); err != nil {
			return rollback(err)
		}
		h.chownLogged(ctx, lg, h.cfg.MTProxyEnvPath, "root:mtproxy")
		lg.f("mtproxy.env written (%d secrets)", len(req.MtproxySecrets))
	}
	if siteChanged {
		if err := h.writeSiteDir(newSite); err != nil {
			return rollback(err)
		}
		lg.f("site deployed (%d files)", len(newSite))
	}

	if envChanged {
		if out, err := h.exec.Run(ctx, "systemctl", "restart", "mtproxy"); err != nil {
			return rollback(fmt.Errorf("restart mtproxy: %s", strings.TrimSpace(string(out))))
		}
		res.RestartedMtproxy = true
		lg.f("mtproxy restarted")
	}
	if out, err := h.exec.Run(ctx, "systemctl", "restart", "tproxy-server"); err != nil {
		return rollback(fmt.Errorf("restart tproxy-server: %s", strings.TrimSpace(string(out))))
	}
	res.RestartedRelay = true
	lg.f("tproxy-server restarted")
	if err := h.waitHealthy(ctx); err != nil {
		return rollback(err)
	}
	lg.f("relay healthy")
	if n, err := h.pruneBackups(keptBackups); err != nil {
		lg.f("backup prune failed: %v", err)
	} else if n > 0 {
		lg.f("pruned %d old backup(s), keeping the newest %d", n, keptBackups)
	}
	res.Ok, res.Log = true, lg.b.String()
	return res
}

// keptBackups is how many per-apply backup directories survive a successful apply. Each one
// can hold a full copy of the site tree, on the same disk the relay runs on, and nothing
// else ever deleted them.
const keptBackups = 5

func randSuffix() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Only reachable if the OS RNG fails; the timestamp alone still names the directory.
		return "0000"
	}
	return hex.EncodeToString(b[:])
}

// pruneBackups removes all but the newest keep backup directories and reports how many it
// deleted. Directory names sort chronologically (RFC3339-ish UTC timestamp + suffix).
func (h *Handler) pruneBackups(keep int) (int, error) {
	root := filepath.Join(h.cfg.StateDir, "backup")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return 0, nil
	}
	sort.Strings(names)
	removed := 0
	var firstErr error
	for _, name := range names[:len(names)-keep] {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

func (h *Handler) siteEquals(files map[string][]byte) bool {
	current := h.readSite()
	if len(current) != len(files) {
		return false
	}
	for p, c := range files {
		if !bytes.Equal(current[p], c) {
			return false
		}
	}
	return true
}

func (h *Handler) readSite() map[string][]byte {
	out := map[string][]byte{}
	dir := h.siteDir()
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err == nil {
			out[filepath.ToSlash(rel)] = data
		}
		return nil
	})
	return out
}

// swapSiteDir replaces SiteDir with src (a directory) via renames.
func (h *Handler) swapSiteDir(src string) error {
	dir := h.siteDir()
	old := dir + ".old"
	_ = os.RemoveAll(old)
	moved := false
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
		moved = true
	}
	// restore puts the previous site back on any failure after the rename above.
	// Without it a failed copy leaves SiteDir missing entirely and the relay
	// serves nothing — including when swapSiteDir is itself the rollback step.
	restore := func() {
		if moved {
			_ = os.Rename(old, dir)
		}
	}
	tmp := dir + ".swap"
	_ = os.RemoveAll(tmp)
	if err := copyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		restore()
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		restore()
		return err
	}
	_ = os.RemoveAll(old)
	if strings.HasSuffix(src, ".new") {
		_ = os.RemoveAll(src)
	}
	return nil
}

func (h *Handler) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(h.cfg.HealthWait)
	for {
		if h.probe(ctx, "/healthz") && h.probe(ctx, "/readyz") {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("relay did not become healthy in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (h *Handler) probe(ctx context.Context, path string) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.RelayAdminURL+path, nil)
	resp, err := h.httpc.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == 200
}
