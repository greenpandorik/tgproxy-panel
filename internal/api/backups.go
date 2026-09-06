package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/store/db"
)

// backupTimeout is the ceiling on one pg_dump run. Generous - a dump is bounded
// by disk, not by the panel - but finite, so a hung client tool cannot block
// every later backup.
const backupTimeout = 30 * time.Minute

// Backups are the whole database, encrypted secrets and all, so every route
// here is owner-only - including the reads, which is why they carry an explicit
// RequireRole rather than relying on the auth group alone.
func (s *Server) mountBackups(r chi.Router) {
	r.With(RequireRole(RoleOwner)).Get("/backups", s.handleListBackups)
	r.With(RequireRole(RoleOwner)).Post("/backups", s.handleCreateBackup)
	r.With(RequireRole(RoleOwner)).Get("/backups/{id}/download", s.handleDownloadBackup)
	r.With(RequireRole(RoleOwner)).Delete("/backups/{id}", s.handleDeleteBackup)
}

func backupJSON(b db.Backup) map[string]any {
	return map[string]any{
		"id":         b.ID,
		"name":       b.Path,
		"size":       b.Size,
		"kind":       string(b.Kind),
		"created_at": b.CreatedAt,
	}
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListBackups(r.Context())
	if err != nil {
		internal(w)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		items = append(items, backupJSON(b))
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// handleCreateBackup runs pg_dump inline. It can take minutes on a large
// database, so it is serialised by backupSlot: a second click while one is in
// flight gets 409 backup_running rather than a second dump over the same
// directory. The nightly worker is not gated by this slot - it writes a
// -scheduled name and a manual dump writes a -manual one, so the rare overlap
// costs some IO and nothing else.
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	select {
	case s.backupSlot <- struct{}{}:
		defer func() { <-s.backupSlot }()
	default:
		writeError(w, 409, "backup_running", "a backup is already running", nil)
		return
	}

	// Detached from the request: a dump of a large database can outlast a browser
	// tab or a reverse-proxy read timeout, and killing pg_dump halfway through
	// leaves the operator with nothing to show for minutes of IO. The slot is held
	// for the whole run either way, so nothing else starts behind it. The ceiling
	// is there so a wedged pg_dump cannot hold that slot forever.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), backupTimeout)
	defer cancel()

	e, err := s.backups.Create(ctx, backup.KindManual)
	if err != nil {
		s.log.Error("backup create", "err", err)
		// err is already scrubbed of the database URL by the runner, and it is the
		// only thing that says why pg_dump failed - a bare "internal error" would
		// leave the operator with nothing to act on.
		writeError(w, 500, "backup_failed", err.Error(), nil)
		return
	}
	row, err := s.store.Q.InsertBackup(ctx, db.InsertBackupParams{Path: e.Name, Size: e.Size, Kind: db.BackupKind(e.Kind)})
	if err != nil {
		// No row means no way to find the file again from the UI; drop it rather
		// than leaving an orphan taking up disk.
		_ = os.Remove(e.Path)
		internal(w)
		return
	}
	s.Audit(ctx, "backup.create", "backup", row.ID.String(), map[string]any{"name": row.Path, "size": row.Size, "kind": string(row.Kind)})
	writeJSON(w, 201, backupJSON(row))
}

func (s *Server) loadBackup(w http.ResponseWriter, r *http.Request) (db.Backup, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.Backup{}, false
	}
	row, err := s.store.Q.GetBackup(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.Backup{}, false
	}
	return row, true
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	row, ok := s.loadBackup(w, r)
	if !ok {
		return
	}
	path, err := s.backups.Path(row.Path)
	if err != nil {
		notFound(w)
		return
	}
	f, err := os.Open(path) //nolint:gosec // path is resolved inside the backup dir by Runner.Path
	if err != nil {
		// The row outlived its file (removed on the host by hand, or a volume
		// swapped out). 404 is the honest answer; the row can still be deleted.
		notFound(w)
		return
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil {
		internal(w)
		return
	}

	s.Audit(r.Context(), "backup.download", "backup", row.ID.String(), map[string]any{"name": row.Path})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(row.Path)}))
	// The dump contains every key and profile in the fleet; no cache, anywhere.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, f); err != nil {
		s.log.Warn("backup download interrupted", "backup", row.ID, "err", err)
	}
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	row, ok := s.loadBackup(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if path, err := s.backups.Path(row.Path); err == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Error("backup delete file", "backup", row.ID, "err", err)
			internal(w)
			return
		}
	}
	n, err := s.store.Q.DeleteBackup(ctx, row.ID)
	if err != nil {
		internal(w)
		return
	}
	if n == 0 {
		notFound(w)
		return
	}
	s.Audit(ctx, "backup.delete", "backup", row.ID.String(), map[string]any{"name": row.Path})
	w.WriteHeader(204)
}

// backupSchedule reads the stored schedule, falling back to the defaults when
// the key is absent or holds a value from before it grew fields.
func (s *Server) backupSchedule(ctx context.Context) backup.Schedule {
	raw, err := s.store.Q.GetSetting(ctx, settingBackupSchedule)
	if err != nil {
		return backup.DefaultSchedule()
	}
	return backup.ParseSchedule(raw)
}
