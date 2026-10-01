package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func presenceRows(t *testing.T, st *store.Store, ids ...uuid.UUID) map[uuid.UUID]db.KeyPresenceForKeysRow {
	t.Helper()
	rows, err := st.Q.KeyPresenceForKeys(context.Background(), db.KeyPresenceForKeysParams{KeyIds: ids, Since: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	out := map[uuid.UUID]db.KeyPresenceForKeysRow{}
	for _, r := range rows {
		out[r.AccessKeyID] = r
	}
	return out
}

func TestKeyPresenceUpsertClearAndCascade(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	mk := func(label string) uuid.UUID {
		k, err := st.Q.CreateKey(ctx, db.CreateKeyParams{Label: label, Type: db.KeyTypePERSONAL, SecretEnc: []byte("x"), CarrierMode: "https", Limits: []byte("{}")})
		if err != nil {
			t.Fatal(err)
		}
		return k.ID
	}
	a, b, gone := mk("a"), mk("b"), uuid.New()

	if err := st.Q.UpsertKeyPresence(ctx, db.UpsertKeyPresenceParams{
		KeyIds: []uuid.UUID{a, b, gone}, Connections: []int32{3, 1, 9}, Devices: []int32{2, 1, 9}, Devices15m: []int32{2, 1, 9},
	}); err != nil {
		t.Fatalf("a key deleted during the sweep must not fail the rest: %v", err)
	}
	rows := presenceRows(t, st, a, b, gone)
	if len(rows) != 2 || rows[a].Connections != 3 || rows[a].Devices != 2 || rows[b].Devices15m != 1 {
		t.Fatalf("rows = %+v", rows)
	}

	if err := st.Q.UpsertKeyPresence(ctx, db.UpsertKeyPresenceParams{
		KeyIds: []uuid.UUID{a}, Connections: []int32{4}, Devices: []int32{3}, Devices15m: []int32{3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Q.ClearKeyPresence(ctx, []uuid.UUID{a}); err != nil {
		t.Fatal(err)
	}
	rows = presenceRows(t, st, a, b)
	if rows[a].Connections != 4 || rows[a].Devices != 3 {
		t.Fatalf("a = %+v, want the update", rows[a])
	}
	if r := rows[b]; r.Connections != 0 || r.Devices != 0 || r.Devices15m != 0 {
		t.Fatalf("b = %+v, want it cleared", r)
	}

	if err := st.Q.ClearKeyPresence(ctx, []uuid.UUID{}); err != nil {
		t.Fatal(err)
	}
	if r := presenceRows(t, st, a)[a]; r.Connections != 0 {
		t.Fatalf("a = %+v: an empty keep list clears everyone", r)
	}
	if err := st.Q.UpsertKeyPresence(ctx, db.UpsertKeyPresenceParams{
		KeyIds: []uuid.UUID{a}, Connections: []int32{2}, Devices: []int32{1}, Devices15m: []int32{1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Q.ClearKeyPresence(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if r := presenceRows(t, st, a)[a]; r.Connections != 0 {
		t.Fatalf("a = %+v: a nil keep list clears everyone too", r)
	}

	if err := st.Q.DeleteKey(ctx, a); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM key_presence`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d presence rows, want the deleted key's row gone with it", n)
	}
}

func TestFreshSnapshotQueries(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	node, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Q.InsertSnapshot(ctx, db.InsertSnapshotParams{NodeID: node.ID, SessionsLive: 5, MtproxyRaw: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE node_stats_snapshots SET taken_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-3 * time.Minute)
	if snaps, err := st.Q.LatestSnapshots(ctx, since); err != nil || len(snaps) != 0 {
		t.Fatalf("latest = %+v err=%v: an hour-old snapshot is not current", snaps, err)
	}
	if _, err := st.Q.NodeLiveSnapshot(ctx, db.NodeLiveSnapshotParams{NodeID: node.ID, Since: since}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("latest node snapshot err = %v, want no rows", err)
	}
	if snaps, err := st.Q.LatestSnapshots(ctx, time.Now().Add(-2*time.Hour)); err != nil || len(snaps) != 1 || snaps[0].PeopleOnline.Valid {
		t.Fatalf("latest = %+v err=%v: people_online stays NULL when nobody counted it", snaps, err)
	}

	if err := st.Q.InsertSnapshot(ctx, db.InsertSnapshotParams{
		NodeID: node.ID, SessionsLive: 7, MtproxyRaw: []byte("{}"), PeopleOnline: pgtype.Int4{Int32: 2, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	live, err := st.Q.LiveSnapshots(ctx, since)
	if err != nil || len(live) != 1 || live[0].SessionsLive != 7 || live[0].PeopleOnline.Int32 != 2 {
		t.Fatalf("live = %+v err=%v", live, err)
	}
	if one, err := st.Q.NodeLiveSnapshot(ctx, db.NodeLiveSnapshotParams{NodeID: node.ID, Since: since}); err != nil || one.SessionsLive != 7 {
		t.Fatalf("node live = %+v err=%v", one, err)
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE nodes SET status = 'offline' WHERE id = $1`, node.ID); err != nil {
		t.Fatal(err)
	}
	if live, err := st.Q.LiveSnapshots(ctx, since); err != nil || len(live) != 0 {
		t.Fatalf("live = %+v err=%v: a server marked offline has no live figures", live, err)
	}
	if _, err := st.Q.NodeLiveSnapshot(ctx, db.NodeLiveSnapshotParams{NodeID: node.ID, Since: since}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("node live err = %v, want no rows for an offline server", err)
	}

	if _, err := st.Q.LatestFleetSnapshot(ctx, since); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("fleet err = %v, want no rows", err)
	}
	if err := st.Q.InsertFleetSnapshot(ctx, db.InsertFleetSnapshotParams{PeopleOnline: 2, People15m: 3, Connections: 9}); err != nil {
		t.Fatal(err)
	}
	fleet, err := st.Q.LatestFleetSnapshot(ctx, since)
	if err != nil || fleet.PeopleOnline != 2 || fleet.People15m != 3 || fleet.Connections != 9 {
		t.Fatalf("fleet = %+v err=%v", fleet, err)
	}
	if err := st.Q.DeleteOldFleetSnapshots(ctx, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Hour)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("fleet err = %v, want the retention sweep to have removed it", err)
	}
}
