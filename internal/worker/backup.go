package worker

import (
	"context"
	"log/slog"
	"time"

	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

// backupTick is how often the worker asks whether the nightly dump is due.
const backupTick = 10 * time.Minute

type Backup struct {
	st     *store.Store
	runner *backup.Runner
	log    *slog.Logger
	now    func() time.Time
}

func NewBackup(st *store.Store, r *backup.Runner, log *slog.Logger) *Backup {
	return &Backup{st: st, runner: r, log: log, now: time.Now}
}

// SetNow overrides the clock; tests drive the schedule with it.
func (b *Backup) SetNow(f func() time.Time) { b.now = f }

// RunOnce takes the nightly dump when it is due and reports whether it did.
func (b *Backup) RunOnce(ctx context.Context) (bool, error) {
	sched := backup.DefaultSchedule()
	if raw, err := b.st.Q.GetSetting(ctx, "backup_schedule"); err == nil {
		sched = backup.ParseSchedule(raw)
	}
	if !sched.Enabled {
		return false, nil
	}
	now := b.now().UTC()
	if now.Hour() != sched.Hour {
		return false, nil
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	n, err := b.st.Q.CountBackupsSince(ctx, db.CountBackupsSinceParams{Kind: db.BackupKindScheduled, CreatedAt: midnight})
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}

	e, err := b.runner.Create(ctx, backup.KindScheduled)
	if err != nil {
		return false, err
	}
	if _, err := b.st.Q.InsertBackup(ctx, db.InsertBackupParams{Path: e.Name, Size: e.Size, Kind: db.BackupKind(e.Kind)}); err != nil {
		return false, err
	}
	if err := b.prune(ctx, sched.Keep); err != nil {
		b.log.Error("prune backups", "err", err)
	}
	return true, nil
}

// prune enforces Keep over the scheduled dumps only.
func (b *Backup) prune(ctx context.Context, keep int) error {
	rows, err := b.st.Q.ListBackups(ctx)
	if err != nil {
		return err
	}
	entries := make([]backup.Entry, 0, len(rows))
	for _, r := range rows {
		if r.Kind != db.BackupKindScheduled {
			continue
		}
		path, err := b.runner.Path(r.Path)
		if err != nil {
			b.log.Warn("backup row names a file outside the backup directory", "backup", r.ID, "err", err)
			continue
		}
		entries = append(entries, backup.Entry{ID: r.ID, Path: path, Name: r.Path, Size: r.Size, Kind: string(r.Kind), CreatedAt: r.CreatedAt})
	}
	_, err = b.runner.Prune(ctx, keep, entries, func(ctx context.Context, e backup.Entry) error {
		_, err := b.st.Q.DeleteBackup(ctx, e.ID)
		return err
	})
	return err
}

func (b *Backup) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := b.RunOnce(ctx); err != nil {
			b.log.Error("backup", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
