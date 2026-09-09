// Package backup creates and restores logical dumps of the panel database with pg_dump/pg_restore.
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

// timeLayout is the UTC stamp in a dump file name.
const timeLayout = "20060102T150405Z"

const maxErrOutput = 800

func DirFor(dataDir string) string { return filepath.Join(dataDir, "backups") }

// Entry describes one dump: the file on disk plus, once the caller has written it, the row that indexes it.
type Entry struct {
	ID        uuid.UUID
	Path      string
	Name      string
	Size      int64
	Kind      string
	CreatedAt time.Time
}

// Runner shells out to the postgres client tools.
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

// Create dumps the whole database into Dir and returns the resulting file.
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
	out, err := r.run(ctx, "pg_dump", "--format=custom", "--no-owner", "--file", path, r.DatabaseURL)
	if err != nil {
		_ = os.Remove(path)
		return Entry{}, fmt.Errorf("pg_dump: %s", r.scrub(out, err))
	}
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("pg_dump produced no file: %w", err)
	}
	return Entry{Path: path, Name: name, Size: info.Size(), Kind: kind, CreatedAt: now}, nil
}

// freeName picks a file name that does not exist yet.
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

// Restore replaces the current database contents with a dump from Dir.
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

// Path resolves a dump name to its absolute path inside Dir, rejecting anything that points elsewhere.
func (r *Runner) Path(name string) (string, error) { return r.resolve(name) }

func (r *Runner) resolve(path string) (string, error) {
	if r.Dir == "" {
		return "", errors.New("backup directory is not configured")
	}
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
		return "", fmt.Errorf("invalid backup file name %q", path)
	}
	if dir := filepath.Dir(filepath.Clean(path)); dir != "." && !sameDir(dir, r.Dir) {
		return "", fmt.Errorf("backup file must be inside %s", r.Dir)
	}
	return filepath.Join(r.Dir, name), nil
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	ra, errA = filepath.Abs(ra)
	rb, errB = filepath.Abs(rb)
	return errA == nil && errB == nil && ra == rb
}

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

// Schedule is the backup_schedule setting: a nightly dump at a fixed UTC hour, keeping the newest Keep files.
type Schedule struct {
	Enabled bool `json:"enabled"`
	Hour    int  `json:"hour"`
	Keep    int  `json:"keep"`
}

// DefaultSchedule is what an installation that has never touched the setting gets: off, and.
func DefaultSchedule() Schedule { return Schedule{Enabled: false, Hour: 3, Keep: 7} }

const (
	minHour, maxHour = 0, 23
	minKeep, maxKeep = 1, 60
)

// ParseSchedule decodes a stored settings value, falling back field by field to the defaults.
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
