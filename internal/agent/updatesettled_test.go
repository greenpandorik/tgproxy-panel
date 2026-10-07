package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateSettled(t *testing.T) {
	h, _, ft := telemtHandler(t, &fakeExec{})
	h.cfg.TelemtBin = filepath.Join(t.TempDir(), "telemt")
	if err := os.WriteFile(h.cfg.TelemtBin, []byte("telemt 3.5.6"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("telemt 3.5.6"))
	saved := updateJournal{Active: true, BinarySHA: hex.EncodeToString(sum[:]), HadWeb: true, AdmissionOpen: true}
	admission := func(open bool) map[string]any {
		return map[string]any{"operator_lifecycle": map[string]any{"state": "running", "admission_open": open}}
	}
	ctx := context.Background()

	for _, outcome := range []string{UpdateOutcomeUpdated, UpdateOutcomeRolledBack, UpdateOutcomeAdmissionClosed} {
		if !h.updateSettled(ctx, saved, outcome) {
			t.Errorf("%s left the journal active", outcome)
		}
	}
	if h.updateSettled(ctx, saved, UpdateOutcomeRollbackFailed) {
		t.Error("rollback_failed closed the journal")
	}

	ft.webStatus = admission(true)
	if !h.updateSettled(ctx, saved, UpdateOutcomeFailed) {
		t.Error("a failure that left the binary and admission as they were kept the journal active")
	}
	ft.webStatus = admission(false)
	if h.updateSettled(ctx, saved, UpdateOutcomeFailed) {
		t.Error("a failure that left admission closed closed the journal")
	}
	ft.webStatus = admission(true)
	if err := os.WriteFile(h.cfg.TelemtBin, []byte("telemt 3.5.7"), 0o755); err != nil {
		t.Fatal(err)
	}
	if h.updateSettled(ctx, saved, UpdateOutcomeFailed) {
		t.Error("a failure that left a different binary on disk closed the journal")
	}
}
