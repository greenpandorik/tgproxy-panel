package reliability

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func meReport(t *testing.T, now time.Time, source string, state string) Report {
	t.Helper()
	var r Report
	raw := fmt.Sprintf(`{"version":1,"at":%q,"me_pool":{"at":%q,%s,"state":%s}}`, now.Format(time.RFC3339), now.Format(time.RFC3339), source, state)
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func meFinding(t *testing.T, r Report, now time.Time, kind string) Finding {
	t.Helper()
	for _, f := range Findings(r, now) {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("missing %s in %+v", kind, Findings(r, now))
	return Finding{}
}

// These tests catch coverage zeros incorrectly resolving a stale/unobserved map,
// and normal warmup or a young replacement backlog being called a failure.
func TestMEFindingsRespectPendingAgeAndMapAuthority(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                              string
		age                               int
		current                           string
		deficit                           int
		wantPending, wantDeficit, wantMap bool
		coverageKnown                     bool
	}{
		{"normal warmup", 299, "true", 3, false, false, false, false},
		{"persistent current deficit", 300, "true", 3, false, true, false, true},
		{"approaching pending TTL", 1500, "true", 0, true, false, false, true},
		{"young stale map", 299, "false", 0, false, false, false, false},
		{"persistent stale map", 300, "false", 0, false, false, true, false},
		{"unknown map", 1500, "null", 0, true, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := fmt.Sprintf(`{"active_generation":1,"pending_generation":2,"hardswap_enabled":true,"pending":true,"pending_age_seconds":%d,"pending_map_current":%s,"pending_writers":2,"writer_deficit":%d,"missing_dc_groups":0,"orphan_warm_writers":0,"replacement_preparing":0,"replacement_retiring":0}`, tc.age, tc.current, tc.deficit)
			r := meReport(t, now, `"enabled":true`, state)
			pending := meFinding(t, r, now, "me_hardswap_pending")
			deficit := meFinding(t, r, now, "me_hardswap_deficit")
			stale := meFinding(t, r, now, "me_hardswap_stale_map")
			if pending.Failed != tc.wantPending || deficit.Failed != tc.wantDeficit || stale.Failed != tc.wantMap || deficit.Known != tc.coverageKnown {
				t.Fatalf("pending=%+v deficit=%+v stale=%+v", pending, deficit, stale)
			}
		})
	}
}

func TestMEFindingsRequireSustainedIndependentBacklogs(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, counts          string
		wantKnown, wantFailed bool
	}{
		{"unobserved", `"orphan_warm_writers":2,"replacement_preparing":1,"replacement_retiring":0`, false, false},
		{"young", `"orphan_warm_writers":2,"replacement_preparing":1,"replacement_retiring":0,"orphan_observed_seconds":299,"replacement_observed_seconds":299`, false, false},
		{"sustained", `"orphan_warm_writers":2,"replacement_preparing":0,"replacement_retiring":1,"orphan_observed_seconds":300,"replacement_observed_seconds":300`, true, true},
		{"clear", `"orphan_warm_writers":0,"replacement_preparing":0,"replacement_retiring":0`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := meReport(t, now, `"enabled":true`, fmt.Sprintf(`{"active_generation":1,"pending_generation":0,"hardswap_enabled":false,"pending":false,"pending_map_current":null,%s}`, tc.counts))
			for _, kind := range []string{"me_orphan_warm_writers", "me_replacement_busy"} {
				f := meFinding(t, r, now, kind)
				if f.Known != tc.wantKnown || f.Failed != tc.wantFailed {
					t.Fatalf("%+v", f)
				}
			}
		})
	}
}

func TestMEUnknownSourcesCannotResolve(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{`"enabled":false,"reason":"source_unavailable"`, `"enabled":false,"reason":"silent"`, `"enabled":true`} {
		r := meReport(t, now, source, "null")
		if got := Findings(r, now); len(got) != 0 {
			t.Fatalf("unavailable: %+v", got)
		}
	}
	state := `{"active_generation":1,"pending_generation":0,"hardswap_enabled":true,"pending":false,"pending_map_current":null,"writer_deficit":0,"missing_dc_groups":0,"orphan_warm_writers":0,"replacement_preparing":0,"replacement_retiring":0}`
	r := meReport(t, now, `"enabled":true`, state)
	raw, _ := json.Marshal(r)
	for _, at := range []time.Time{now.Add(-121 * time.Second), {}, now.Add(time.Second)} {
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		pool, ok := fields["me_pool"].(map[string]any)
		if !ok {
			t.Fatal("ME source missing from serialized report")
		}
		pool["at"] = at.Format(time.RFC3339)
		changed, _ := json.Marshal(fields)
		_ = json.Unmarshal(changed, &r)
		if got := Findings(r, now); len(got) != 0 {
			t.Fatalf("source time %v: %+v", at, got)
		}
	}
}

func TestMEConfirmedNoPendingCanResolveButMissingCountersCannot(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	r := meReport(t, now, `"enabled":true`, `{"active_generation":1,"pending_generation":0,"hardswap_enabled":true,"pending":false,"pending_map_current":null}`)
	for _, kind := range []string{"me_hardswap_pending", "me_hardswap_deficit", "me_hardswap_stale_map"} {
		f := meFinding(t, r, now, kind)
		if !f.Known || f.Failed {
			t.Fatalf("%+v", f)
		}
	}
	for _, kind := range []string{"me_orphan_warm_writers", "me_replacement_busy"} {
		f := meFinding(t, r, now, kind)
		if f.Known || f.Failed {
			t.Fatalf("%+v", f)
		}
	}
}

func TestMEMissingDCFamilyGroupsAreNotZeroCoverage(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		groups        string
		known, failed bool
	}{
		{"1", true, true},
		{"0", true, false},
		{"null", false, false},
	} {
		r := meReport(t, now, `"enabled":true`, fmt.Sprintf(`{"active_generation":1,"pending_generation":2,"hardswap_enabled":true,"pending":true,"pending_age_seconds":300,"pending_map_current":true,"writer_deficit":0,"missing_dc_groups":%s}`, tc.groups))
		f := meFinding(t, r, now, "me_hardswap_deficit")
		if f.Known != tc.known || f.Failed != tc.failed {
			t.Fatalf("groups=%s: %+v", tc.groups, f)
		}
	}
}
