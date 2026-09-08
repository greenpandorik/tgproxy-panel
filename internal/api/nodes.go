package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

const installTokenTTL = 24 * time.Hour

type nodeJSON struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	Hostname      string          `json:"hostname"`
	PublicIP      string          `json:"public_ip"`
	ACMEEmail     string          `json:"acme_email"`
	Status        string          `json:"status"`
	Online        bool            `json:"online"`
	Engine        string          `json:"engine"`
	TLSDomain     string          `json:"tls_domain"`
	ClassicPort   int             `json:"classic_port"`
	TelemtVersion string          `json:"telemt_version"`
	TProxyVersion string          `json:"tproxy_version"`
	AgentVersion  string          `json:"agent_version"`
	MaxProfiles   int             `json:"max_profiles"`
	ProfileCount  int             `json:"profile_count"`
	Dirty         bool            `json:"dirty"`
	LastSeenAt    *time.Time      `json:"last_seen_at"`
	LastApplyAt   *time.Time      `json:"last_apply_at"`
	CreatedAt     time.Time       `json:"created_at"`
	Health        map[string]any  `json:"health,omitempty"`
	LastCheck     json.RawMessage `json:"last_check"`
}

func (s *Server) nodeJSON(r *http.Request, n db.Node) nodeJSON {
	count, _ := s.store.Q.CountNodeProfiles(r.Context(), n.ID)
	return s.nodeJSONWithCount(r, n, count)
}

// nodeJSONWithCount is the single-node projection with the profile count already
// known. The list endpoint gets every count in one query (ListNodesWithCounts)
// instead of a CountNodeProfiles round trip per node.
func (s *Server) nodeJSONWithCount(r *http.Request, n db.Node, count int64) nodeJSON {
	out := nodeJSON{
		ID: n.ID, Name: n.Name, Hostname: n.Hostname, PublicIP: n.PublicIp, ACMEEmail: n.AcmeEmail, Status: string(n.Status),
		Online: s.driver != nil && s.driver.Online(n.ID), Engine: string(n.Engine), TLSDomain: n.TlsDomain,
		ClassicPort: int(n.ClassicPort), TelemtVersion: n.TelemtVersion,
		TProxyVersion: n.TproxyVersion, AgentVersion: n.AgentVersion,
		MaxProfiles: int(n.MaxProfiles), ProfileCount: int(count), Dirty: n.Dirty, LastSeenAt: n.LastSeenAt, LastApplyAt: n.LastApplyAt,
		CreatedAt: n.CreatedAt,
	}
	// last_health is the HealthReport as the heartbeat marshalled it (Go field names). The
	// SPA's NodeHealth type is the snake_case shape of the /health endpoint, so the row is
	// re-emitted through the same projection; a row the report cannot be read from is
	// omitted rather than passed through in a shape the client does not know.
	if len(n.LastHealth) > 0 {
		var h nodedriver.HealthReport
		if err := json.Unmarshal(n.LastHealth, &h); err == nil {
			out.Health = healthJSON(h)
		}
	}
	if len(n.LastCheck) > 0 {
		out.LastCheck = redactLastCheck(n.LastCheck, isWriter(r))
	}
	return out
}

// redactLastCheck strips each probe's Detail string for non-writer roles, keeping
// name/ok/ran_at/all_ok visible to every role. Detail can echo internal network
// facts (resolved IPs, TLS errors, private-range hints) that only writers should
// see.
//
// A report this function cannot process is dropped rather than passed through: a
// redaction path has to fail closed, or the one case it does not understand is
// exactly the case that leaks. Returning nil gives the viewer the same shape a
// node with no check yet has, which the SPA already renders.
func redactLastCheck(raw json.RawMessage, writer bool) json.RawMessage {
	if writer {
		return raw
	}
	var report nodecheck.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil
	}
	for i := range report.Results {
		report.Results[i].Detail = ""
	}
	redacted, err := json.Marshal(report)
	if err != nil {
		return nil
	}
	return redacted
}

func (s *Server) loadNode(w http.ResponseWriter, r *http.Request) (db.Node, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.Node{}, false
	}
	n, err := s.store.Q.GetNode(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.Node{}, false
	}
	return n, true
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListNodesWithCounts(r.Context())
	if err != nil {
		internal(w)
		return
	}
	items := make([]nodeJSON, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.nodeJSONWithCount(r, row.Node, row.ProfileCount))
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

type createNodeReq struct {
	Name        string `json:"name"`
	Hostname    string `json:"hostname"`
	ACMEEmail   string `json:"acme_email"`
	PublicIP    string `json:"public_ip"`
	Engine      string `json:"engine"`
	TLSDomain   string `json:"tls_domain"`
	ClassicPort int    `json:"classic_port"`
}

// defaultClassicPort is the Fake-TLS listener port telemt nodes get unless the
// operator picks another one.
const defaultClassicPort = 8443

// validateClassicPort keeps the Fake-TLS listener out of the privileged range
// (Caddy owns 80/443 on a telemt node, and telemt itself runs unprivileged), and
// off the reserved ports the panel already uses on the node.
func validateClassicPort(port int) string {
	if port == 80 || port == 443 {
		return "80 and 443 are taken by the node's web server"
	}
	if port < 1024 || port > 65535 {
		return "1024..65535"
	}
	return ""
}

// validatePublicIP accepts an empty value (the install script fills it in) or one IPv4
// address. It is what telemt's WEB vhost names in public_addr and what the readiness check
// expects the hostname to resolve to, so anything else would fail the node later and less
// clearly.
func validatePublicIP(ip string) string {
	if ip == "" {
		return ""
	}
	if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
		return "IPv4 address"
	}
	return ""
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req createNodeReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	req.Hostname = strings.ToLower(strings.TrimSpace(req.Hostname))
	req.TLSDomain = strings.ToLower(strings.TrimSpace(req.TLSDomain))
	req.PublicIP = strings.TrimSpace(req.PublicIP)
	if req.Engine == "" {
		req.Engine = string(domain.EngineTelemt)
	}
	// The Fake-TLS listener masks behind the node's own site by default, so an
	// unspecified tls_domain is the node's hostname.
	if req.TLSDomain == "" {
		req.TLSDomain = req.Hostname
	}
	if req.ClassicPort == 0 {
		req.ClassicPort = defaultClassicPort
	}
	fields := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		fields["name"] = "required"
	}
	if err := domain.ValidateHostname(req.Hostname); err != nil {
		fields["hostname"] = err.Error()
	}
	if _, err := mail.ParseAddress(req.ACMEEmail); err != nil {
		fields["acme_email"] = "valid email required for Let's Encrypt"
	}
	if !domain.Engine(req.Engine).Valid() {
		fields["engine"] = "tproxy or telemt"
	}
	if err := domain.ValidateHostname(req.TLSDomain); err != nil {
		fields["tls_domain"] = err.Error()
	}
	if msg := validateClassicPort(req.ClassicPort); msg != "" {
		fields["classic_port"] = msg
	}
	if msg := validatePublicIP(req.PublicIP); msg != "" {
		fields["public_ip"] = msg
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	if _, err := s.store.Q.GetNodeByHostname(r.Context(), req.Hostname); err == nil {
		conflict(w, "hostname already exists")
		return
	}
	token, err := crypto.NewToken(24)
	if err != nil {
		internal(w)
		return
	}
	hash := crypto.HashToken(token)
	exp := time.Now().Add(installTokenTTL)
	var node db.Node
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		var err error
		node, err = q.CreateNode(r.Context(), db.CreateNodeParams{
			Name: req.Name, Hostname: req.Hostname, PublicIp: req.PublicIP, AcmeEmail: req.ACMEEmail,
			InstallTokenHash: &hash, InstallTokenExpires: &exp,
			Engine:      db.NullNodeEngine{NodeEngine: db.NodeEngine(req.Engine), Valid: true},
			TlsDomain:   &req.TLSDomain,
			ClassicPort: pgtype.Int4{Int32: int32(req.ClassicPort), Valid: true},
		})
		if err != nil {
			return err
		}
		secret, err := crypto.NewSecretHex()
		if err != nil {
			return err
		}
		enc, err := s.box.EncryptString(secret)
		if err != nil {
			return err
		}
		_, err = q.CreateProfile(r.Context(), db.CreateProfileParams{NodeID: node.ID, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")})
		return err
	})
	if err != nil {
		s.log.Error("create node", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.create", "node", node.ID.String(), map[string]any{"hostname": node.Hostname, "engine": string(node.Engine)})
	writeJSON(w, 201, map[string]any{"node": s.nodeJSON(r, node), "install_command": s.installCommand(token), "expires_at": exp})
}

func (s *Server) installCommand(token string) string {
	return "curl -fsSL " + s.cfg.PublicURL + "/api/v1/install/" + token + ".sh | sudo bash"
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, s.nodeJSON(r, n))
}

type patchNodeReq struct {
	Name        *string `json:"name"`
	PublicIP    *string `json:"public_ip"`
	MaxProfiles *int    `json:"max_profiles"`
	ACMEEmail   *string `json:"acme_email"`
	TLSDomain   *string `json:"tls_domain"`
	ClassicPort *int    `json:"classic_port"`
}

func (s *Server) handlePatchNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	var req patchNodeReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if req.Name != nil {
		n.Name = *req.Name
	}
	// public_ip is desired state too: telemt names it in the WEB vhost's public_addr and the
	// readiness check compares the A record against it, so a corrected address (a NAT host
	// whose installer detected the egress side) must reach the node like a listener change.
	dirty := false
	if req.PublicIP != nil {
		ip := strings.TrimSpace(*req.PublicIP)
		if msg := validatePublicIP(ip); msg != "" {
			validation(w, map[string]string{"public_ip": msg})
			return
		}
		dirty = dirty || ip != n.PublicIp
		n.PublicIp = ip
	}
	if req.ACMEEmail != nil {
		// acme_email is interpolated into the root-run installer script, so it gets the same
		// validation as on create — an unvalidated value here is an admin -> root-on-node path.
		if _, err := mail.ParseAddress(*req.ACMEEmail); err != nil {
			validation(w, map[string]string{"acme_email": "must be a valid email address"})
			return
		}
		n.AcmeEmail = *req.ACMEEmail
	}
	if req.MaxProfiles != nil {
		if *req.MaxProfiles < 1 || *req.MaxProfiles > 1024 {
			validation(w, map[string]string{"max_profiles": "1..1024"})
			return
		}
		n.MaxProfiles = int32(*req.MaxProfiles)
	}
	// tls_domain and classic_port describe the node's listeners: changing either
	// is a change to the desired state, so the node is marked dirty and the agent
	// (which owns the restart the new listener needs) picks it up on the next apply.
	listeners := db.UpdateNodeParams{ID: n.ID, Name: n.Name, PublicIp: n.PublicIp, MaxProfiles: n.MaxProfiles, AcmeEmail: n.AcmeEmail}
	if req.TLSDomain != nil {
		d := strings.ToLower(strings.TrimSpace(*req.TLSDomain))
		if err := domain.ValidateHostname(d); err != nil {
			validation(w, map[string]string{"tls_domain": err.Error()})
			return
		}
		listeners.TlsDomain = &d
		dirty = dirty || d != n.TlsDomain
	}
	if req.ClassicPort != nil {
		if msg := validateClassicPort(*req.ClassicPort); msg != "" {
			validation(w, map[string]string{"classic_port": msg})
			return
		}
		listeners.ClassicPort = pgtype.Int4{Int32: int32(*req.ClassicPort), Valid: true}
		dirty = dirty || int32(*req.ClassicPort) != n.ClassicPort
	}
	updated, err := s.store.Q.UpdateNode(r.Context(), listeners)
	if err != nil {
		internal(w)
		return
	}
	if dirty {
		if err := s.store.Q.SetNodeDirty(r.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: true}); err != nil {
			s.log.Error("mark node dirty", "err", err)
		}
		updated, err = s.store.Q.GetNode(r.Context(), n.ID)
		if err != nil {
			internal(w)
			return
		}
	}
	s.Audit(r.Context(), "node.update", "node", n.ID.String(), nil)
	writeJSON(w, 200, s.nodeJSON(r, updated))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if err := s.store.Q.DeleteNode(r.Context(), n.ID); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.delete", "node", n.ID.String(), map[string]any{"hostname": n.Hostname})
	w.WriteHeader(204)
}

func (s *Server) handleInstallCommand(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	token, err := crypto.NewToken(24)
	if err != nil {
		internal(w)
		return
	}
	hash := crypto.HashToken(token)
	exp := time.Now().Add(installTokenTTL)
	if err := s.store.Q.SetNodeInstallToken(r.Context(), db.SetNodeInstallTokenParams{ID: n.ID, InstallTokenHash: &hash, InstallTokenExpires: &exp}); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.install_token", "node", n.ID.String(), nil)
	writeJSON(w, 200, map[string]any{"command": s.installCommand(token), "expires_at": exp})
}

func (s *Server) driverErr(w http.ResponseWriter, err error) {
	if errors.Is(err, nodedriver.ErrOffline) {
		writeError(w, 503, "node_offline", "node is offline", nil)
		return
	}
	writeError(w, 502, "node_error", err.Error(), nil)
}

func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	h, err := s.driver.Health(r.Context(), n.ID)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	writeJSON(w, 200, healthJSON(h))
}

func healthJSON(h nodedriver.HealthReport) map[string]any {
	// dcs is always a list, never null: the SPA iterates it for every engine.
	dcs := make([]map[string]any, 0, len(h.DCs))
	for _, d := range h.DCs {
		dcs = append(dcs, map[string]any{"dc": d.DC, "latency_ms": d.LatencyMs, "known": d.Known, "ip_preference": d.IPPreference})
	}
	return map[string]any{
		"relay_active": h.RelayActive, "mtproxy_active": h.MTProxyActive, "caddy_active": h.CaddyActive,
		"healthz": h.Healthz, "readyz": h.Readyz, "tproxy_version": h.TProxyVersion, "agent_version": h.AgentVersion,
		"uptime_seconds": h.UptimeSeconds, "cpu_percent": h.CPUPercent, "mem_used_percent": h.MemUsedPercent,
		"disk_used_percent": h.DiskUsedPercent, "profile_count": h.ProfileCount,
		"dcs": dcs, "upstream_healthy": h.UpstreamHealthy, "upstream_fails": h.UpstreamFails,
		"effective_latency_ms": h.EffectiveLatencyMs, "connect_success_total": h.ConnectSuccessTotal,
		"connect_fail_total": h.ConnectFailTotal, "upstream_last_check_age_secs": h.UpstreamLastCheckAgeSecs,
		"dc_data_available": h.DcDataAvailable,
	}
}

func (s *Server) handleNodeProfiles(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("live") == "1" {
		ps, err := s.driver.GetProfiles(r.Context(), n.ID)
		if err != nil {
			s.driverErr(w, err)
			return
		}
		items := make([]map[string]any, 0, len(ps))
		for _, p := range ps {
			items = append(items, map[string]any{"name": p.Name, "backend": p.Backend, "carrier_mode": p.CarrierMode, "limits": p.Limits})
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
		return
	}
	// The key columns come along for the ride: a profile row on a telemt node is only
	// legible next to the key that produced it - whose label it carries, when it expires,
	// and which limits telemt is enforcing for it. The node's own default profile has no
	// key, so those fields are absent there rather than zero.
	rows, err := s.store.Q.ListNodeProfilesWithKey(r.Context(), n.ID)
	if err != nil {
		internal(w)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		item := map[string]any{
			"id": p.ID, "name": p.Name, "access_key_id": p.AccessKeyID, "backend": p.Backend,
			"carrier_mode": p.CarrierMode, "limits": json.RawMessage(p.Limits), "sync_state": p.SyncState, "created_at": p.CreatedAt,
			"key_label": "", "key_expires_at": p.KeyExpiresAt, "telemt_limits": telemtLimitsJSON(p.KeyTelemtLimits),
		}
		if p.KeyLabel != nil {
			item["key_label"] = *p.KeyLabel
		}
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleNodeStats(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	st, err := s.driver.Stats(r.Context(), n.ID)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) handleNodeMetrics(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	text, err := s.driver.Metrics(r.Context(), n.ID)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(text))
}

func (s *Server) handleNodeRestart(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if err := s.driver.RestartRelay(r.Context(), n.ID); err != nil {
		s.driverErr(w, err)
		return
	}
	s.Audit(r.Context(), "node.restart_relay", "node", n.ID.String(), nil)
	w.WriteHeader(204)
}

func (s *Server) handleNodeApply(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	_ = s.store.Q.SetNodeDirty(r.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: true})
	if s.applyNow != nil {
		s.applyNow(context.WithoutCancel(r.Context()), n.ID)
	}
	s.Audit(r.Context(), "node.apply", "node", n.ID.String(), nil)
	writeJSON(w, 202, map[string]any{"queued": true})
}

func (s *Server) handleNodeJobs(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.store.Q.ListNodeApplyJobs(r.Context(), db.ListNodeApplyJobsParams{NodeID: n.ID, Limit: int32(limit)})
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (s *Server) handleNodeLogs(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	services := strings.Split(r.URL.Query().Get("services"), ",")
	if services[0] == "" {
		services = []string{"tproxy-server"}
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	follow := r.URL.Query().Get("follow") == "1"
	ch, err := s.driver.TailLogs(r.Context(), n.ID, services, lines, follow)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	sse := newSSE(w)
	for line := range ch {
		if err := sse.send("log", map[string]any{"service": line.Service, "line": line.Line, "time": line.Time}); err != nil {
			return
		}
	}
	_ = sse.send("end", map[string]any{})
}
