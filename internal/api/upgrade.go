package api

import (
	"net/http"
	"strings"

	"tgwebproxy/internal/agent"
	"tgwebproxy/internal/nodeinstall"
	"tgwebproxy/internal/store/db"
)

// The node upgrade manifest: what the panel says a node should be running.
type upgradeArtifact struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	URL     string `json:"url"`
}

// upgradeTProxy is the tproxy engine's pin.
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

// handleNodeUpgrade answers GET /api/v1/node/upgrade for a node presenting its own agent token.
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
