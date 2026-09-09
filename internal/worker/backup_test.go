package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type backupFixture struct {
	st  *store.Store
	dir string
	w   *worker.Backup
	at  time.Time
	// dumps counts how many times the fake pg_dump ran.
	dumps int
	err   error
}

func newBackupFixture(t *testing.T) *backupFixture {
	t.Helper()
	now := time.Now().UTC()
	f := &backupFixture{
		st:  store.OpenTest(t),
		dir: t.TempDir(),
		at:  time.Date(now.Year(), now.Month(), now.Day(), 3, 30, 0, 0, time.UTC),
	}
	r := &backup.Runner{
		DatabaseURL: "postgres://u:p@localhost/db",
		Dir:         f.dir,
		Exec: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			f.dumps++
			if f.err != nil {
				return []byte("pg_dump: error: disk full"), f.err
			}
			for i, a := range args {
				if a == "--file" && i+1 < len(args) {
					return nil, os.WriteFile(args[i+1], []byte("dump"), 0o600)
				}
			}
			return nil, errors.New("no --file")
		},
	}
	f.w = worker.NewBackup(f.st, r, slog.New(slog.DiscardHandler))
	f.w.SetNow(func() time.Time { return f.at })
	return f
}

func (f *backupFixture) setSchedule(t *testing.T, s backup.Schedule) {
	t.Helper()
	raw, _ := json.Marshal(s)
	if err := f.st.Q.UpsertSetting(context.Background(), db.UpsertSettingParams{Key: "backup_schedule", Value: raw}); err != nil {
		t.Fatal(err)
	}
}

func (f *backupFixture) rows(t *testing.T) []db.Backup {
	t.Helper()
	rows, err := f.st.Q.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestBackupWorkerRunsAtTheScheduledHour(t *testing.T) {
	f := newBackupFixture(t)
	f.setSchedule(t, backup.Schedule{Enabled: true, Hour: 3, Keep: 7})

	ran, err := f.w.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("RunOnce = %v, %v; want true, nil", ran, err)
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Kind != db.BackupKindScheduled {
		t.Fatalf("rows %+v", rows)
	}
	if _, err := os.Stat(filepath.Join(f.dir, rows[0].Path)); err != nil {
		t.Fatalf("dump file: %v", err)
	}
	if rows[0].Size != int64(len("dump")) {
		t.Errorf("size %d", rows[0].Size)
	}

	// Later the same hour: the day's dump already exists, so nothing runs.
	f.at = f.at.Add(20 * time.Minute)
	ran, err = f.w.RunOnce(context.Background())
	if err != nil || ran {
		t.Fatalf("second RunOnce = %v, %v; want false, nil", ran, err)
	}
	if n := len(f.rows(t)); n != 1 {
		t.Fatalf("%d rows after the second tick, want 1", n)
	}
	if f.dumps != 1 {
		t.Fatalf("pg_dump ran %d times", f.dumps)
	}
}

func TestBackupWorkerSkipsWhenDisabledOrOffHour(t *testing.T) {
	f := newBackupFixture(t)

	// Never configured at all: the setting is absent, defaults are disabled.
	if ran, err := f.w.RunOnce(context.Background()); ran || err != nil {
		t.Fatalf("unconfigured RunOnce = %v, %v", ran, err)
	}
	f.setSchedule(t, backup.Schedule{Enabled: false, Hour: 3, Keep: 7})
	if ran, err := f.w.RunOnce(context.Background()); ran || err != nil {
		t.Fatalf("disabled RunOnce = %v, %v", ran, err)
	}
	f.setSchedule(t, backup.Schedule{Enabled: true, Hour: 4, Keep: 7})
	if ran, err := f.w.RunOnce(context.Background()); ran || err != nil {
		t.Fatalf("off-hour RunOnce = %v, %v", ran, err)
	}
	if f.dumps != 0 {
		t.Fatalf("pg_dump ran %d times while it should not have", f.dumps)
	}
	if n := len(f.rows(t)); n != 0 {
		t.Fatalf("%d rows", n)
	}
}

func TestBackupWorkerCountsOnlyScheduledDumpsAndRunsDaily(t *testing.T) {
	f := newBackupFixture(t)
	f.setSchedule(t, backup.Schedule{Enabled: true, Hour: 3, Keep: 7})
	if _, err := f.st.Q.InsertBackup(context.Background(), db.InsertBackupParams{Path: "manual.dump", Size: 1, Kind: db.BackupKindManual}); err != nil {
		t.Fatal(err)
	}

	if ran, err := f.w.RunOnce(context.Background()); !ran || err != nil {
		t.Fatalf("RunOnce = %v, %v; a manual dump must not stand in for the nightly one", ran, err)
	}
	f.at = f.at.Add(24 * time.Hour)
	if ran, err := f.w.RunOnce(context.Background()); !ran || err != nil {
		t.Fatalf("next-day RunOnce = %v, %v", ran, err)
	}
	if n := len(f.rows(t)); n != 3 {
		t.Fatalf("%d rows, want the manual dump plus two nightly ones", n)
	}
}

func TestBackupWorkerPrunesToKeep(t *testing.T) {
	f := newBackupFixture(t)
	f.setSchedule(t, backup.Schedule{Enabled: true, Hour: 3, Keep: 2})
	ctx := context.Background()

	// Two older nightly dumps plus one manual dump, all with files on disk.
	var old []db.Backup
	for i, name := range []string{"tgwp-old1-scheduled.dump", "tgwp-old2-scheduled.dump"} {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		row, err := f.st.Q.InsertBackup(ctx, db.InsertBackupParams{Path: name, Size: 3, Kind: db.BackupKindScheduled})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.Pool.Exec(ctx, `UPDATE backups SET created_at = $2 WHERE id = $1`, row.ID, f.at.Add(-time.Duration(48-24*i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
		old = append(old, row)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "manual.dump"), []byte("m"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.Q.InsertBackup(ctx, db.InsertBackupParams{Path: "manual.dump", Size: 1, Kind: db.BackupKindManual}); err != nil {
		t.Fatal(err)
	}

	if ran, err := f.w.RunOnce(ctx); !ran || err != nil {
		t.Fatalf("RunOnce = %v, %v", ran, err)
	}

	rows := f.rows(t)
	kinds := map[db.BackupKind]int{}
	names := map[string]bool{}
	for _, r := range rows {
		kinds[r.Kind]++
		names[r.Path] = true
	}
	if kinds[db.BackupKindScheduled] != 2 {
		t.Fatalf("%d scheduled rows, want keep=2: %+v", kinds[db.BackupKindScheduled], rows)
	}
	if kinds[db.BackupKindManual] != 1 || !names["manual.dump"] {
		t.Fatalf("the manual dump was pruned: %+v", rows)
	}
	if names[old[0].Path] {
		t.Errorf("oldest nightly dump %s was kept", old[0].Path)
	}
	if _, err := os.Stat(filepath.Join(f.dir, old[0].Path)); !os.IsNotExist(err) {
		t.Errorf("oldest dump file still on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "manual.dump")); err != nil {
		t.Errorf("manual dump file removed: %v", err)
	}
}

func TestBackupWorkerReportsDumpFailureWithoutARow(t *testing.T) {
	f := newBackupFixture(t)
	f.setSchedule(t, backup.Schedule{Enabled: true, Hour: 3, Keep: 7})
	f.err = errors.New("exit status 1")

	ran, err := f.w.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce swallowed the pg_dump failure")
	}
	if ran {
		t.Error("RunOnce reported a backup it did not take")
	}
	if n := len(f.rows(t)); n != 0 {
		t.Fatalf("%d rows after a failed dump", n)
	}
	// The failure must not count as "done for today": the next tick retries.
	f.err = nil
	f.at = f.at.Add(10 * time.Minute)
	if ran, err := f.w.RunOnce(context.Background()); !ran || err != nil {
		t.Fatalf("retry RunOnce = %v, %v", ran, err)
	}
}
