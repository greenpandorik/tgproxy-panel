package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/store/db"
)

const backupTestURL = "postgres://tgwp:s3cret@localhost:5432/tgwp?sslmode=disable"

type backupEntryJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

func backupHarness(t *testing.T, exec func(ctx context.Context, name string, args ...string) ([]byte, error)) (*apitest.Harness, string) {
	t.Helper()
	dir := t.TempDir()
	h := apitest.New(t, func(d *api.Deps) {
		d.Backups = &backup.Runner{DatabaseURL: backupTestURL, Dir: dir, Exec: exec}
	})
	return h, dir
}

func fakeDump(contents string) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for i, a := range args {
			if a == "--file" && i+1 < len(args) {
				return nil, os.WriteFile(args[i+1], []byte(contents), 0o600)
			}
		}
		return nil, errors.New("no --file argument")
	}
}

func TestBackupCreateListDownloadDelete(t *testing.T) {
	h, dir := backupHarness(t, fakeDump("PGDMP-fake-dump"))
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	resp := c.Post("/api/v1/backups", nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create backup: %d", resp.StatusCode)
	}
	var created backupEntryJSON
	c.JSON(resp, &created)
	if created.ID == "" || created.Kind != "manual" || created.Size != int64(len("PGDMP-fake-dump")) {
		t.Fatalf("created entry %+v", created)
	}
	if !strings.HasSuffix(created.Name, "-manual.dump") || strings.ContainsRune(created.Name, filepath.Separator) {
		t.Fatalf("name %q, want a bare dump file name", created.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, created.Name)); err != nil {
		t.Fatalf("dump file: %v", err)
	}

	var list struct {
		Items []backupEntryJSON `json:"items"`
	}
	c.JSON(c.Get("/api/v1/backups"), &list)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("list %+v, want the created backup", list.Items)
	}

	dl := c.Get("/api/v1/backups/" + created.ID + "/download")
	if dl.StatusCode != 200 {
		t.Fatalf("download: %d", dl.StatusCode)
	}
	body, _ := io.ReadAll(dl.Body)
	_ = dl.Body.Close()
	if string(body) != "PGDMP-fake-dump" {
		t.Fatalf("download body %q", body)
	}
	if ct := dl.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content-type %q", ct)
	}
	disp, params, err := mime.ParseMediaType(dl.Header.Get("Content-Disposition"))
	if err != nil || disp != "attachment" || params["filename"] != created.Name {
		t.Errorf("content-disposition %q (%v)", dl.Header.Get("Content-Disposition"), err)
	}

	if resp := c.Delete("/api/v1/backups/" + created.ID); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, created.Name)); !os.IsNotExist(err) {
		t.Errorf("dump file still on disk after delete: %v", err)
	}
	c.JSON(c.Get("/api/v1/backups"), &list)
	if len(list.Items) != 0 {
		t.Errorf("list after delete: %+v", list.Items)
	}
	if resp := c.Get("/api/v1/backups/" + created.ID + "/download"); resp.StatusCode != 404 {
		t.Errorf("download after delete: %d", resp.StatusCode)
	}

	var audit struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/audit?action=backup."), &audit)
	seen := map[string]bool{}
	for _, a := range audit.Items {
		seen[a.Action] = true
	}
	for _, want := range []string{"backup.create", "backup.download", "backup.delete"} {
		if !seen[want] {
			t.Errorf("missing audit entry %s (got %v)", want, seen)
		}
	}
}

func TestBackupRoutesAreOwnerOnly(t *testing.T) {
	h, _ := backupHarness(t, fakeDump("dump"))
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")
	var created backupEntryJSON
	owner.JSON(owner.Post("/api/v1/backups", nil), &created)

	h.CreateAdmin("adm", "pass-123456", "admin")
	admin := h.Login("adm", "pass-123456")
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/backups"},
		{http.MethodPost, "/api/v1/backups"},
		{http.MethodGet, "/api/v1/backups/" + created.ID + "/download"},
		{http.MethodDelete, "/api/v1/backups/" + created.ID},
	}
	for _, tc := range cases {
		resp := admin.Do(tc.method, tc.path, nil)
		if statusOf(t, resp) != http.StatusForbidden {
			t.Errorf("%s %s as admin: %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestBackupCreateRejectsAConcurrentRun(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	h, _ := backupHarness(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Only the first dump blocks; the run after the slot is released must not.
		once.Do(func() {
			close(started)
			<-release
		})
		return fakeDump("dump")(ctx, name, args...)
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	done := make(chan int, 1)
	go func() {
		resp := c.Post("/api/v1/backups", nil)
		done <- statusOf(t, resp)
	}()
	<-started

	second := h.Login("root", "pass-123456")
	resp := second.Post("/api/v1/backups", nil)
	code, status := errCodeOf(t, resp)
	if status != http.StatusConflict || code != "backup_running" {
		t.Fatalf("second create: %d/%q, want 409/backup_running", status, code)
	}

	close(release)
	if got := <-done; got != 201 {
		t.Fatalf("first create: %d", got)
	}
	// The slot must be released once the first run finishes.
	if resp := c.Post("/api/v1/backups", nil); resp.StatusCode != 201 {
		t.Fatalf("create after the run finished: %d", resp.StatusCode)
	}
}

func TestBackupCreateFailureDoesNotLeakTheDatabaseURL(t *testing.T) {
	h, _ := backupHarness(t, func(context.Context, string, ...string) ([]byte, error) {
		return []byte("pg_dump: error: connection to " + backupTestURL + " failed"), errors.New("exit status 1")
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	resp := c.Post("/api/v1/backups", nil)
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "s3cret") || strings.Contains(string(body), backupTestURL) {
		t.Fatalf("error body leaks the database URL: %s", body)
	}
	var list struct {
		Items []backupEntryJSON `json:"items"`
	}
	c.JSON(c.Get("/api/v1/backups"), &list)
	if len(list.Items) != 0 {
		t.Errorf("a failed dump left a row behind: %+v", list.Items)
	}
}

// A row whose file was removed from the host by hand must not 500 the download and must still be deletable.
func TestBackupDownloadMissingFileIsNotFound(t *testing.T) {
	h, dir := backupHarness(t, fakeDump("dump"))
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	var created backupEntryJSON
	c.JSON(c.Post("/api/v1/backups", nil), &created)
	if err := os.Remove(filepath.Join(dir, created.Name)); err != nil {
		t.Fatal(err)
	}

	if resp := c.Get("/api/v1/backups/" + created.ID + "/download"); resp.StatusCode != 404 {
		t.Errorf("download: %d, want 404", resp.StatusCode)
	}
	if resp := c.Delete("/api/v1/backups/" + created.ID); resp.StatusCode != 204 {
		t.Errorf("delete: %d, want 204", resp.StatusCode)
	}
}

func TestBackupUnknownIDs(t *testing.T) {
	h, _ := backupHarness(t, fakeDump("dump"))
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	for _, path := range []string{"/api/v1/backups/not-a-uuid/download", "/api/v1/backups/00000000-0000-0000-0000-000000000000/download"} {
		if resp := c.Get(path); resp.StatusCode != 404 {
			t.Errorf("GET %s: %d, want 404", path, resp.StatusCode)
		}
	}
	if resp := c.Delete("/api/v1/backups/00000000-0000-0000-0000-000000000000"); resp.StatusCode != 404 {
		t.Errorf("delete unknown: %d, want 404", resp.StatusCode)
	}
}

func TestSettingsBackupSchedule(t *testing.T) {
	h, _ := backupHarness(t, fakeDump("dump"))
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	var got struct {
		BackupSchedule backup.Schedule `json:"backup_schedule"`
	}
	c.JSON(c.Get("/api/v1/settings"), &got)
	if got.BackupSchedule != (backup.Schedule{Enabled: false, Hour: 3, Keep: 7}) {
		t.Fatalf("default schedule %+v", got.BackupSchedule)
	}

	resp := c.Put("/api/v1/settings", map[string]any{
		"backup_schedule": map[string]any{"enabled": true, "hour": 11, "keep": 30},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	c.JSON(resp, &got)
	if got.BackupSchedule != (backup.Schedule{Enabled: true, Hour: 11, Keep: 30}) {
		t.Fatalf("saved schedule %+v", got.BackupSchedule)
	}
	c.JSON(c.Get("/api/v1/settings"), &got)
	if got.BackupSchedule != (backup.Schedule{Enabled: true, Hour: 11, Keep: 30}) {
		t.Fatalf("reloaded schedule %+v", got.BackupSchedule)
	}
	raw, err := h.Store.Q.GetSetting(t.Context(), "backup_schedule")
	if err != nil {
		t.Fatal(err)
	}
	if s := backup.ParseSchedule(raw); !s.Enabled || s.Hour != 11 || s.Keep != 30 {
		t.Fatalf("stored value %s parses as %+v", raw, s)
	}

	for _, bad := range []map[string]any{
		{"enabled": true, "hour": 24, "keep": 7},
		{"enabled": true, "hour": -1, "keep": 7},
		{"enabled": true, "hour": 3, "keep": 0},
		{"enabled": true, "hour": 3, "keep": 61},
	} {
		resp := c.Put("/api/v1/settings", map[string]any{"backup_schedule": bad})
		if resp.StatusCode != 422 {
			t.Errorf("put %v: %d, want 422", bad, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	// The rejected values must not have overwritten the saved schedule.
	c.JSON(c.Get("/api/v1/settings"), &got)
	if got.BackupSchedule != (backup.Schedule{Enabled: true, Hour: 11, Keep: 30}) {
		t.Fatalf("schedule after rejected writes %+v", got.BackupSchedule)
	}
}

func TestSettingsBackupScheduleToleratesTheOldStringValue(t *testing.T) {
	h, _ := backupHarness(t, fakeDump("dump"))
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	raw, _ := json.Marshal("0 3 * * *")
	if err := h.Store.Q.UpsertSetting(t.Context(), db.UpsertSettingParams{Key: "backup_schedule", Value: raw}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		BackupSchedule backup.Schedule `json:"backup_schedule"`
	}
	c.JSON(c.Get("/api/v1/settings"), &got)
	if got.BackupSchedule != backup.DefaultSchedule() {
		t.Fatalf("legacy value produced %+v, want the defaults", got.BackupSchedule)
	}
}
