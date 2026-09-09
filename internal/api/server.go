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
	Notifier     telegramSender
	Backups      *backup.Runner
	NodeChecker  *nodecheck.Checker
	Updates      *updates.Checker
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
	backupSlot     chan struct{}
	publicStatus   publicStatusCache
	// updates is nil when the update check is disabled.
	updates *updates.Checker
}

func New(d Deps) *Server {
	s := &Server{
		store: d.Store, box: d.Box, signer: d.Signer, log: d.Log, cfg: d.Cfg,
		secureCookies: strings.HasPrefix(d.Cfg.PublicURL, "https://"),
		loginLimiter:  newIPLimiter(10, 10*time.Minute, 15*time.Minute),
		subLimiter:    newIPLimiter(60, time.Minute, time.Minute),
		driver:        d.Driver, presence: d.Presence, keys: d.Keys, applyNow: d.ApplyNow, siteProvider: d.SiteProvider,
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

// Handler builds the panel router.
func (s *Server) Handler() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, denyFraming, securityHeaders, withIP, s.loadSession, s.accessLog)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	r.Get("/metrics", s.handleMetrics)
	r.Get("/s/{token}", s.handleSubscriptionPage)
	r.Get("/s/{token}.json", s.handleSubscriptionJSON)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/totp/verify", s.handleTOTPVerify)
		r.Get("/install/{token}.sh", s.handleInstallScript)
		r.Post("/install/{token}/register", s.handleInstallRegister)
		r.Get("/install/agent/{platform}", s.handleAgentDownload)
		r.Get("/node/upgrade", s.handleNodeUpgrade)
		r.Get("/branding", s.handleGetActiveBranding)
		r.Get("/branding/assets/{id}/{file}", s.handleBrandingAsset)
		r.Get("/status/public", s.handlePublicStatus)
		r.Group(func(r chi.Router) {
			r.Use(requireAuth, csrfCheck)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/status/update", s.handleUpdateStatus)
			r.Post("/me/password", s.handleChangePassword)
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
		r.With(RequireRole(writers...)).Get("/registration-secret", s.handleNodeRegistrationSecret)
		r.Get("/health", s.handleNodeHealth)
		r.Get("/profiles", s.handleNodeProfiles)
		r.Get("/stats", s.handleNodeStats)
		r.Get("/metrics", s.handleNodeMetrics)
		r.Get("/logs", s.handleNodeLogs)
		r.Get("/jobs", s.handleNodeJobs)
		r.With(RequireRole(writers...)).Post("/restart", s.handleNodeRestart)
		r.With(RequireRole(writers...)).Post("/apply", s.handleNodeApply)
		r.Get("/web-policy", s.handleGetNodeWebPolicy)
		r.With(RequireRole(writers...)).Put("/web-policy", s.handlePutNodeWebPolicy)
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
func denyFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// spaCSP is the baseline policy.
const spaCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", spaCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
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
