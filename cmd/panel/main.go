package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"tgwebproxy/internal/admincli"
	"tgwebproxy/internal/api"
	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/config"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/gateway"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/logging"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/notify"
	"tgwebproxy/internal/settings"
	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/updates"
	"tgwebproxy/internal/worker"
	agentv1 "tgwebproxy/proto/agent/v1"
)

// maxGRPCMessageBytes is the explicit ceiling on agent-stream envelopes in both directions.
const maxGRPCMessageBytes = 16 << 20

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logging.New(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := store.Migrate(ctx, st.Pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := store.SeedPresets(ctx, st, sitekit.Presets()); err != nil {
		return fmt.Errorf("seed presets: %w", err)
	}

	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "migrate":
		fmt.Println("migrations applied")
		return nil
	case "admin":
		return adminCmd(ctx, st, args[1:])
	case "db":
		return dbCmd(ctx, cfg, st, args[1:])
	case "keys":
		return keysCmd(ctx, cfg, st, args[1:])
	case "serve":
		return serve(ctx, cfg, st, log)
	}
	return fmt.Errorf("unknown command %q (serve|migrate|admin create|admin totp-reset|db backup|db restore|keys rotate)", cmd)
}

// adminRoles is the enum accepted by db.AdminRole.
var adminRoles = []string{api.RoleOwner, api.RoleAdmin, api.RoleViewer}

const adminUsage = "usage: panel admin create <username> <password> [owner|admin|viewer]\n       panel admin totp-reset <username>"

func adminCmd(ctx context.Context, st *store.Store, args []string) error {
	if len(args) == 0 {
		return errors.New(adminUsage)
	}
	switch args[0] {
	case "create":
		return adminCreate(ctx, st, args[1:])
	case "totp-reset":
		return adminTOTPReset(ctx, st, args[1:])
	}
	return errors.New(adminUsage)
}

func adminTOTPReset(ctx context.Context, st *store.Store, args []string) error {
	if len(args) != 1 {
		return errors.New(adminUsage)
	}
	u, err := admincli.ResetTOTP(ctx, st, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("two-factor authentication disabled for %s; recovery codes deleted\n", u.Username)
	return nil
}

func adminCreate(ctx context.Context, st *store.Store, args []string) error {
	if len(args) < 2 {
		return errors.New(adminUsage)
	}
	role := api.RoleOwner
	if len(args) > 2 {
		role = args[2]
	}
	if !slices.Contains(adminRoles, role) {
		return fmt.Errorf("unknown role %q, want one of %s", role, strings.Join(adminRoles, "|"))
	}
	hash, err := crypto.HashPassword(args[1])
	if err != nil {
		return err
	}
	u, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: args[0], PasswordHash: hash, Role: db.AdminRole(role)})
	if err != nil {
		return err
	}
	fmt.Println("created admin", u.Username, "role", u.Role)
	return nil
}

const dbUsage = "usage: panel db backup\n       panel db restore <file> --yes"

func dbCmd(ctx context.Context, cfg config.Config, st *store.Store, args []string) error {
	if len(args) == 0 {
		return errors.New(dbUsage)
	}
	runner := &backup.Runner{DatabaseURL: cfg.DatabaseURL, Dir: backup.DirFor(cfg.DataDir)}
	switch args[0] {
	case "backup":
		if len(args) != 1 {
			return errors.New(dbUsage)
		}
		return dbBackup(ctx, runner, st)
	case "restore":
		return dbRestore(ctx, cfg, runner, st, args[1:])
	}
	return errors.New(dbUsage)
}

func dbBackup(ctx context.Context, runner *backup.Runner, st *store.Store) error {
	e, err := runner.Create(ctx, backup.KindManual)
	if err != nil {
		return err
	}
	if _, err := st.Q.InsertBackup(ctx, db.InsertBackupParams{Path: e.Name, Size: e.Size, Kind: db.BackupKindManual}); err != nil {
		return fmt.Errorf("dump written to %s but the backups row failed: %w", e.Path, err)
	}
	fmt.Printf("backup written: %s (%d bytes)\n", e.Path, e.Size)
	return nil
}

func dbRestore(ctx context.Context, cfg config.Config, runner *backup.Runner, st *store.Store, args []string) error {
	file, yes := "", false
	for _, a := range args {
		switch {
		case a == "--yes":
			yes = true
		case file == "" && !strings.HasPrefix(a, "-"):
			file = a
		default:
			return errors.New(dbUsage)
		}
	}
	if file == "" {
		return errors.New(dbUsage)
	}
	if !yes {
		return errors.New("refusing to restore without --yes: pg_restore --clean drops every existing table first, so this replaces the current database")
	}
	release, err := requireNoPanel(ctx, st, "restore")
	if err != nil {
		return err
	}
	// Hand the database over to pg_restore rather than holding a pool open across the DROPs it is about to issue.
	release()
	if st != nil {
		st.Close()
	}
	if err := runner.Restore(ctx, file); err != nil {
		return err
	}
	fmt.Println("restore complete; restart the panel so it picks up the restored state")
	return nil
}

const keysUsage = "usage: panel keys rotate [--dry-run]"

func keysCmd(ctx context.Context, cfg config.Config, st *store.Store, args []string) error {
	if len(args) == 0 {
		return errors.New(keysUsage)
	}
	switch args[0] {
	case "rotate":
		return keysRotate(ctx, cfg, st, args[1:])
	}
	return errors.New(keysUsage)
}

func keysRotate(ctx context.Context, cfg config.Config, st *store.Store, args []string) error {
	dryRun := false
	for _, a := range args {
		if a != "--dry-run" {
			return errors.New(keysUsage)
		}
		dryRun = true
	}
	release, err := requireNoPanel(ctx, st, "rotate")
	if err != nil {
		return err
	}
	defer release()

	newKeyB64 := strings.TrimSpace(os.Getenv("MASTER_KEY_NEW"))
	if newKeyB64 == "" {
		return errors.New("MASTER_KEY_NEW is required (base64 of 32 random bytes; generate with: openssl rand -base64 32)")
	}
	newKey, err := base64.StdEncoding.DecodeString(newKeyB64)
	if err != nil {
		return errors.New("MASTER_KEY_NEW must be standard base64")
	}
	if len(newKey) != 32 {
		return fmt.Errorf("MASTER_KEY_NEW must decode to 32 bytes, got %d", len(newKey))
	}
	if bytes.Equal(newKey, cfg.MasterKey) {
		return errors.New("MASTER_KEY_NEW must not equal the current MASTER_KEY - rotation would encrypt every row under a new version number but the same key material, which is pointless and likely means the wrong env var was set")
	}
	newVersion := cfg.MasterKeyVersion + 1

	keyMap := map[int][]byte{}
	for v, k := range cfg.OldMasterKeys {
		keyMap[v] = k
	}
	keyMap[cfg.MasterKeyVersion] = cfg.MasterKey
	from, err := crypto.NewBox(cfg.MasterKeyVersion, keyMap)
	if err != nil {
		return err
	}
	keyMap[newVersion] = newKey
	to, err := crypto.NewBox(newVersion, keyMap)
	if err != nil {
		return err
	}

	if dryRun {
		report, err := store.CountPending(ctx, st, to)
		if err != nil {
			return err
		}
		printKeysReport("rows pending rotation (dry run: nothing was written)", report)
		return nil
	}

	report, err := store.Rotate(ctx, st, from, to)
	if err != nil {
		return err
	}
	printKeysReport("rows re-encrypted", report)
	fmt.Println()
	fmt.Println("set these in the environment before the next start, then drop any older")
	fmt.Println("MASTER_KEY_V<n> that no row references any more:")
	fmt.Println("MASTER_KEY=<new key>")
	fmt.Printf("MASTER_KEY_VERSION=%d\n", newVersion)
	fmt.Printf("MASTER_KEY_V%d=<old key>\n", cfg.MasterKeyVersion)
	return nil
}

func printKeysReport(label string, r store.Report) {
	fmt.Println(label + ":")
	fmt.Printf("  profiles.secret_enc:          %d\n", r.Profiles)
	fmt.Printf("  access_keys.secret_enc:       %d\n", r.Keys)
	fmt.Printf("  admin_users.totp_secret_enc:  %d\n", r.TOTPSecrets)
	fmt.Printf("  admin_users.totp_pending_enc: %d\n", r.TOTPPending)
	fmt.Printf("  settings.telegram_alerts:     %d\n", r.Settings)
}

const panelAdvisoryLockID int64 = 0x7467_7770_0000_0001

// requireNoPanel is the guard rail in front of the two destructive commands.
func requireNoPanel(ctx context.Context, st *store.Store, verb string) (func(), error) {
	if st == nil {
		return func() {}, nil
	}
	lock, ok, err := st.TryAdvisoryLock(ctx, panelAdvisoryLockID)
	if err != nil {
		return nil, fmt.Errorf("checking whether the panel is running: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("the panel holds the database lock, so it is still running; stop the panel first, then %s", verb)
	}
	return lock.Release, nil
}

func serve(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) error {
	lock, ok, err := st.TryAdvisoryLock(ctx, panelAdvisoryLockID)
	if err != nil {
		return fmt.Errorf("panel lock: %w", err)
	}
	if !ok {
		return errors.New("another panel instance holds the lock on this database; stop it before starting a second one")
	}
	defer lock.Release()

	masterKeys := map[int][]byte{}
	for v, k := range cfg.OldMasterKeys {
		masterKeys[v] = k
	}
	masterKeys[cfg.MasterKeyVersion] = cfg.MasterKey
	box, err := crypto.NewBox(cfg.MasterKeyVersion, masterKeys)
	if err != nil {
		return err
	}

	reg := gateway.NewRegistry()
	presence := nodesvc.NewPresence(st, log)
	var driver nodedriver.Driver
	if cfg.NodeDriver == "mock" {
		driver = nodedriver.NewMock()
	} else {
		driver = nodedriver.NewGateway(reg, 15*time.Second)
	}
	grpcSrv := grpc.NewServer(grpc.MaxRecvMsgSize(maxGRPCMessageBytes), grpc.MaxSendMsgSize(maxGRPCMessageBytes))
	agentv1.RegisterAgentGatewayServer(grpcSrv, gateway.NewServer(reg, presence, presence, log))

	keySvc := keys.New(st, box)
	settingsReader := settings.New(st)
	applyW := worker.NewApply(st, box, driver, time.Duration(cfg.ApplyInterval)*time.Second, log)
	applyW.SetIntervalFunc(func(ctx context.Context) time.Duration {
		return settingsReader.Duration(ctx, "apply_interval", time.Duration(cfg.ApplyInterval)*time.Second)
	})
	statsW := worker.NewStats(st, driver, time.Duration(cfg.OfflineAfter)*time.Second, log)
	statsW.SetOfflineAfterFunc(func(ctx context.Context) time.Duration {
		return settingsReader.Duration(ctx, "offline_after", time.Duration(cfg.OfflineAfter)*time.Second)
	})
	tg := notify.NewTelegram(&http.Client{Timeout: 10 * time.Second}, "")
	backupRunner := &backup.Runner{DatabaseURL: cfg.DatabaseURL, Dir: backup.DirFor(cfg.DataDir)}
	deps := api.Deps{
		Store: st, Box: box, Signer: crypto.NewSigner(cfg.SessionSecret), Log: log, Cfg: cfg, Driver: driver, Presence: presence,
		Keys: keySvc, ApplyNow: func(ctx context.Context, id uuid.UUID) { applyW.Trigger(id) }, Notifier: tg,
		Backups: backupRunner,
	}
	if cfg.UpdateCheck {
		deps.Updates = updates.New(cfg.GitHubRepo, cfg.GitHubToken)
		deps.Updates.Log = log
	}
	srv := api.New(deps)
	alerts := worker.NewAlerts(srv.TelegramConfig, tg, log)
	applyW.SetAlerts(alerts)
	statsW.SetAlerts(alerts)
	stopWorkers := worker.Start(ctx, applyW, worker.NewExpiry(st, keySvc, log), statsW, worker.NewBackup(st, backupRunner, log))
	httpHandler := srv.Handler()
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcSrv.ServeHTTP(w, r)
			return
		}
		httpHandler.ServeHTTP(w, r)
	})
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, Protocols: protocols, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		grpcSrv.GracefulStop()
	}()
	log.Info("panel listening", "addr", cfg.HTTPAddr)
	serveErr := httpSrv.ListenAndServe()
	if err := stopWorkers(context.Background()); err != nil {
		log.Warn("workers did not stop cleanly", "err", err)
	}
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}
