package worker

import (
	"testing"
	"time"

	"tgwebproxy/internal/reliability"
)

func TestMEUnknownSnapshotBreaksIncidentStreakWithoutClearingIncident(t *testing.T) {
	for _, source := range []string{"absent", "stale", "disabled", "incomplete"} {
		for _, phase := range []string{"recovery", "problem"} {
			t.Run(source+"/"+phase, func(t *testing.T) {
				f := newBlockingFixture(t)
				zero, one, elapsed := uint64(0), uint64(1), uint64(300)
				pool := &reliability.MEPool{At: f.now, Enabled: true, State: &reliability.MEPoolState{ActiveGeneration: 1, HardswapEnabled: true, OrphanWarmWriters: &zero, ReplacementPreparing: &zero, ReplacementRetiring: &zero}}
				if phase == "problem" {
					pool.State.OrphanWarmWriters = &one
					pool.State.OrphanObservedSeconds = &elapsed
				}
				known := reliability.Report{Version: 1, At: f.now, MEPool: pool}
				const kind = "reliability_me_orphan_warm_writers"
				if phase == "recovery" {
					if _, err := f.s.st.Pool.Exec(t.Context(), `INSERT INTO alerts(node_id,kind,message) VALUES($1,$2,'orphan')`, f.n.ID, kind); err != nil {
						t.Fatal(err)
					}
				}
				for range 2 {
					f.s.recordFindings(t.Context(), f.n, reliability.Findings(known, f.now))
				}
				unknown := known
				unavailable := *pool
				switch source {
				case "absent":
					unknown.MEPool = nil
				case "stale":
					unavailable.At = f.now.Add(-121 * time.Second)
					unknown.MEPool = &unavailable
				case "disabled":
					unavailable.Enabled = false
					unknown.MEPool = &unavailable
				case "incomplete":
					unavailable.State = nil
					unknown.MEPool = &unavailable
				}
				f.s.recordFindings(t.Context(), f.n, reliability.Findings(unknown, f.now))
				f.s.recordFindings(t.Context(), f.n, reliability.Findings(known, f.now))
				open := func() int {
					var count int
					if err := f.s.st.Pool.QueryRow(t.Context(), `SELECT count(*) FROM alerts WHERE node_id=$1 AND kind=$2 AND resolved_at IS NULL`, f.n.ID, kind).Scan(&count); err != nil {
						t.Fatal(err)
					}
					return count
				}
				want := 0
				if phase == "recovery" {
					want = 1
				}
				if count := open(); count != want {
					t.Fatalf("unknown ME source bridged %s streak: open=%d expected %d", phase, count, want)
				}
				for range 2 {
					f.s.recordFindings(t.Context(), f.n, reliability.Findings(known, f.now))
				}
				if count := open(); count != 1-want {
					t.Fatalf("three new known observations did not finish %s: open=%d", phase, count)
				}
			})
		}
	}
}

func TestNeverObservedMEUnknownDoesNotPreventHealthyNotificationSummary(t *testing.T) {
	f := newBlockingFixture(t)
	findings := reliability.Findings(reliability.Report{Version: 1, At: f.now}, f.now)
	findings = append(findings, reliability.Finding{Kind: "disk_pressure", Known: true})
	f.s.recordFindings(t.Context(), f.n, findings)
	if !f.a.observeReliability(t.Context(), f.n, true, f.now) {
		t.Fatal("notification candidate missing")
	}
	var phase string
	if err := f.s.st.Pool.QueryRow(t.Context(), `SELECT phase FROM notification_candidates WHERE node_id=$1`, f.n.ID).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "healthy" {
		t.Fatalf("never observed ME made notification summary %s", phase)
	}
	var rows int
	if err := f.s.st.Pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_findings WHERE node_id=$1 AND kind LIKE 'me_%'`, f.n.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("unavailable ME created %d unknown rows", rows)
	}
}

func TestDisabledHardswapBreaksPriorRecoveryStreaks(t *testing.T) {
	for _, kind := range []string{"reliability_me_hardswap_pending", "reliability_me_hardswap_deficit", "reliability_me_hardswap_stale_map"} {
		t.Run(kind, func(t *testing.T) {
			f := newBlockingFixture(t)
			pool := &reliability.MEPool{At: f.now, Enabled: true, State: &reliability.MEPoolState{ActiveGeneration: 1, HardswapEnabled: true}}
			report := reliability.Report{Version: 1, At: f.now, MEPool: pool}
			if _, err := f.s.st.Pool.Exec(t.Context(), `INSERT INTO alerts(node_id,kind,message) VALUES($1,$2,'hardswap')`, f.n.ID, kind); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				f.s.recordFindings(t.Context(), f.n, reliability.Findings(report, f.now))
			}
			pool.State.HardswapEnabled = false
			f.s.recordFindings(t.Context(), f.n, reliability.Findings(report, f.now))
			pool.State.HardswapEnabled = true
			f.s.recordFindings(t.Context(), f.n, reliability.Findings(report, f.now))
			var count int
			if err := f.s.st.Pool.QueryRow(t.Context(), `SELECT count(*) FROM alerts WHERE node_id=$1 AND kind=$2 AND resolved_at IS NULL`, f.n.ID, kind).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("disabled hardswap bridged recovery streak")
			}
		})
	}
}
