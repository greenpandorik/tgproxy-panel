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
	"syscall"
	"time"

	"tgwebproxy/internal/telemt"
)

// UpgradeOptions configures RunUpgrade.
type UpgradeOptions struct {
	EnvPath string
	Scope   UpgradeScope
	// Check reports and changes nothing.
	Check bool
	// Yes skips the confirmation prompt. Without a terminal it is required.
	Yes bool

	Out, Err io.Writer
	In       *os.File
	TTY      *bool

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

func stdoutOf(w io.Writer) *os.File {
	f, _ := w.(*os.File)
	return f
}

type upgrader struct {
	o           UpgradeOptions
	ui          *ui
	env         map[string]string
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

// installed reports what is on the node now.
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

// confirm prints the plan's consequence and asks, unless --yes.
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
func (u *upgrader) upgradeAgent(ctx context.Context, c ComponentPlan) error {
	u.ui.step("tgwp-agent %s", c.Wanted)
	if insideUnit(agentUnit) {
		err := fmt.Errorf("this command is running inside %s.service; restarting the unit would kill it mid-upgrade. Run it from a login shell: %s upgrade --agent", agentUnit, u.o.AgentBin)
		u.ui.fail("%v", err)
		return err
	}
	dir := filepath.Dir(u.o.AgentBin)
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

func insideUnit(unit string) bool {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "/"+unit+".service")
}

// download fetches the artifact into dir and verifies the panel's sha256 before returning the file.
func (u *upgrader) download(ctx context.Context, a UpgradeArtifact, dir, pattern string) (string, error) {
	return downloadArtifact(ctx, u.o.HTTP, a, dir, pattern)
}

// downloadArtifact fetches the artifact into dir and verifies the panel's sha256 before
// returning the file; a download that does not match is deleted and nothing is installed.
func downloadArtifact(ctx context.Context, httpc *http.Client, a UpgradeArtifact, dir, pattern string) (string, error) {
	if a.SHA256 == "" {
		return "", fmt.Errorf("the panel published no sha256 for %s; refusing to install an unverified download", a.URL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpc.Do(req)
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

// extractTelemtBinary pulls the `telemt` executable out of the release tarball into dst (0755).
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

// backupBinary copies path to path+".prev" and returns it.
func backupBinary(path string) (string, error) { return backupFile(path, 0o755) }

// backupFile copies path to path+".prev" with mode and returns it; a file that does not
// exist yields an empty name, which restoreFile reads as "there was nothing to put back".
func backupFile(path string, mode os.FileMode) (string, error) {
	prev := path + ".prev"
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = src.Close() }()
	info, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	dst, err := os.OpenFile(prev, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
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
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(prev, int(stat.Uid), int(stat.Gid)); err != nil {
			_ = os.Remove(prev)
			return "", fmt.Errorf("preserve owner of %s: %w", path, err)
		}
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

// restoreBinary puts the kept copy back.
func restoreBinary(prev, path string) error { return restoreFile(prev, path, 0o755) }

// restoreFile puts the kept copy back; with nothing kept it removes what was installed.
func restoreFile(prev, path string, mode os.FileMode) error {
	if prev == "" {
		return os.Remove(path)
	}
	if _, err := os.Stat(prev); err != nil {
		return err
	}
	if err := os.Rename(prev, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
