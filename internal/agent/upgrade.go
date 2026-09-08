package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// Node self-upgrade: the data half.
//
// A node must be able to move to a newer pinned engine on its own, by running one command on
// the host. It asks the panel what it should be running (GET /api/v1/node/upgrade, authorised
// by the node token it already holds), compares that with what is installed, and upgrades
// only what differs. Nothing here needs a panel session, an install token or a re-install.

const (
	// DefaultAgentEnvPath is the file the installer writes and the systemd unit reads. It
	// holds TGWP_PANEL_URL and TGWP_TOKEN, so it is 0600 and its contents are never printed.
	DefaultAgentEnvPath = "/etc/tgwp-agent/agent.env"
	// DefaultAgentBin is where the install script puts the agent binary.
	DefaultAgentBin = "/usr/local/bin/tgwp-agent"

	// UpgradePath is the panel route that answers the manifest.
	UpgradePath = "/api/v1/node/upgrade"

	agentUnit  = "tgwp-agent"
	telemtUnit = "telemt"

	// ComponentTelemt and ComponentAgent name the two upgradable components. tproxy-server is
	// built from source at a pinned commit and is not one of them.
	ComponentTelemt = "telemt"
	ComponentAgent  = "agent"
)

// maxUpgradeDownloadBytes caps a download. The telemt tarball is tens of megabytes; this is
// only here so a wrong URL cannot fill the node's disk.
const maxUpgradeDownloadBytes = 512 << 20

// UpgradeArtifact is one downloadable component as the panel pins it.
type UpgradeArtifact struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	URL     string `json:"url"`
}

// UpgradeTProxy is the tproxy engine's pin: a git commit, not a release asset.
type UpgradeTProxy struct {
	Commit string `json:"commit"`
	Repo   string `json:"repo"`
}

// UpgradeManifest is the panel's answer to "what should this node be running?".
type UpgradeManifest struct {
	Engine string           `json:"engine"`
	Telemt *UpgradeArtifact `json:"telemt,omitempty"`
	TProxy *UpgradeTProxy   `json:"tproxy,omitempty"`
	Agent  UpgradeArtifact  `json:"agent"`
}

// UpgradeScope limits which components are considered (--telemt / --agent). The zero value
// means "neither flag was given", which is the same as both.
type UpgradeScope struct{ Telemt, Agent bool }

func (s UpgradeScope) includes(component string) bool {
	if !s.Telemt && !s.Agent {
		return true
	}
	switch component {
	case ComponentTelemt:
		return s.Telemt
	case ComponentAgent:
		return s.Agent
	}
	return false
}

// ComponentPlan is the verdict for one component.
type ComponentPlan struct {
	Name string
	// Installed is what the node is running now; empty when it could not be determined, which
	// counts as out of date (better a needless reinstall than a silent skip).
	Installed string
	Wanted    string
	Artifact  UpgradeArtifact
	// Change is true when this component would be replaced.
	Change bool
	// Reason is the one-line explanation printed for this component.
	Reason string
}

// UpgradePlan is what the command would do, in the order it would do it.
type UpgradePlan struct{ Components []ComponentPlan }

// Changes returns only the components that would be replaced.
func (p UpgradePlan) Changes() []ComponentPlan {
	out := make([]ComponentPlan, 0, len(p.Components))
	for _, c := range p.Components {
		if c.Change {
			out = append(out, c)
		}
	}
	return out
}

// Installed is what the node currently has, keyed by component name. An empty or missing
// value means "could not be determined".
type Installed map[string]string

var reVersion = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?`)

// normalizeVersion reduces a reported version to something comparable: "telemt 3.5.6",
// "v3.5.6" and "3.5.6\n" are all 3.5.6.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "telemt ")
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

// sameVersion is deliberately equality, not "newer than": the panel's pin is the truth, so a
// node running something newer than the pin is out of date too and gets moved back to it.
// That is what makes a downgrade (the documented way to back out of a bad release) work with
// the same command.
func sameVersion(installed, wanted string) bool {
	a, b := normalizeVersion(installed), normalizeVersion(wanted)
	return a != "" && strings.EqualFold(a, b)
}

// ParseVersionOutput pulls a version out of whatever `telemt --version` prints - the exact
// wording varies between releases, so the first thing shaped like a version wins.
func ParseVersionOutput(out string) string {
	return reVersion.FindString(out)
}

// BuildUpgradePlan compares the panel's manifest with what is installed. It never decides
// anything from the node's own opinion of what it should run: the panel's pin is the target.
func BuildUpgradePlan(m UpgradeManifest, installed Installed, scope UpgradeScope) UpgradePlan {
	var plan UpgradePlan
	// telemt first, the agent last: replacing the agent restarts the unit this command's own
	// node depends on, so anything else must already be done by then.
	if m.Telemt != nil && scope.includes(ComponentTelemt) {
		plan.Components = append(plan.Components, componentPlan(ComponentTelemt, installed[ComponentTelemt], *m.Telemt))
	} else if m.Telemt != nil {
		plan.Components = append(plan.Components, ComponentPlan{
			Name: ComponentTelemt, Installed: installed[ComponentTelemt], Wanted: m.Telemt.Version,
			Artifact: *m.Telemt, Reason: "skipped (not in --telemt/--agent scope)",
		})
	}
	if m.TProxy != nil && scope.includes(ComponentTelemt) {
		// tproxy-server is compiled from source on the node; the agent cannot swap it in
		// place, so this is reported and never acted on.
		plan.Components = append(plan.Components, ComponentPlan{
			Name: "tproxy-server", Installed: installed["tproxy-server"], Wanted: m.TProxy.Commit,
			Reason: "pinned at " + shortCommit(m.TProxy.Commit) + "; a tproxy node is upgraded by re-running its install command",
		})
	}
	if scope.includes(ComponentAgent) {
		plan.Components = append(plan.Components, componentPlan(ComponentAgent, installed[ComponentAgent], m.Agent))
	} else {
		plan.Components = append(plan.Components, ComponentPlan{
			Name: ComponentAgent, Installed: installed[ComponentAgent], Wanted: m.Agent.Version,
			Artifact: m.Agent, Reason: "skipped (not in --telemt/--agent scope)",
		})
	}
	return plan
}

func componentPlan(name, installed string, a UpgradeArtifact) ComponentPlan {
	c := ComponentPlan{Name: name, Installed: installed, Wanted: a.Version, Artifact: a}
	switch {
	case sameVersion(installed, a.Version):
		c.Reason = "up to date (" + normalizeVersion(a.Version) + ")"
	case installed == "":
		c.Change = true
		c.Reason = "installed version unknown → install " + a.Version
	default:
		c.Change = true
		c.Reason = normalizeVersion(installed) + " → " + normalizeVersion(a.Version)
	}
	// A component the panel cannot vouch for is never installed: an unverified download runs
	// as root on this host.
	if c.Change && a.SHA256 == "" {
		c.Change = false
		c.Reason = "the panel published no sha256 for " + a.Version + "; refusing to install an unverified download"
	}
	return c
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// ReadEnvFile parses the KEY=VALUE lines of an agent env file. Values are taken literally
// (the installer writes no quoting) and the map is never logged: it holds the node token.
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FetchUpgradeManifest asks the panel what this node should be running. The node token goes
// in the Authorization header and is never part of a URL, a log line or an error.
func FetchUpgradeManifest(ctx context.Context, httpc *http.Client, panelURL, token string) (UpgradeManifest, error) {
	var m UpgradeManifest
	if httpc == nil {
		httpc = http.DefaultClient
	}
	url := strings.TrimRight(panelURL, "/") + UpgradePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return m, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpc.Do(req)
	if err != nil {
		return m, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return m, fmt.Errorf("GET %s: %w", url, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return m, errors.New("the panel rejected this node's token (401); the node may have been re-installed or removed - check TGWP_TOKEN in the agent env file")
	case resp.StatusCode != http.StatusOK:
		return m, fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, firstLine(string(body)))
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("GET %s: invalid response body", url)
	}
	if m.Engine == "" {
		return m, fmt.Errorf("GET %s: response carries no engine", url)
	}
	return m, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
