package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodeinstall"
	"tgwebproxy/internal/store/db"
)

func (s *Server) agentSHA256() string {
	b, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "agent", "tgwp-agent-linux-amd64.sha256"))
	if err != nil {
		return ""
	}
	// An empty or whitespace-only checksum file must not panic an HTTP handler.
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	n, err := s.store.Q.GetNodeByInstallToken(r.Context(), ptr(crypto.HashToken(token)))
	if err != nil {
		notFound(w)
		return
	}
	profiles, err := s.store.Q.ListNodeProfiles(r.Context(), n.ID)
	if err != nil || len(profiles) == 0 {
		s.log.Error("install script profiles", "err", err, "node", n.ID, "profiles", len(profiles))
		internal(w)
		return
	}
	secret, err := s.box.DecryptString(profiles[0].SecretEnc)
	if err != nil {
		s.log.Error("install script secret", "err", err, "node", n.ID)
		internal(w)
		return
	}
	site := nodeinstall.FallbackSite()
	if s.siteProvider != nil {
		if custom, err := s.siteProvider(r.Context(), n.ID); err == nil && custom != nil {
			site = custom
		}
	}
	script, err := nodeinstall.Render(nodeinstall.Params{
		PanelURL: s.cfg.PublicURL, InstallToken: token, Hostname: n.Hostname, ACMEEmail: n.AcmeEmail, Secret: secret,
		TProxyCommit: s.cfg.TProxyCommit, AgentSHA256: s.agentSHA256(), Site: site,
		// The engine is fixed when the node is created and picks the script branch.
		Engine: domain.Engine(n.Engine), WebUser: profiles[0].Name,
		TLSDomain: n.TlsDomain, ClassicPort: int(n.ClassicPort), PublicIP: n.PublicIp,
		TelemtVersion: s.cfg.TelemtVersion, TelemtSHA256: s.cfg.TelemtSHA256,
	})
	if err != nil {
		s.log.Error("install script render", "err", err, "node", n.ID)
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}

type registerReq struct {
	Hostname      string `json:"hostname"`
	TProxyVersion string `json:"tproxy_version"`
	AgentVersion  string `json:"agent_version"`
	// PublicIP is the address the install script detected on the node.
	PublicIP string `json:"public_ip"`
}

func (s *Server) handleInstallRegister(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	n, err := s.store.Q.GetNodeByInstallToken(r.Context(), ptr(crypto.HashToken(token)))
	if err != nil {
		notFound(w)
		return
	}
	var req registerReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	publicIP := strings.TrimSpace(req.PublicIP)
	storeIP := publicIP != "" && n.PublicIp == ""
	if n.Engine == db.NodeEngineTelemt && n.PublicIp == "" && publicIP == "" {
		// Refused before the install token is consumed, so the script can retry.
		validation(w, map[string]string{"public_ip": "required for telemt nodes"})
		return
	}
	nodeToken, err := crypto.NewToken(32)
	if err != nil {
		internal(w)
		return
	}
	if storeIP {
		if err := s.store.Q.SetNodePublicIP(r.Context(), db.SetNodePublicIPParams{ID: n.ID, PublicIp: publicIP}); err != nil {
			internal(w)
			return
		}
	}
	if err := s.store.Q.RegisterNode(r.Context(), db.RegisterNodeParams{ID: n.ID, AgentTokenHash: ptr(crypto.HashToken(nodeToken)), TproxyVersion: req.TProxyVersion, AgentVersion: req.AgentVersion}); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.register", "node", n.ID.String(), map[string]any{"hostname": n.Hostname})
	writeJSON(w, 200, map[string]any{"node_id": n.ID, "token": nodeToken, "panel_url": s.cfg.PublicURL})
}

func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	if platform != "linux-amd64" {
		notFound(w)
		return
	}
	path := filepath.Join(s.cfg.DataDir, "agent", "tgwp-agent-"+platform)
	if _, err := os.Stat(path); err != nil {
		writeError(w, 404, "agent_missing", "agent binary not built; run make agent-linux", nil)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

func ptr(s string) *string { return &s }
