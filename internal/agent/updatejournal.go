package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// An intent is fsynced before admission or binaries change. It survives SIGKILL and reboot.
type updateJournal struct {
	Outcome       string    `json:"outcome,omitempty"`
	Error         string    `json:"error,omitempty"`
	Target        string    `json:"target"`
	Active        bool      `json:"active"`
	Version       string    `json:"version"`
	Binary        string    `json:"binary"`
	BinarySHA     string    `json:"binary_sha256"`
	Config        string    `json:"config"`
	ConfigSHA     string    `json:"config_sha256"`
	HadWeb        bool      `json:"had_web"`
	AdmissionOpen bool      `json:"admission_open"`
	Started       time.Time `json:"started"`
}

func (h *Handler) updateJournalPath() string {
	return filepath.Join(h.cfg.StateDir, "telemt-update-journal.json")
}

func (h *Handler) writeUpdateJournal(j updateJournal) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	return writeAtomic(h.updateJournalPath(), b, 0o600)
}

func saveVerifiedCopy(src, dst string, mode os.FileMode) (string, error) {
	b, e := os.ReadFile(src)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	if e = writeAtomic(dst, b, mode); e != nil {
		return "", e
	}
	return hex.EncodeToString(sum[:]), nil
}

func verifiedFile(path, sha string) ([]byte, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != sha {
		return nil, errors.New("rollback artifact checksum mismatch")
	}
	return b, nil
}

func (h *Handler) prepareUpdateJournal(ctx context.Context, plan updatePlan, target string) (updateJournal, error) {
	if err := h.checkUpdateJournal(); err != nil {
		return updateJournal{}, err
	}
	j := updateJournal{Target: target, Active: true, Version: plan.installed, HadWeb: plan.hadWeb, Started: time.Now().UTC()}
	if e := os.MkdirAll(h.cfg.StateDir, 0o700); e != nil {
		return j, e
	}
	if plan.hadWeb {
		st, e := h.tm.WebStatus(ctx)
		if e != nil {
			return j, e
		}
		if st.OperatorLifecycle == nil {
			return j, errors.New("cannot record original WEB admission state")
		}
		j.AdmissionOpen = st.OperatorLifecycle.AdmissionOpen
	}
	j.Binary = filepath.Join(h.cfg.StateDir, "telemt-known-good")
	j.Config = filepath.Join(h.cfg.StateDir, "telemt-known-good.toml")
	var e error
	j.BinarySHA, e = saveVerifiedCopy(h.cfg.TelemtBin, j.Binary, 0o700)
	if e != nil {
		return j, e
	}
	j.ConfigSHA, e = saveVerifiedCopy(h.cfg.TelemtConfigPath, j.Config, 0o600)
	if e != nil {
		return j, e
	}
	return j, h.writeUpdateJournal(j)
}

func (h *Handler) recoverUpdate(ctx context.Context) error {
	if h.tm == nil {
		return nil
	}
	b, e := os.ReadFile(h.updateJournalPath())
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	var j updateJournal
	if e = json.Unmarshal(b, &j); e != nil {
		return e
	}
	if !j.Active {
		if j.Outcome != "" {
			u := newTelemtUpdate(j.Version, j.Target)
			var cause error
			if j.Error != "" {
				cause = errors.New(j.Error)
			}
			u.end(j.Outcome, cause)
			h.updMu.Lock()
			h.upd = u
			h.updMu.Unlock()
		}
		return nil
	}
	if !h.maintenance.TryLock() {
		return errors.New("maintenance busy during recovery")
	}
	defer h.maintenance.Unlock()
	ctx, cancel := context.WithTimeout(ctx, rollbackTimeout)
	defer cancel()
	// Paths are fixed locally. A corrupted journal must never select arbitrary destinations.
	if j.Binary != filepath.Join(h.cfg.StateDir, "telemt-known-good") || j.Config != filepath.Join(h.cfg.StateDir, "telemt-known-good.toml") {
		return errors.New("invalid rollback artifact paths")
	}
	binary, e := verifiedFile(j.Binary, j.BinarySHA)
	if e != nil {
		return e
	}
	config, e := verifiedFile(j.Config, j.ConfigSHA)
	if e != nil {
		return e
	}
	if e = writeAtomic(h.cfg.TelemtBin, binary, 0o755); e != nil {
		return e
	}
	if e = writeAtomicKeepOwner(h.cfg.TelemtConfigPath, config, 0o600); e != nil {
		return e
	}
	if e = h.restartTelemt(ctx); e != nil {
		return fmt.Errorf("restart previous telemt: %w", e)
	}
	if e = h.verifyTelemtVersion(ctx, j.Version); e != nil {
		return e
	}
	if j.HadWeb {
		if j.AdmissionOpen {
			e = h.tm.WebResume(ctx)
		} else {
			e = h.tm.WebPause(ctx)
		}
		if e != nil {
			return e
		}
		st, e := h.tm.WebStatus(ctx)
		if e != nil {
			return e
		}
		if st.OperatorLifecycle == nil || st.OperatorLifecycle.AdmissionOpen != j.AdmissionOpen {
			return errors.New("original WEB admission could not be restored")
		}
	}
	j.Active = false
	j.Outcome = UpdateOutcomeRolledBack
	j.Error = "interrupted update recovered from durable journal"
	if e = h.writeUpdateJournal(j); e != nil {
		return e
	}
	u := newTelemtUpdate(j.Version, j.Target)
	u.end(UpdateOutcomeRolledBack, errors.New("interrupted update recovered from durable journal"))
	h.updMu.Lock()
	h.upd = u
	h.updMu.Unlock()
	h.recoveryEvent("update_rollback", "recovered interrupted update")
	return nil
}

// Fail closed on unreadable intents: never overwrite the only rollback artifacts.
func (h *Handler) checkUpdateJournal() error {
	b, err := os.ReadFile(h.updateJournalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var j updateJournal
	if err = json.Unmarshal(b, &j); err != nil {
		return err
	}
	if j.Active {
		return errors.New("interrupted update requires recovery; inspect agent logs and restart the agent after resolving the recovery error")
	}
	return nil
}
