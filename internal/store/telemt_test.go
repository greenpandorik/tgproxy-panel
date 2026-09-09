package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func ptrStr(s string) *string { return &s }

func TestNodeEngineDefaultsAndOverrides(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()

	def, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "d", Hostname: "d.test"})
	if err != nil {
		t.Fatal(err)
	}
	if def.Engine != db.NodeEngineTelemt || def.ClassicPort != 8443 || def.TlsDomain != "" || def.TelemtVersion != "" {
		t.Fatalf("defaults: %+v", def)
	}

	old, err := st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "o", Hostname: "o.test",
		Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTproxy, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if old.Engine != db.NodeEngineTproxy {
		t.Fatalf("engine %q", old.Engine)
	}

	tl, err := st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "t", Hostname: "t.test",
		Engine:      db.NullNodeEngine{NodeEngine: db.NodeEngineTelemt, Valid: true},
		TlsDomain:   ptrStr("t.test"),
		ClassicPort: pgtype.Int4{Int32: 9443, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tl.TlsDomain != "t.test" || tl.ClassicPort != 9443 {
		t.Fatalf("telemt node %+v", tl)
	}

	// UpdateNode leaves the Fake-TLS settings alone when they are not supplied.
	kept, err := st.Q.UpdateNode(ctx, db.UpdateNodeParams{ID: tl.ID, Name: "t2", MaxProfiles: 16})
	if err != nil {
		t.Fatal(err)
	}
	if kept.TlsDomain != "t.test" || kept.ClassicPort != 9443 {
		t.Fatalf("update wiped fake-tls settings: %+v", kept)
	}
	changed, err := st.Q.UpdateNode(ctx, db.UpdateNodeParams{
		ID: tl.ID, Name: "t2", MaxProfiles: 16,
		TlsDomain: ptrStr("other.test"), ClassicPort: pgtype.Int4{Int32: 8443, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.TlsDomain != "other.test" || changed.ClassicPort != 8443 {
		t.Fatalf("update ignored fake-tls settings: %+v", changed)
	}

	if err := st.Q.SetNodePublicIP(ctx, db.SetNodePublicIPParams{ID: def.ID, PublicIp: "203.0.113.7"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Q.SetNodeTelemtVersion(ctx, db.SetNodeTelemtVersionParams{ID: def.ID, TelemtVersion: "3.5.5"}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Q.GetNode(ctx, def.ID)
	if got.PublicIp != "203.0.113.7" || got.TelemtVersion != "3.5.5" {
		t.Fatalf("node %+v", got)
	}
}

func TestAccessKeyTelemtLimitsAndKeyStatsSnapshots(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	node, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := st.Q.CreateKey(ctx, db.CreateKeyParams{
		Label: "k", Type: db.KeyTypeSHARED, SecretEnc: []byte("x"), CarrierMode: "https", Limits: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(key.TelemtLimits) != "{}" {
		t.Fatalf("default telemt_limits %s", key.TelemtLimits)
	}
	updated, err := st.Q.UpdateKey(ctx, db.UpdateKeyParams{
		ID: key.ID, Label: "k", CarrierMode: "https", Limits: []byte("{}"),
		TelemtLimits: []byte(`{"data_quota_bytes":1024}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(updated.TelemtLimits) != `{"data_quota_bytes": 1024}` && string(updated.TelemtLimits) != `{"data_quota_bytes":1024}` {
		t.Fatalf("telemt_limits %s", updated.TelemtLimits)
	}
	// A nil argument keeps whatever the key already had.
	kept, err := st.Q.UpdateKey(ctx, db.UpdateKeyParams{ID: key.ID, Label: "k", CarrierMode: "https", Limits: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if string(kept.TelemtLimits) == "{}" {
		t.Fatal("nil telemt_limits must not clear the stored value")
	}

	if err := st.Q.InsertKeyStatsSnapshot(ctx, db.InsertKeyStatsSnapshotParams{
		AccessKeyID: key.ID, NodeID: node.ID, Connections: 3, TotalOctets: 4096, QuotaUsedBytes: 4096, ActiveIps: 2,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Q.ListKeyStatsSnapshots(ctx, db.ListKeyStatsSnapshotsParams{
		AccessKeyID: key.ID, TakenAt: time.Now().Add(-time.Hour), TakenAt_2: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Connections != 3 || rows[0].TotalOctets != 4096 || rows[0].ActiveIps != 2 {
		t.Fatalf("snapshots %+v", rows)
	}
	latest, err := st.Q.LatestKeyStatsSnapshots(ctx, key.ID)
	if err != nil || len(latest) != 1 {
		t.Fatalf("latest %+v err=%v", latest, err)
	}
	// The sweep deletes in bounded batches and reports the row count so the caller can loop.
	n, err := st.Q.DeleteOldKeyStatsSnapshots(ctx, db.DeleteOldKeyStatsSnapshotsParams{
		Before: time.Now().Add(time.Hour), Batch: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("retention sweep deleted %d rows, want 1", n)
	}
	rows, _ = st.Q.ListKeyStatsSnapshots(ctx, db.ListKeyStatsSnapshotsParams{
		AccessKeyID: key.ID, TakenAt: time.Now().Add(-time.Hour), TakenAt_2: time.Now().Add(time.Hour),
	})
	if len(rows) != 0 {
		t.Fatalf("retention sweep left %d rows", len(rows))
	}
}
