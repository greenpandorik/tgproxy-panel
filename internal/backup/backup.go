// Package backup creates and restores logical dumps of the panel database with
// pg_dump/pg_restore. It owns the dump directory and the naming scheme; the
// rows describing the dumps live in the backups table and are written by the
// callers (the API, the worker and the CLI).
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kinds mirror the backup_kind enum in the database.
const (
	KindManual    = "manual"
	KindScheduled = "scheduled"
)

// timeLayout is the UTC stamp in a dump file name. Sortable, no separators that
// need quoting in a shell.
const timeLayout = "20060102T150405Z"

// maxErrOutput caps how much of pg_dump's output ends up in an error string:
// enough for the message, not enough to paste a whole failed restore log into
// an API response.
const maxErrOutput = 800

// DirFor is where dumps live: a subdirectory of DATA_DIR, so the panel's own
// volume carries them and a deployment that already backs that volume up gets
// the dumps for free. Every caller resolves names against it.
func DirFor(dataDir string) string { return filepath.Join(dataDir, "backups") }

// Entry describes one dump: the file on disk plus, once the caller has written
// it, the row that indexes it.
type Entry struct {
	ID        uuid.UUID
	Path      string
	Name      string
	Size      int64
	Kind      string
	CreatedAt time.Time
}

// Runner shells out to the postgres client tools. Exec is injected so tests can
// run the whole flow without a database or the binaries installed; nil means
// os/exec with the process's PATH.
type Runner struct {
	DatabaseURL string
	Dir         string
	Exec        func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (r *Runner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.Exec != nil {
		return r.Exec(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // name is a constant, args are built here
}

// Create dumps the whole database into Dir and returns the resulting file. kind
// must be one of the backup_kind enum values; it becomes part of the file name
// so an operator can tell a nightly dump from one taken by hand.
//
// The dump is a full logical copy, so it includes the encrypted columns exactly
// as they are stored - restoring it without the matching MASTER_KEY leaves every
// secret undecryptable. That is deliberate: the master key is not in the
// database, so a stolen dump is not a stolen fleet.
func (r *Runner) Create(ctx context.Context, kind string) (Entry, error) {
	if kind != KindManual && kind != KindScheduled {
		return Entry{}, fmt.Errorf("unknown backup kind %q", kind)
	}
	if r.Dir == "" {
		return Entry{}, errors.New("backup directory is not configured")
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	name, path, err := r.freeName(now, kind)
	if err != nil {
		return Entry{}, err
	}
	// --format=custom so pg_restore can run --clean --if-exists; --no-owner so a
	// restore into a database owned by a different role does not fail on every
	// ALTER ... OWNER TO.
	out, err := r.run(ctx, "pg_dump", "--format=custom", "--no-owner", "--file", path, r.DatabaseURL)
	if err != nil {
		// A failed dump can still have created a truncated file; leaving it would
		// offer a restore that silently loses data.
		_ = os.Remove(path)
		return Entry{}, fmt.Errorf("pg_dump: %s", r.scrub(out, err))
	}
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("pg_dump produced no file: %w", err)
	}
	return Entry{Path: path, Name: name, Size: info.Size(), Kind: kind, CreatedAt: now}, nil
}

// freeName picks a file name that does not exist yet. The name is stamped to the
// second, so two dumps of the same kind started inside one second would resolve
// to the same file and pg_dump would overwrite the first while its row still
// pointed at it. The plain name is what every normal dump gets; the -2, -3
// variants only appear in that one-second race.
func (r *Runner) freeName(now time.Time, kind string) (string, string, error) {
	stamp := now.Format(timeLayout)
	for i := 1; i <= 9; i++ {
		name := fmt.Sprintf("tgwp-%s-%s.dump", stamp, kind)
		if i > 1 {
			name = fmt.Sprintf("tgwp-%s-%s-%d.dump", stamp, kind, i)
		}
		path := filepath.Join(r.Dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return name, path, nil
		}
	}
	return "", "", fmt.Errorf("too many backups already taken at %s", stamp)
}

// Restore replaces the current database contents with a dump from Dir. path may
// be a bare file name or a full path, but it must resolve inside Dir: the only
// dumps the panel offers are the ones it took itself, and accepting an arbitrary
// path would let a caller point pg_restore at any file on the host.
//
// It is destructive (--clean --if-exists drops the existing objects first) and
// has no way to tell a running panel to stand back, which is why the only caller
// is the CLI and the CLI refuses while the panel holds its lock file.
func (r *Runner) Restore(ctx context.Context, path string) error {
	full, err := r.resolve(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf("backup file: %w", err)
	}
	out, err := r.run(ctx, "pg_restore", "--clean", "--if-exists", "--no-owner", "--dbname", r.DatabaseURL, full)
	if err != nil {
		return fmt.Errorf("pg_restore: %s", r.scrub(out, err))
	}
	return nil
}

// Path resolves a dump name to its absolute path inside Dir, rejecting anything
// that points elsewhere. Handlers use it before opening a file named by a row.
func (r *Runner) Path(name string) (string, error) { return r.resolve(name) }

func (r *Runner) resolve(path string) (string, error) {
	if r.Dir == "" {
		return "", errors.New("backup directory is not configured")
	}
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
		return "", fmt.Errorf("invalid backup file name %q", path)
	}
	// filepath.Base alone would happily turn ../../etc/passwd into passwd and
	// "restore" a file the caller never named. Compare the directory the caller
	// gave (when it gave one) against Dir instead of silently rewriting it.
	if dir := filepath.Dir(filepath.Clean(path)); dir != "." && !sameDir(dir, r.Dir) {
		return "", fmt.Errorf("backup file must be inside %s", r.Dir)
	}
	return filepath.Join(r.Dir, name), nil
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	// Dir may be relative (./data/backups) while the caller passed the absolute
	// path it read out of a listing, or either side may be a symlink (/tmp on
	// macOS is /private/tmp).
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	ra, errA = filepath.Abs(ra)
	rb, errB = filepath.Abs(rb)
	return errA == nil && errB == nil && ra == rb
}

// scrub turns a failed command into a one-line message: the tool's own output
// where there is one, the raw error otherwise, with the database URL (which
// carries the password and is passed on the command line) removed from both.
func (r *Runner) scrub(out []byte, err error) string {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	if r.DatabaseURL != "" {
		msg = strings.ReplaceAll(msg, r.DatabaseURL, "<database-url>")
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > maxErrOutput {
		msg = msg[:maxErrOutput] + "…"
	}
	return msg
}

// Prune deletes the dumps beyond the newest keep, oldest first, removing the
// file and then calling del for the row. A missing file is not an error - the
// row still has to go - but a failing del stops the sweep, because a row whose
// file is already gone would otherwise offer a broken download forever.
//
// Returns how many entries were removed.
func (r *Runner) Prune(ctx context.Context, keep int, entries []Entry, del func(context.Context, Entry) error) (int, error) {
	if keep <= 0 || len(entries) <= keep {
		return 0, nil
	}
	ordered := make([]Entry, len(entries))
	copy(ordered, entries)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CreatedAt.After(ordered[j].CreatedAt) })

	removed := 0
	for _, e := range ordered[keep:] {
		if e.Path != "" {
			if err := os.Remove(e.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return removed, err
			}
		}
		if err := del(ctx, e); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// Schedule is the backup_schedule setting: a nightly dump at a fixed UTC hour,
// keeping the newest Keep files.
type Schedule struct {
	Enabled bool `json:"enabled"`
	Hour    int  `json:"hour"`
	Keep    int  `json:"keep"`
}

// DefaultSchedule is what an installation that has never touched the setting
// gets: off, and - once switched on - 03:00 UTC keeping a week of dumps.
func DefaultSchedule() Schedule { return Schedule{Enabled: false, Hour: 3, Keep: 7} }

// The accepted bounds, shared by ParseSchedule's clamp and Validate's message so
// the two can never drift apart.
const (
	minHour, maxHour = 0, 23
	minKeep, maxKeep = 1, 60
)

// ParseSchedule decodes a stored settings value, falling back field by field to
// the defaults. Anything that is not an object (including the plain string this
// setting held before it grew fields) parses as the defaults rather than an
// error, so an old value cannot break the worker or the settings page.
//
// Out-of-range numbers are clamped rather than rejected. PUT /settings runs
// Validate and refuses them, so nothing the API accepts needs this - but a value
// that arrives another way (a restore from an older install, a hand-written
// UPDATE) with hour: 99 would otherwise make RunOnce return false forever and
// nightly backups would silently never happen, with nothing anywhere to say why.
// A clamped hour runs the backup at the wrong time; an unclamped one runs it
// never, and only one of those is noticeable.
func ParseSchedule(raw []byte) Schedule {
	s := DefaultSchedule()
	if len(raw) == 0 {
		return s
	}
	var in struct {
		Enabled *bool `json:"enabled"`
		Hour    *int  `json:"hour"`
		Keep    *int  `json:"keep"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return DefaultSchedule()
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if in.Hour != nil {
		s.Hour = *in.Hour
	}
	if in.Keep != nil {
		s.Keep = *in.Keep
	}
	s.Hour = clamp(s.Hour, minHour, maxHour)
	s.Keep = clamp(s.Keep, minKeep, maxKeep)
	return s
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// Validate reports out-of-range fields in the shape the API's 422 body uses.
func (s Schedule) Validate() map[string]string {
	fields := map[string]string{}
	if s.Hour < minHour || s.Hour > maxHour {
		fields["backup_schedule.hour"] = fmt.Sprintf("must be %d..%d", minHour, maxHour)
	}
	if s.Keep < minKeep || s.Keep > maxKeep {
		fields["backup_schedule.keep"] = fmt.Sprintf("must be %d..%d", minKeep, maxKeep)
	}
	return fields
}
