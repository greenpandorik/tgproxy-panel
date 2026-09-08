package agent

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tgwebproxy/internal/telemt"
)

// Node self-upgrade: the acting half.
//
// Every replacement follows the same shape, which is what makes it safe to run unattended on
// a live node: download to a temp file, verify the panel's sha256 (a mismatch aborts before
// anything on disk is touched), keep the previous binary, install atomically, restart the
// unit, wait for it to prove itself, and on failure put the previous binary back and restart
// it. The rollback is a rename of a file that is already on disk, so it works even when the
// download that broke things is no longer reachable.

// UpgradeOptions configures RunUpgrade. Everything with a system default is overridable so
// the whole flow can be exercised in a temp directory with a fake systemctl.
type UpgradeOptions struct {
	EnvPath string
	Scope   UpgradeScope
	// Check reports and changes nothing.
	Check bool
	// Yes skips the confirmation prompt. Without a terminal it is required.
	Yes bool

	Out, Err io.Writer
	// In is the prompt's input; nil means os.Stdin. TTY decides whether there is anybody to
	// ask at all and whether to colour the output.
	In  *os.File
	TTY *bool

	HTTP *http.Client
	Exec Exec
	Env  func(string) string

	AgentBin, TelemtBin string
	// HealthWait bounds each post-restart health wait (the installer gives telemt 60s).
	HealthWait time.Duration
	// Sleep is the poll delay; tests pass a no-op.
	Sleep func(time.Duration)
}

func (o *UpgradeOptions) withDefaults() {
	if o.EnvPath == "" {
		o.EnvPath = DefaultAgentEnvPath
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.Err == nil {
		o.Err = os.Stderr
	}
	if o.In == nil {
		o.In = os.Stdin
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	if o.Exec == nil {
		o.Exec = OSExec{}
	}
	if o.Env == nil {
		o.Env = os.Getenv
	}
	if o.AgentBin == "" {
		o.AgentBin = DefaultAgentBin
	}
	if o.TelemtBin == "" {
		o.TelemtBin = DefaultTelemtBin
	}
	if o.HealthWait == 0 {
		o.HealthWait = 60 * time.Second
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.TTY == nil {
		tty := isTTY(o.In) && isTTY(stdoutOf(o.Out))
		o.TTY = &tty
	}
}

// stdoutOf returns w as an *os.File when it is one, so colour detection looks at the real
// stdout rather than a test buffer.
func stdoutOf(w io.Writer) *os.File {
	f, _ := w.(*os.File)
	return f
}

type upgrader struct {
	o   UpgradeOptions
	ui  *ui
	env map[string]string
	// telemtToken is telemt's control-API authorization value. It is read once, before
	// anything is replaced, and never printed.
	telemtToken string
}

// RunUpgrade is `tgwp-agent upgrade`.
func RunUpgrade(ctx context.Context, o UpgradeOptions) error {
	o.withDefaults()
	u := &upgrader{o: o, ui: newUI(o.Out, o.Err, *o.TTY, o.Env)}
	return u.run(ctx)
}

func (u *upgrader) run(ctx context.Context) error {
	u.ui.step("Node upgrade")

	env, err := ReadEnvFile(u.o.EnvPath)
	if err != nil {
		u.ui.fail("cannot read %s: %v", u.o.EnvPath, err)
		u.ui.info("run this as root on the node, or point --env at the agent's env file")
		return err
	}
	u.env = env
	panelURL, token := env["TGWP_PANEL_URL"], env["TGWP_TOKEN"]
	if panelURL == "" || token == "" {
		err := fmt.Errorf("%s has no TGWP_PANEL_URL and TGWP_TOKEN", u.o.EnvPath)
		u.ui.fail("%v", err)
		return err
	}
	engine := env["TGWP_ENGINE"]
	if engine == "" {
		engine = EngineTProxy
	}
	u.ui.ok("%s: panel %s, engine %s", u.o.EnvPath, panelURL, engine)

	m, err := FetchUpgradeManifest(ctx, u.o.HTTP, panelURL, token)
	if err != nil {
		u.ui.fail("%v", err)
		return err
	}
	u.ui.ok("the panel pins: %s", describeManifest(m))

	plan := BuildUpgradePlan(m, u.installed(ctx, m), u.o.Scope)
	u.ui.step("Plan")
	for _, c := range plan.Components {
		if c.Change {
			u.ui.warn("%-8s %s", c.Name, c.Reason)
		} else {
			u.ui.ok("%-8s %s", c.Name, c.Reason)
		}
	}
	changes := plan.Changes()
	if u.o.Check {
		if len(changes) == 0 {
			u.ui.info("--check: nothing to do")
		} else {
			u.ui.info("--check: %d component(s) would be replaced; nothing was changed", len(changes))
		}
		return nil
	}
	if len(changes) == 0 {
		u.ui.ok("everything is already at the pinned version")
		return nil
	}
	// The telemt API token is needed to prove telemt came back healthy. Read it now, while a
	// failure still costs nothing.
	for _, c := range changes {
		if c.Name == ComponentTelemt {
			if err := u.loadTelemtToken(); err != nil {
				u.ui.fail("%v", err)
				return err
			}
		}
	}
	if err := u.confirm(changes); err != nil {
		return err
	}

	for _, c := range changes {
		var err error
		switch c.Name {
		case ComponentTelemt:
			err = u.upgradeTelemt(ctx, c)
		case ComponentAgent:
			err = u.upgradeAgent(ctx, c)
		default:
			continue
		}
		if err != nil {
			return err
		}
	}
	u.ui.step("Done")
	u.ui.ok("this node is at the panel's pinned versions")
	return nil
}

func describeManifest(m UpgradeManifest) string {
	parts := []string{}
	if m.Telemt != nil {
		parts = append(parts, "telemt "+m.Telemt.Version)
	}
	if m.TProxy != nil {
		parts = append(parts, "tproxy-server "+shortCommit(m.TProxy.Commit))
	}
	parts = append(parts, "agent "+m.Agent.Version)
	return strings.Join(parts, ", ")
}

// installed reports what is on the node now. A component whose version cannot be read is
// reported as empty, which the plan treats as out of date.
func (u *upgrader) installed(ctx context.Context, m UpgradeManifest) Installed {
	out := Installed{}
	if m.Telemt != nil {
		out[ComponentTelemt] = u.installedTelemt(ctx)
	}
	if m.TProxy != nil {
		out["tproxy-server"] = strings.TrimSpace(u.env["TGWP_TPROXY_VERSION"])
	}
	out[ComponentAgent] = u.installedAgent(ctx)
	return out
}

// installedTelemt asks the binary first (`telemt --version`) and falls back to the running
// process's own answer over the control API, which is the version actually serving traffic.
func (u *upgrader) installedTelemt(ctx context.Context) string {
	if out, err := u.o.Exec.Run(ctx, u.o.TelemtBin, "--version"); err == nil {
		if v := ParseVersionOutput(string(out)); v != "" {
			return v
		}
	}
	if err := u.loadTelemtToken(); err != nil {
		return ""
	}
	info, err := u.telemtClient().SystemInfo(ctx)
	if err != nil {
		return ""
	}
	return ParseVersionOutput(info.Version)
}

// installedAgent asks the installed binary rather than assuming this process is it: the
// operator may be running a freshly downloaded copy from /tmp.
func (u *upgrader) installedAgent(ctx context.Context) string {
	if out, err := u.o.Exec.Run(ctx, u.o.AgentBin, "version"); err == nil {
		if v := ParseVersionOutput(string(out)); v != "" {
			return v
		}
	}
	return Version
}

func (u *upgrader) telemtAPI() string {
	if v := strings.TrimSpace(u.env["TGWP_TELEMT_API"]); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultTelemtAPI
}

func (u *upgrader) telemtClient() *telemt.Client {
	return telemt.New(u.telemtAPI(), u.telemtToken)
}

// loadTelemtToken reads telemt's control-API token from the env file or, as the installer
// leaves it, from /etc/telemt/api.token (0600, root-readable). The value never leaves this
// struct.
func (u *upgrader) loadTelemtToken() error {
	if u.telemtToken != "" {
		return nil
	}
	if v := strings.TrimSpace(u.env["TGWP_TELEMT_API_TOKEN"]); v != "" {
		u.telemtToken = v
		return nil
	}
	path := strings.TrimSpace(u.env["TGWP_TELEMT_API_TOKEN_FILE"])
	if path == "" {
		path = DefaultTelemtTokenPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read telemt api token %s: %w (run as root on a telemt node)", path, err)
	}
	u.telemtToken = strings.TrimSpace(string(raw))
	if u.telemtToken == "" {
		return fmt.Errorf("%s is empty", path)
	}
	return nil
}

// confirm prints the plan's consequence and asks, unless --yes. Without a terminal there is
// nobody to ask, so --yes is required rather than assumed.
func (u *upgrader) confirm(changes []ComponentPlan) error {
	if u.o.Yes {
		return nil
	}
	names := make([]string, 0, len(changes))
	for _, c := range changes {
		names = append(names, c.Name)
	}
	if !*u.o.TTY {
		err := errors.New("not a terminal: re-run with --yes (or --check to see the plan only)")
		u.ui.fail("%v", err)
		return err
	}
	u.ui.print(u.o.Out, "\n  Replace %s and restart the unit(s)? Live sessions on this node are dropped. [y/N] ", strings.Join(names, " and "))
	line, _ := bufio.NewReader(u.o.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	err := errors.New("aborted")
	u.ui.fail("aborted; nothing was changed")
	return err
}

// upgradeTelemt replaces /usr/local/bin/telemt with the pinned release and proves the new
// process serves its control API before the previous binary is discarded.
func (u *upgrader) upgradeTelemt(ctx context.Context, c ComponentPlan) error {
	u.ui.step("telemt %s", c.Wanted)
	dir := filepath.Dir(u.o.TelemtBin)
	tgz, err := u.download(ctx, c.Artifact, dir, ".telemt-download-")
	if err != nil {
		u.ui.fail("%v", err)
		return err
	}
	defer func() { _ = os.Remove(tgz) }()
	u.ui.ok("downloaded and sha256 verified (%s)", c.Artifact.SHA256[:12])

	staged := filepath.Join(dir, ".telemt.new")
	if err := extractTelemtBinary(tgz, staged); err != nil {
		_ = os.Remove(staged)
		u.ui.fail("%v", err)
		return err
	}
	defer func() { _ = os.Remove(staged) }()

	prev, err := backupBinary(u.o.TelemtBin)
	if err != nil {
		u.ui.fail("%v", err)
		return err
	}
	if err := os.Rename(staged, u.o.TelemtBin); err != nil {
		u.ui.fail("install %s: %v", u.o.TelemtBin, err)
		return err
	}
	u.ui.ok("%s installed%s", u.o.TelemtBin, keptAs(prev))

	restore := func(cause error) error {
		u.ui.fail("%v", cause)
		if err := restoreBinary(prev, u.o.TelemtBin); err != nil {
			u.ui.fail("rolling back %s failed: %v; the previous binary is still at %s", u.o.TelemtBin, err, prev)
			return cause
		}
		u.ui.warn("%s", rolledBack(prev, "telemt"))
		if out, err := u.o.Exec.Run(ctx, "systemctl", "restart", telemtUnit); err != nil {
			u.ui.fail("restarting %s after the rollback failed: %s", telemtUnit, firstLine(string(out)))
		} else {
			u.ui.ok("%s restarted with the previous binary", telemtUnit)
		}
		return cause
	}
	if out, err := u.o.Exec.Run(ctx, "systemctl", "restart", telemtUnit); err != nil {
		return restore(fmt.Errorf("systemctl restart %s: %s", telemtUnit, firstLine(string(out))))
	}
	ready := func() bool {
		rd, err := u.telemtClient().Ready(ctx)
		return err == nil && rd.Ready
	}
	if !u.ui.waitFor("telemt ready on its control API", u.o.HealthWait, u.o.Sleep, ready) {
		u.ui.info("telemt's log: journalctl -u telemt -n 30")
		return restore(fmt.Errorf("telemt %s did not report ready within %s", c.Wanted, u.o.HealthWait))
	}
	_ = os.Remove(prev)
	u.ui.ok("telemt is now %s", c.Wanted)
	return nil
}

// upgradeAgent replaces the agent binary and lets systemd restart the unit.
//
// The delicate case: a process cannot restart itself. This command is not the agent - it is a
// short-lived invocation of the same binary from the operator's shell - so `systemctl restart`
// is carried out by systemd and cannot be interrupted by this process exiting. The one way
// that stops being true is running the command from inside the unit's own cgroup, where the
// restart would kill the caller mid-flight; that is refused rather than half-done.
func (u *upgrader) upgradeAgent(ctx context.Context, c ComponentPlan) error {
	u.ui.step("tgwp-agent %s", c.Wanted)
	if insideUnit(agentUnit) {
		err := fmt.Errorf("this command is running inside %s.service; restarting the unit would kill it mid-upgrade. Run it from a login shell: %s upgrade --agent", agentUnit, u.o.AgentBin)
		u.ui.fail("%v", err)
		return err
	}
	dir := filepath.Dir(u.o.AgentBin)
	// The same name the install script stages under, in the same directory as the target, so
	// the install below is an atomic rename within one filesystem.
	staged := filepath.Join(dir, "tgwp-agent.new")
	_ = os.Remove(staged)
	tmp, err := u.download(ctx, c.Artifact, dir, ".tgwp-agent-download-")
	if err != nil {
		u.ui.fail("%v", err)
		return err
	}
	if err := os.Rename(tmp, staged); err != nil {
		_ = os.Remove(tmp)
		u.ui.fail("stage %s: %v", staged, err)
		return err
	}
	defer func() { _ = os.Remove(staged) }()
	if err := os.Chmod(staged, 0o755); err != nil {
		u.ui.fail("chmod %s: %v", staged, err)
		return err
	}
	u.ui.ok("downloaded and sha256 verified (%s)", c.Artifact.SHA256[:12])

	prev, err := backupBinary(u.o.AgentBin)
	if err != nil {
		u.ui.fail("%v", err)
		return err
	}
	if err := os.Rename(staged, u.o.AgentBin); err != nil {
		u.ui.fail("install %s: %v", u.o.AgentBin, err)
		return err
	}
	u.ui.ok("%s installed%s", u.o.AgentBin, keptAs(prev))

	since := time.Now()
	restore := func(cause error) error {
		u.ui.fail("%v", cause)
		if err := restoreBinary(prev, u.o.AgentBin); err != nil {
			u.ui.fail("rolling back %s failed: %v; the previous binary is still at %s", u.o.AgentBin, err, prev)
			return cause
		}
		u.ui.warn("%s", rolledBack(prev, "agent"))
		if out, err := u.o.Exec.Run(ctx, "systemctl", "restart", agentUnit); err != nil {
			u.ui.fail("restarting %s after the rollback failed: %s", agentUnit, firstLine(string(out)))
		} else {
			u.ui.ok("%s restarted with the previous binary", agentUnit)
		}
		return cause
	}
	if out, err := u.o.Exec.Run(ctx, "systemctl", "restart", agentUnit); err != nil {
		return restore(fmt.Errorf("systemctl restart %s: %s", agentUnit, firstLine(string(out))))
	}
	if !u.ui.waitFor("tgwp-agent active and reconnected", u.o.HealthWait, u.o.Sleep, func() bool {
		return u.agentHealthy(ctx, since)
	}) {
		u.ui.info("the agent's log: journalctl -u tgwp-agent -n 30")
		return restore(fmt.Errorf("tgwp-agent %s did not come back within %s", c.Wanted, u.o.HealthWait))
	}
	_ = os.Remove(prev)
	u.ui.ok("tgwp-agent is now %s", c.Wanted)
	return nil
}

// agentHealthy is the agent's equivalent of telemt's readiness probe: the unit is active and
// the new process has logged that it reached the panel. Where journalctl is unavailable, a
// unit that is still active a few seconds after the restart is the best available answer (a
// binary that cannot start would be cycling under Restart=always).
func (u *upgrader) agentHealthy(ctx context.Context, since time.Time) bool {
	if out, err := u.o.Exec.Run(ctx, "systemctl", "is-active", agentUnit); err != nil ||
		strings.TrimSpace(string(out)) != "active" {
		return false
	}
	out, err := u.o.Exec.Run(ctx, "journalctl", "-u", agentUnit, "--since", since.Format("2006-01-02 15:04:05"), "--no-pager", "-n", "200")
	if err != nil {
		return time.Since(since) >= 5*time.Second
	}
	return strings.Contains(string(out), "connected to panel")
}

// insideUnit reports whether this process belongs to the given systemd service, which is how
// `tgwp-agent upgrade` recognises that it is the very unit it is about to restart.
func insideUnit(unit string) bool {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "/"+unit+".service")
}

// download fetches the artifact into dir and verifies the panel's sha256 before returning the
// file. A mismatch, or a missing checksum, is an error and the file is deleted: this is the
// one gate between a compromised release host and root on the node.
func (u *upgrader) download(ctx context.Context, a UpgradeArtifact, dir, pattern string) (string, error) {
	if a.SHA256 == "" {
		return "", fmt.Errorf("the panel published no sha256 for %s; refusing to install an unverified download", a.URL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := u.o.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", a.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", a.URL, resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxUpgradeDownloadBytes))
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("download %s: %w", a.URL, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
		_ = os.Remove(name)
		return "", fmt.Errorf("checksum mismatch for %s: the download is %s, the panel pinned %s. Nothing was replaced", a.URL, got, a.SHA256)
	}
	if err := os.Chmod(name, 0o755); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// extractTelemtBinary pulls the `telemt` executable out of the release tarball into dst
// (0755). The release lays the binary out under a directory that changes between versions, so
// it is found by name.
func extractTelemtBinary(tgz, dst string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", tgz, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", tgz, err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "telemt" {
			continue
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.LimitReader(tr, maxUpgradeDownloadBytes))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		return os.Chmod(dst, 0o755)
	}
	return fmt.Errorf("no telemt binary inside %s", tgz)
}

// backupBinary copies path to path+".prev" and returns it. It is a copy, not a rename, so the
// target keeps its inode until the new binary is renamed over it and there is never a moment
// where neither exists. A target that does not exist yet (a component being installed rather
// than replaced) yields an empty path: there is nothing to roll back to, and restoreBinary
// takes that to mean "remove what was installed".
func backupBinary(path string) (string, error) {
	prev := path + ".prev"
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(prev, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(prev)
		return "", fmt.Errorf("keep a copy of %s: %w", path, err)
	}
	return prev, nil
}

// rolledBack describes what the rollback did, which differs when there was nothing to keep.
func rolledBack(prev, what string) string {
	if prev == "" {
		return "removed the " + what + " binary that was just installed; there was no previous one"
	}
	return "rolled back to the previous " + what + " binary"
}

// keptAs names the copy the rollback would use, or says there was nothing to keep.
func keptAs(prev string) string {
	if prev == "" {
		return " (there was no previous binary to keep)"
	}
	return " (previous kept as " + filepath.Base(prev) + ")"
}

// restoreBinary puts the kept copy back. It renames rather than downloading anything, so a
// rollback works with no network and no release host. With no kept copy (nothing was there
// before) it removes what was just installed instead.
func restoreBinary(prev, path string) error {
	if prev == "" {
		return os.Remove(path)
	}
	if _, err := os.Stat(prev); err != nil {
		return err
	}
	if err := os.Rename(prev, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}
