// Package api serves the panel HTTP API and the embedded SPA.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodecheck"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/notify"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/updates"
	"tgwebproxy/web"
)

type Deps struct {
	Store    *store.Store
	Box      *crypto.Box
	Signer   crypto.Signer
	Log      *slog.Logger
	Cfg      config.Config
	Driver   nodedriver.Driver
	Presence *nodesvc.Presence
	Keys     *keys.Service
	// ApplyNow is nil until Part C; handlers treat nil as "mark dirty only".
	ApplyNow func(ctx context.Context, nodeID uuid.UUID)
	// SiteProvider is nil-safe; a nil value falls back to the embedded fallback site.
	SiteProvider func(ctx context.Context, nodeID uuid.UUID) (map[string][]byte, error)
	// Notifier sends the "send test message" Telegram probe from
	// POST /settings/telegram/test; nil (the default) builds a real
	// *notify.Telegram. Tests inject a fake to avoid a real HTTP call.
	Notifier telegramSender
	// Backups runs pg_dump/pg_restore for the /backups routes; nil (the
	// default) builds a real one over cfg.DatabaseURL and DATA_DIR/backups.
	// Tests inject a runner with a fake Exec so no postgres client tools are
	// needed.
	Backups *backup.Runner
	// NodeChecker runs POST /nodes/{id}/check's DNS/TCP/TLS/HTTP probes;
	// nil (the default) builds a real *nodecheck.Checker (real resolver,
	// real dialer). Tests inject one with a fake resolver/dial so the
	// check never touches the network.
	NodeChecker *nodecheck.Checker
	// Updates answers GET /status/update from GitHub release metadata. It is
	// only used when Cfg.UpdateCheck is true - with UPDATE_CHECK=false the
	// handler reports enabled=false and no outbound request is ever made,
	// whatever was wired in here. Tests point one at an httptest stub.
	Updates *updates.Checker
}

type Server struct {
	store          *store.Store
	box            *crypto.Box
	signer         crypto.Signer
	log            *slog.Logger
	cfg            config.Config
	secureCookies  bool
	loginLimiter   *ipLimiter
	subLimiter     *ipLimiter
	driver         nodedriver.Driver
	presence       *nodesvc.Presence
	keys           *keys.Service
	applyNow       func(context.Context, uuid.UUID)
	siteProvider   func(context.Context, uuid.UUID) (map[string][]byte, error)
	tg             telegramSender
	nodeChecker    *nodecheck.Checker
	metricsHandler http.Handler
	backups        *backup.Runner
	// backupSlot has room for one dump: pg_dump on a real database runs for
	// minutes, and two of them writing the same directory at once is never what
	// the operator meant. A full slot is answered with 409 backup_running.
	backupSlot chan struct{}
	// publicStatus memoises GET /api/v1/status/public for publicStatusTTL, so
	// the one unauthenticated read cannot be turned into a node-count query
	// flood.
	publicStatus publicStatusCache
	// updates is nil when the update check is disabled.
	updates *updates.Checker
}

func New(d Deps) *Server {
	s := &Server{
		store: d.Store, box: d.Box, signer: d.Signer, log: d.Log, cfg: d.Cfg,
		secureCookies: strings.HasPrefix(d.Cfg.PublicURL, "https://"),
		loginLimiter:  newIPLimiter(10, 10*time.Minute, 15*time.Minute),
		// New instance dedicated to the public subscription page: 60 requests per
		// IP per minute, matching the per-node QR/link scrape a normal client does
		// once and a monitoring probe might do periodically, while still shutting
		// down a scraper working through many tokens from one address.
		subLimiter: newIPLimiter(60, time.Minute, time.Minute),
		driver:     d.Driver, presence: d.Presence, keys: d.Keys, applyNow: d.ApplyNow, siteProvider: d.SiteProvider,
		tg: d.Notifier, nodeChecker: d.NodeChecker, backups: d.Backups,
		backupSlot: make(chan struct{}, 1),
	}
	if s.backups == nil {
		s.backups = &backup.Runner{DatabaseURL: d.Cfg.DatabaseURL, Dir: backup.DirFor(d.Cfg.DataDir)}
	}
	if d.Cfg.UpdateCheck {
		s.updates = d.Updates
	}
	if s.siteProvider == nil {
		s.siteProvider = s.NodeSiteFiles
	}
	if s.tg == nil {
		s.tg = notify.NewTelegram(http.DefaultClient, "")
	}
	s.metricsHandler = newMetricsHandler(d.Store)
	return s
}

// Handler builds the panel router. It returns chi.Router (still an
// http.Handler) so tests can walk the route table.
func (s *Server) Handler() chi.Router {
	r := chi.NewRouter()
	// No middleware.RealIP here, deliberately: it rewrites r.RemoteAddr from the
	// *first* X-Forwarded-For entry, which is the one a client can write for
	// itself. clientIP (session.go) reads the last entry instead, and RemoteAddr
	// has to stay the real peer address for it to have anything to fall back to.
	r.Use(middleware.RequestID, middleware.Recoverer, denyFraming, withIP, s.loadSession)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	r.Get("/metrics", s.handleMetrics)
	// The public subscription page: it lives at the panel's own root (not under
	// /api/v1), because the URL a key holder is handed and pastes into a browser
	// is PublicURL + "/s/" + token - a short link, not an API path. It is rate
	// limited per IP (s.subLimiter) inside the handlers themselves, since a
	// middleware here would also have to cover the .json twin below.
	r.Get("/s/{token}", s.handleSubscriptionPage)
	r.Get("/s/{token}.json", s.handleSubscriptionJSON)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		// Public by design: the caller has no session yet. The signed, five-minute
		// challenge minted by /auth/login is what authenticates the request.
		r.Post("/auth/totp/verify", s.handleTOTPVerify)
		r.Get("/install/{token}.sh", s.handleInstallScript)
		r.Post("/install/{token}/register", s.handleInstallRegister)
		r.Get("/install/agent/{platform}", s.handleAgentDownload)
		r.Get("/branding", s.handleGetActiveBranding)
		r.Get("/branding/assets/{id}/{file}", s.handleBrandingAsset)
		// Public by design: the login screen renders it before anyone has a
		// session. It carries only the version, two node counts and the relay
		// commit - no hostnames, names or IPs - and is memoised for 10s.
		r.Get("/status/public", s.handlePublicStatus)
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, csrfCheck)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)
			// Every role: the topbar shows the version chip to whoever is logged
			// in, and the answer is public repository metadata anyway.
			r.Get("/status/update", s.handleUpdateStatus)
			r.Post("/me/password", s.handleChangePassword)
			// Self-service for every role, viewers included: a second factor on your
			// own account is not a privileged operation, so no RequireRole here.
			r.Post("/auth/totp/setup", s.handleTOTPSetup)
			r.Post("/auth/totp/confirm", s.handleTOTPConfirm)
			r.Post("/auth/totp/disable", s.handleTOTPDisable)
			r.With(RequireRole(RoleOwner)).Get("/admins", s.handleListAdmins)
			r.With(RequireRole(RoleOwner)).Post("/admins", s.handleCreateAdmin)
			r.With(RequireRole(RoleOwner)).Delete("/admins/{id}", s.handleDeleteAdmin)
			s.mountProtected(r) // later parts add nodes, keys, etc.
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { notFound(w) })
		r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		})
	})

	r.Handle("/*", spaHandler())
	return r
}

// mountProtected is extended in later tasks (keys, sites, branding, dashboard).
func (s *Server) mountProtected(r chi.Router) {
	r.Get("/nodes", s.handleListNodes)
	r.With(RequireRole(writers...)).Post("/nodes", s.handleCreateNode)
	r.Route("/nodes/{id}", func(r chi.Router) {
		r.Get("/", s.handleGetNode)
		r.With(RequireRole(writers...)).Patch("/", s.handlePatchNode)
		r.With(RequireRole(writers...)).Delete("/", s.handleDeleteNode)
		r.With(RequireRole(writers...)).Get("/install-command", s.handleInstallCommand)
		r.Get("/health", s.handleNodeHealth)
		r.Get("/profiles", s.handleNodeProfiles)
		r.Get("/stats", s.handleNodeStats)
		r.Get("/metrics", s.handleNodeMetrics)
		r.Get("/logs", s.handleNodeLogs)
		r.Get("/jobs", s.handleNodeJobs)
		r.With(RequireRole(writers...)).Post("/restart", s.handleNodeRestart)
		r.With(RequireRole(writers...)).Post("/apply", s.handleNodeApply)
		r.With(RequireRole(writers...)).Post("/check", s.handleNodeCheck)
		r.With(RequireRole(writers...)).Post("/site", s.handleAssignSite)
		r.Get("/site", s.handleGetNodeSite)
		r.Get("/site/preview", s.handleSitePreview)
	})
	s.mountKeys(r)
	s.mountSites(r)
	s.mountBranding(r)
	s.mountDashboard(r)
	s.mountMonitoring(r)
	s.mountSettings(r)
	s.mountBackups(r)
	s.mountAudit(r)
}

// denyFraming refuses to be embedded anywhere, on every response the panel sends.
// The SPA is a session-authenticated admin UI with destructive buttons on it and
// the public subscription page is a page of connection details; neither has any
// reason to appear inside someone else's document, and clickjacking is the attack
// that costs nothing to close. The subscription page additionally carries
// `frame-ancestors 'none'` in its own CSP (subscription.go), which is the modern
// spelling of the same rule; this header covers the rest of the panel and the
// browsers that only know the old one.
func denyFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func spaHandler() http.Handler {
	dist, _ := fs.Sub(web.Dist, "dist")
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := dist.Open(p); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
