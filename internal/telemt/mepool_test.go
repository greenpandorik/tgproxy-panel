package telemt

import (
	"context"
	"net/http"
	"testing"
)

func TestMEPoolStateDoesNotManufactureZeroCoverageOrMapCurrency(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		wantMap      *bool
	}{
		{name: "no pending nulls", fields: `"pending_map_current":null`, wantMap: nil},
		{name: "older missing counters", fields: `"pending_map_current":true`, wantMap: boolPointer(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				ok(w, `{"enabled":true,"generated_at_epoch_secs":1791710000,"data":{"generations":{"active_generation":7,"pending_hardswap_generation":0,"pending_hardswap_age_secs":null},"hardswap":{"enabled":true,"pending":false,`+tc.fields+`}}}`)
			})
			pool, err := c.MEPoolState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if pool.State == nil {
				t.Fatalf("missing state: %+v", pool)
			}
			s := pool.State
			if s.WriterDeficit != nil || s.MissingDCGroups != nil || s.PendingWriters != nil || s.OrphanWarmWriters != nil || s.ReplacementPreparing != nil || s.ReplacementRetiring != nil || s.PendingAgeSeconds != nil {
				t.Fatalf("missing counters became zeros: %+v", s)
			}
			if (s.PendingMapCurrent == nil) != (tc.wantMap == nil) || (tc.wantMap != nil && *s.PendingMapCurrent != *tc.wantMap) {
				t.Fatalf("map currency: %+v", s.PendingMapCurrent)
			}
		})
	}
}

func boolPointer(v bool) *bool { return &v }

func TestMEPoolStateRejectsIncompleteAndIncoherentSnapshots(t *testing.T) {
	for _, data := range []string{
		`null`,
		`{}`,
		`{"generations":{"active_generation":1,"pending_hardswap_generation":0},"hardswap":{"enabled":true}}`,
		`{"generations":{"active_generation":1,"pending_hardswap_generation":0},"hardswap":{"enabled":true,"pending":true}}`,
	} {
		c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			ok(w, `{"enabled":true,"generated_at_epoch_secs":1791710000,"data":`+data+`}`)
		})
		pool, err := c.MEPoolState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if pool.State != nil || pool.Reason != "incomplete_snapshot" {
			t.Fatalf("incomplete snapshot authoritative: %+v", pool)
		}
	}
}

func TestMEPoolStateAbsentSourceTimeIsUnknown(t *testing.T) {
	for _, epoch := range []string{"0", "18446744073709551615"} {
		c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			ok(w, `{"enabled":false,"reason":"source_unavailable","generated_at_epoch_secs":`+epoch+`}`)
		})
		pool, err := c.MEPoolState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !pool.At.IsZero() || pool.State != nil || pool.Enabled {
			t.Fatalf("unavailable timestamp: %+v", pool)
		}
	}
}
