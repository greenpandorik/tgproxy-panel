package api

import (
	"net/http"
	"strings"

	"tgwebproxy/internal/agent"
	"tgwebproxy/internal/nodeinstall"
	"tgwebproxy/internal/store/db"
)

// The node upgrade manifest: what the panel says a node should be running.
//
// It is the answer to "am I current?", asked by `tgwp-agent upgrade` on the node itself. The
// node authenticates with the token it already holds (/etc/tgwp-agent/agent.env), so an
// upgrade needs neither a panel session nor a fresh install token, and nothing about the node
// is re-installed - which is the whole point: moving a fleet to a new pinned engine used to
// mean regenerating an install command per node and re-running the entire installer.
type upgradeArtifact struct {
	Version string `json:"version"`
	// SHA256 of the file at URL. Empty means the panel has no checksum for it, and the node
	// must refuse to install it: an unverified download becomes root on the node.
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

// upgradeTProxy is the tproxy engine's pin. tproxy-server is built from source at a commit
// rather than downloaded as a release asset, so it has no version/sha256/url shape and the
// agent cannot upgrade it in place; the node reports the commit it should be at and the
// operator re-runs the install script for that engine.
type upgradeTProxy struct {
	Commit string `json:"commit"`
	Repo   string `json:"repo"`
}

type upgradeManifest struct {
	Engine string           `json:"engine"`
	Telemt *upgradeArtifact `json:"telemt,omitempty"`
	TProxy *upgradeTProxy   `json:"tproxy,omitempty"`
	Agent  upgradeArtifact  `json:"agent"`
}

const tproxyRepoURL = "https://github.com/telegramdesktop/tproxy-server.git"

// agentDownloadURL is the same route the install script curls the agent from
// (GET /api/v1/install/agent/{platform}); agentSHA256 reads the same checksum file the
// script verifies against. There is deliberately no second way to ship the agent binary.
//
// The reported agent version is this panel binary's own agent.Version: the release image
// builds both from one source tree and stages the binary on every start, so the two agree.
// The checksum, on the other hand, is always read from the file that is actually served, so
// even a hand-assembled DATA_DIR cannot make a node install something unverified.
func (s *Server) agentDownloadURL() string {
	return s.cfg.PublicURL + "/api/v1/install/agent/linux-amd64"
}

// bearerToken returns the Bearer credential of an Authorization header, or "".
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	v := r.Header.Get("Authorization")
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(v[len(prefix):])
}

// handleNodeUpgrade answers GET /api/v1/node/upgrade for a node presenting its own agent
// token - the same credential the gRPC gateway takes, validated the same way (sha256 lookup
// through nodesvc.Presence). A missing, malformed, unknown or superseded token is one
// indistinguishable 401: a caller probing tokens learns nothing from the difference.
func (s *Server) handleNodeUpgrade(w http.ResponseWriter, r *http.Request) {
	nodeID, err := s.presence.NodeByToken(r.Context(), bearerToken(r))
	if err != nil {
		unauthorized(w)
		return
	}
	n, err := s.store.Q.GetNode(r.Context(), nodeID)
	if err != nil {
		internal(w)
		return
	}
	out := upgradeManifest{
		Engine: string(n.Engine),
		Agent: upgradeArtifact{
			Version: agent.Version,
			SHA256:  s.agentSHA256(),
			URL:     s.agentDownloadURL(),
		},
	}
	if n.Engine == db.NodeEngineTelemt {
		if s.cfg.TelemtSHA256 == "" {
			// Same rule as the install script: the panel never tells a node to download a
			// binary it cannot verify. The operator's only clue is this log line.
			s.log.Error("node upgrade manifest: telemt build is not pinned", "node", n.ID, "version", s.cfg.TelemtVersion)
			writeError(w, 500, "unpinned_telemt", "TELEMT_SHA256_X86_64 is not set on the panel", nil)
			return
		}
		out.Telemt = &upgradeArtifact{
			Version: s.cfg.TelemtVersion,
			SHA256:  s.cfg.TelemtSHA256,
			URL:     nodeinstall.TelemtReleaseURL(s.cfg.TelemtVersion),
		}
	} else {
		out.TProxy = &upgradeTProxy{Commit: s.cfg.TProxyCommit, Repo: tproxyRepoURL}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}
