package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/backup"
)

const testURL = "postgres://tgwp:s3cret@localhost:5432/tgwp?sslmode=disable"

// fakeExec records the command it was asked to run and, when the argument list
// carries a --file, creates that file with the given contents so Create can stat
// a real dump without a real pg_dump.
type fakeExec struct {
	name  string
	args  []string
	write string
	out   []byte
	err   error
	calls int
}

func (f *fakeExec) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls++
	f.name, f.args = name, args
	if f.err == nil {
		for i, a := range args {
			if a == "--file" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], []byte(f.write), 0o600); err != nil {
					return nil, err
				}
			}
		}
	}
	return f.out, f.err
}

func newRunner(t *testing.T, fe *fakeExec) *backup.Runner {
	t.Helper()
	return &backup.Runner{DatabaseURL: testURL, Dir: t.TempDir(), Exec: fe.run}
}

func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func TestCreateRunsPgDumpAndReturnsEntry(t *testing.T) {
	fe := &fakeExec{write: "dump-bytes"}
	r := newRunner(t, fe)

	e, err := r.Create(context.Background(), backup.KindManual)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fe.name != "pg_dump" {
		t.Errorf("ran %q, want pg_dump", fe.name)
	}
	joined := strings.Join(fe.args, " ")
	for _, want := range []string{"--format=custom", "--no-owner", "--file", testURL} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %v missing %q", fe.args, want)
		}
	}
	file, _ := argValue(fe.args, "--file")
	if filepath.Dir(file) != r.Dir {
		t.Errorf("dump written to %q, want inside %q", file, r.Dir)
	}
	if e.Path != file || e.Name != filepath.Base(file) {
		t.Errorf("entry %+v does not match the dump path %q", e, file)
	}
	if !strings.HasPrefix(e.Name, "tgwp-") || !strings.HasSuffix(e.Name, "-manual.dump") {
		t.Errorf("name %q, want tgwp-<ts>-manual.dump", e.Name)
	}
	if e.Size != int64(len("dump-bytes")) {
		t.Errorf("size %d, want %d", e.Size, len("dump-bytes"))
	}
	if e.Kind != backup.KindManual {
		t.Errorf("kind %q, want manual", e.Kind)
	}
	if time.Since(e.CreatedAt) > time.Minute || e.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at %v, want a recent UTC time", e.CreatedAt)
	}
}

func TestCreateNamesScheduledDumpsApart(t *testing.T) {
	fe := &fakeExec{write: "x"}
	r := newRunner(t, fe)
	e, err := r.Create(context.Background(), backup.KindScheduled)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasSuffix(e.Name, "-scheduled.dump") {
		t.Errorf("name %q, want a -scheduled.dump suffix", e.Name)
	}
}

func TestCreateRejectsUnknownKind(t *testing.T) {
	fe := &fakeExec{write: "x"}
	r := newRunner(t, fe)
	if _, err := r.Create(context.Background(), "weekly"); err == nil {
		t.Fatal("Create accepted an unknown kind")
	}
	if fe.calls != 0 {
		t.Errorf("pg_dump ran %d times for an invalid kind", fe.calls)
	}
}

// The dump command carries the database URL, password and all. A failure must
// surface pg_dump's own message without ever echoing that URL back to an API
// response, a log line or the CLI.
func TestCreateErrorCarriesStderrButNotTheURL(t *testing.T) {
	fe := &fakeExec{
		write: "partial",
		out:   []byte("pg_dump: error: connection to " + testURL + " failed: FATAL: role does not exist\n"),
		err:   errors.New("exit status 1"),
	}
	r := newRunner(t, fe)

	_, err := r.Create(context.Background(), backup.KindManual)
	if err == nil {
		t.Fatal("Create returned no error for a failed pg_dump")
	}
	if !strings.Contains(err.Error(), "role does not exist") {
		t.Errorf("error %q does not carry pg_dump's message", err)
	}
	for _, secret := range []string{"s3cret", testURL} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q leaks %q", err, secret)
		}
	}
	entries, _ := os.ReadDir(r.Dir)
	if len(entries) != 0 {
		t.Errorf("failed dump left %d file(s) behind: %v", len(entries), entries)
	}
}

func TestCreateErrorWithoutOutputStillFails(t *testing.T) {
	fe := &fakeExec{err: errors.New("exec: \"pg_dump\": executable file not found in $PATH")}
	r := newRunner(t, fe)
	if _, err := r.Create(context.Background(), backup.KindManual); err == nil {
		t.Fatal("Create returned no error when pg_dump could not run")
	}
}

// Two dumps of the same kind inside one second share a timestamp; the second
// must not overwrite the first file while its row still points at it.
func TestCreateNeverOverwritesAnExistingDump(t *testing.T) {
	fe := &fakeExec{write: "x"}
	r := newRunner(t, fe)
	first, err := r.Create(context.Background(), backup.KindManual)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := r.Create(context.Background(), backup.KindManual)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.Path == first.Path {
		t.Fatalf("both dumps landed on %s", first.Path)
	}
	if data, _ := os.ReadFile(first.Path); string(data) != "first" {
		t.Errorf("the first dump was overwritten: %q", data)
	}
}

func TestRestoreRunsPgRestore(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	path := filepath.Join(r.Dir, "tgwp-20260101T000000Z-manual.dump")
	if err := os.WriteFile(path, []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := r.Restore(context.Background(), path); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if fe.name != "pg_restore" {
		t.Errorf("ran %q, want pg_restore", fe.name)
	}
	joined := strings.Join(fe.args, " ")
	for _, want := range []string{"--clean", "--if-exists", "--no-owner", "--dbname", testURL, path} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %v missing %q", fe.args, want)
		}
	}
}

// A bare file name is the form the CLI and the runbook use.
func TestRestoreAcceptsABareFileName(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	if err := os.WriteFile(filepath.Join(r.Dir, "d.dump"), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Restore(context.Background(), "d.dump"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if fe.calls != 1 {
		t.Errorf("pg_restore ran %d times, want 1", fe.calls)
	}
}

func TestRestoreRejectsPathsOutsideDir(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	outside := filepath.Join(t.TempDir(), "elsewhere.dump")
	if err := os.WriteFile(outside, []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, "../../etc/passwd", "sub/../../escape.dump", ".."} {
		if err := r.Restore(context.Background(), path); err == nil {
			t.Errorf("Restore accepted %q", path)
		}
	}
	if fe.calls != 0 {
		t.Errorf("pg_restore ran %d times for rejected paths", fe.calls)
	}
}

func TestRestoreRejectsMissingFile(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	if err := r.Restore(context.Background(), "nope.dump"); err == nil {
		t.Fatal("Restore accepted a file that does not exist")
	}
	if fe.calls != 0 {
		t.Errorf("pg_restore ran %d times for a missing file", fe.calls)
	}
}

func TestRestoreErrorDoesNotLeakTheURL(t *testing.T) {
	fe := &fakeExec{out: []byte("pg_restore: error: could not connect to " + testURL), err: errors.New("exit status 1")}
	r := newRunner(t, fe)
	if err := os.WriteFile(filepath.Join(r.Dir, "d.dump"), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := r.Restore(context.Background(), "d.dump")
	if err == nil {
		t.Fatal("Restore returned no error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error %q leaks the password", err)
	}
}

func mkEntry(t *testing.T, dir, name string, age time.Duration) backup.Entry {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return backup.Entry{ID: uuid.New(), Path: path, Name: name, Size: 1, Kind: backup.KindScheduled, CreatedAt: time.Now().UTC().Add(-age)}
}

func TestPruneDeletesTheOldestBeyondKeep(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	// Deliberately unsorted: Prune must order by created_at itself.
	entries := []backup.Entry{
		mkEntry(t, r.Dir, "b.dump", 2*time.Hour),
		mkEntry(t, r.Dir, "d.dump", 4*time.Hour),
		mkEntry(t, r.Dir, "a.dump", time.Hour),
		mkEntry(t, r.Dir, "c.dump", 3*time.Hour),
	}
	var deleted []string
	n, err := r.Prune(context.Background(), 2, entries, func(_ context.Context, e backup.Entry) error {
		deleted = append(deleted, e.Name)
		return nil
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 2 || len(deleted) != 2 {
		t.Fatalf("removed %d rows %v, want c.dump and d.dump", n, deleted)
	}
	if deleted[0] != "c.dump" || deleted[1] != "d.dump" {
		t.Errorf("deleted %v, want [c.dump d.dump] (oldest last)", deleted)
	}
	for _, name := range []string{"a.dump", "b.dump"} {
		if _, err := os.Stat(filepath.Join(r.Dir, name)); err != nil {
			t.Errorf("%s should have been kept: %v", name, err)
		}
	}
	for _, name := range []string{"c.dump", "d.dump"} {
		if _, err := os.Stat(filepath.Join(r.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s file should have been removed, stat err = %v", name, err)
		}
	}
}

func TestPruneKeepsEverythingWhenUnderTheLimit(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	entries := []backup.Entry{mkEntry(t, r.Dir, "a.dump", time.Hour)}
	n, err := r.Prune(context.Background(), 7, entries, func(context.Context, backup.Entry) error {
		t.Error("del called while under the limit")
		return nil
	})
	if err != nil || n != 0 {
		t.Fatalf("Prune = %d, %v; want 0, nil", n, err)
	}
}

// keep <= 0 would otherwise mean "delete everything"; a misconfigured setting
// must never wipe the backup history.
func TestPruneIgnoresNonPositiveKeep(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	entries := []backup.Entry{mkEntry(t, r.Dir, "a.dump", time.Hour), mkEntry(t, r.Dir, "b.dump", 2*time.Hour)}
	n, err := r.Prune(context.Background(), 0, entries, func(context.Context, backup.Entry) error {
		t.Error("del called with keep=0")
		return nil
	})
	if err != nil || n != 0 {
		t.Fatalf("Prune = %d, %v; want 0, nil", n, err)
	}
}

// A dump deleted from disk by hand still has a row; pruning must clear the row
// rather than stopping on the missing file.
func TestPruneRemovesTheRowWhenTheFileIsGone(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	entries := []backup.Entry{mkEntry(t, r.Dir, "a.dump", time.Hour), mkEntry(t, r.Dir, "b.dump", 2*time.Hour)}
	if err := os.Remove(entries[1].Path); err != nil {
		t.Fatal(err)
	}
	var deleted int
	n, err := r.Prune(context.Background(), 1, entries, func(context.Context, backup.Entry) error {
		deleted++
		return nil
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 || deleted != 1 {
		t.Fatalf("removed %d rows (del called %d times), want 1", n, deleted)
	}
}

func TestPruneReportsDeleteFailures(t *testing.T) {
	fe := &fakeExec{}
	r := newRunner(t, fe)
	entries := []backup.Entry{mkEntry(t, r.Dir, "a.dump", time.Hour), mkEntry(t, r.Dir, "b.dump", 2*time.Hour)}
	if _, err := r.Prune(context.Background(), 1, entries, func(context.Context, backup.Entry) error {
		return errors.New("boom")
	}); err == nil {
		t.Fatal("Prune swallowed the row delete error")
	}
}

func TestScheduleDefaults(t *testing.T) {
	def := backup.DefaultSchedule()
	if def.Enabled || def.Hour != 3 || def.Keep != 7 {
		t.Fatalf("defaults %+v, want {false 3 7}", def)
	}
	for _, raw := range []string{"", `""`, `"daily"`, `{`, `null`, `{"enabled":true}`} {
		got := backup.ParseSchedule([]byte(raw))
		if raw == `{"enabled":true}` {
			if !got.Enabled || got.Hour != 3 || got.Keep != 7 {
				t.Errorf("ParseSchedule(%q) = %+v, want enabled with defaults filled in", raw, got)
			}
			continue
		}
		if got != def {
			t.Errorf("ParseSchedule(%q) = %+v, want the defaults", raw, got)
		}
	}
	if got := backup.ParseSchedule([]byte(`{"enabled":true,"hour":11,"keep":30}`)); got != (backup.Schedule{Enabled: true, Hour: 11, Keep: 30}) {
		t.Errorf("ParseSchedule = %+v", got)
	}
}

// TestScheduleClampsOutOfRangeStoredValues covers final-review M8. PUT /settings
// runs Validate and refuses hour: 99, but a value that reaches the settings row
// another way - a restore from an older install, a hand-written UPDATE - used to
// make RunOnce return false forever, so nightly backups silently never ran and
// nothing anywhere said why. A clamped hour dumps at the wrong time; an
// unclamped one never dumps at all, and only the first of those is noticeable.
func TestScheduleClampsOutOfRangeStoredValues(t *testing.T) {
	for raw, want := range map[string]backup.Schedule{
		`{"enabled":true,"hour":99,"keep":7}`:   {Enabled: true, Hour: 23, Keep: 7},
		`{"enabled":true,"hour":-5,"keep":7}`:   {Enabled: true, Hour: 0, Keep: 7},
		`{"enabled":true,"hour":3,"keep":0}`:    {Enabled: true, Hour: 3, Keep: 1},
		`{"enabled":true,"hour":3,"keep":-1}`:   {Enabled: true, Hour: 3, Keep: 1},
		`{"enabled":true,"hour":3,"keep":9999}`: {Enabled: true, Hour: 3, Keep: 60},
		`{"enabled":true,"hour":24,"keep":61}`:  {Enabled: true, Hour: 23, Keep: 60},
		// In range: untouched.
		`{"enabled":true,"hour":0,"keep":1}`:   {Enabled: true, Hour: 0, Keep: 1},
		`{"enabled":true,"hour":23,"keep":60}`: {Enabled: true, Hour: 23, Keep: 60},
	} {
		if got := backup.ParseSchedule([]byte(raw)); got != want {
			t.Errorf("ParseSchedule(%s) = %+v, want %+v", raw, got, want)
		}
	}
	// A clamped schedule is one the settings page can save back unchanged.
	if fields := backup.ParseSchedule([]byte(`{"enabled":true,"hour":99,"keep":9999}`)).Validate(); len(fields) != 0 {
		t.Errorf("a clamped schedule still fails Validate: %v", fields)
	}
}

func TestScheduleValidate(t *testing.T) {
	for _, s := range []backup.Schedule{{Hour: -1, Keep: 7}, {Hour: 24, Keep: 7}, {Hour: 3, Keep: 0}, {Hour: 3, Keep: 61}} {
		if fields := s.Validate(); len(fields) == 0 {
			t.Errorf("Validate(%+v) accepted an out-of-range value", s)
		}
	}
	for _, s := range []backup.Schedule{{Hour: 0, Keep: 1}, {Hour: 23, Keep: 60}, {Enabled: true, Hour: 3, Keep: 7}} {
		if fields := s.Validate(); len(fields) != 0 {
			t.Errorf("Validate(%+v) = %v, want no errors", s, fields)
		}
	}
}
