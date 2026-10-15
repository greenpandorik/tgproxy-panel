package worker_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type presenceFleet struct {
	f          *fixture
	mock       *nodedriver.Mock
	a, b       uuid.UUID
	ivan, team uuid.UUID
}

func newPresenceFleet(t *testing.T) presenceFleet {
	t.Helper()
	f := newTelemtFixture(t)
	ctx := context.Background()
	other, err := f.st.Q.CreateNode(ctx, db.CreateNodeParams{
		Name: "b", Hostname: "b.test", AcmeEmail: "a@b.co", Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTelemt, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	both := []uuid.UUID{f.node.ID, other.ID}
	ivan, err := f.keys.Create(ctx, keys.CreateInput{Label: "ivan", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: both})
	if err != nil {
		t.Fatal(err)
	}
	team, err := f.keys.Create(ctx, keys.CreateInput{Label: "team", Type: domain.KeyShared, CarrierMode: "https", NodeIDs: both})
	if err != nil {
		t.Fatal(err)
	}
	mock := nodedriver.NewMock()
	for _, id := range both {
		mock.SetOnline(id, true)
		mock.SetMetrics(id, "telemt_connections_total 1\n")
		_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: id})
	}
	return presenceFleet{f: f, mock: mock, a: f.node.ID, b: other.ID, ivan: ivan.ID, team: team.ID}
}

func (p presenceFleet) userStats(total string, users map[uuid.UUID][]string, conns map[uuid.UUID]string) map[string]string {
	out := map[string]string{"connections_total": total}
	for id, ips := range users {
		name := domain.ProfileName(id)
		out["user."+name+".connections"] = conns[id]
		out["user."+name+".active_ips"] = strconv.Itoa(len(ips))
		out["user."+name+".ip_list"] = strings.Join(ips, ",")
	}
	return out
}

func keyPresence(t *testing.T, p presenceFleet) map[uuid.UUID]db.KeyPresenceForKeysRow {
	t.Helper()
	rows, err := p.f.st.Q.KeyPresenceForKeys(context.Background(), db.KeyPresenceForKeysParams{
		KeyIds: []uuid.UUID{p.ivan, p.team}, Since: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[uuid.UUID]db.KeyPresenceForKeysRow{}
	for _, r := range rows {
		out[r.AccessKeyID] = r
	}
	return out
}

func TestStatsCountsPeopleAcrossTheFleet(t *testing.T) {
	p := newPresenceFleet(t)
	ctx := context.Background()
	p.mock.SetStats(p.a, p.userStats("7",
		map[uuid.UUID][]string{p.ivan: {"198.51.100.7"}, p.team: {"203.0.113.1", "203.0.113.2"}},
		map[uuid.UUID]string{p.ivan: "3", p.team: "4"}))
	p.mock.SetStats(p.b, p.userStats("5",
		map[uuid.UUID][]string{p.ivan: {"198.51.100.8"}, p.team: {"203.0.113.2", "203.0.113.3"}},
		map[uuid.UUID]string{p.ivan: "2", p.team: "3"}))
	s := worker.NewStats(p.f.st, p.mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	fleet, err := p.f.st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if fleet.PeopleOnline != 4 || fleet.People15m != 4 || fleet.Connections != 12 {
		t.Fatalf("fleet = %+v, want Ivan once and three addresses behind the shared key, over 12 connections", fleet)
	}
	for _, id := range []uuid.UUID{p.a, p.b} {
		snap, err := p.f.st.Q.NodeLiveSnapshot(ctx, db.NodeLiveSnapshotParams{NodeID: id, Since: time.Now().Add(-time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if !snap.PeopleOnline.Valid || snap.PeopleOnline.Int32 != 3 {
			t.Fatalf("node %s people = %+v, want 3", id, snap.PeopleOnline)
		}
	}
	got := keyPresence(t, p)
	if k := got[p.ivan]; k.Connections != 5 || k.Devices != 2 || k.Devices15m != 2 {
		t.Fatalf("ivan = %+v", k)
	}
	if k := got[p.team]; k.Connections != 7 || k.Devices != 3 || k.Devices15m != 3 {
		t.Fatalf("team = %+v", k)
	}

	var leaked int
	if err := p.f.st.Pool.QueryRow(ctx,
		`SELECT count(*) FROM node_stats_snapshots WHERE mtproxy_raw::text LIKE '%203.0.113%' OR mtproxy_raw::text LIKE '%ip_list%'`,
	).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d snapshots kept the raw address lists", leaked)
	}

	// Ivan disconnects: offline now, still seen in the last 15 minutes.
	p.mock.SetStats(p.a, p.userStats("4",
		map[uuid.UUID][]string{p.ivan: {}, p.team: {"203.0.113.1", "203.0.113.2"}},
		map[uuid.UUID]string{p.ivan: "0", p.team: "4"}))
	p.mock.SetStats(p.b, p.userStats("0",
		map[uuid.UUID][]string{p.ivan: {}, p.team: {}},
		map[uuid.UUID]string{p.ivan: "0", p.team: "0"}))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	fleet, _ = p.f.st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Minute))
	if fleet.PeopleOnline != 2 || fleet.People15m != 4 || fleet.Connections != 4 {
		t.Fatalf("fleet = %+v, want two people now and four over the window", fleet)
	}
	got = keyPresence(t, p)
	if k := got[p.ivan]; k.Connections != 0 || k.Devices != 0 || k.Devices15m != 2 {
		t.Fatalf("ivan = %+v", k)
	}

	// A panel that starts over has no window: Ivan is cleared.
	fresh := worker.NewStats(p.f.st, p.mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := fresh.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if k := keyPresence(t, p)[p.ivan]; k.Connections != 0 || k.Devices != 0 || k.Devices15m != 0 {
		t.Fatalf("ivan = %+v, want every count back at zero", k)
	}
}

func TestStatsKeepsPeopleOnAServerItMissedOnce(t *testing.T) {
	p := newPresenceFleet(t)
	ctx := context.Background()
	p.mock.SetStats(p.a, p.userStats("7",
		map[uuid.UUID][]string{p.team: {"203.0.113.1", "203.0.113.2"}},
		map[uuid.UUID]string{p.team: "7"}))
	p.mock.SetStats(p.b, p.userStats("3",
		map[uuid.UUID][]string{p.ivan: {"198.51.100.7"}},
		map[uuid.UUID]string{p.ivan: "3"}))
	s := worker.NewStats(p.f.st, p.mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	p.mock.SetOnline(p.b, false)
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	fleet, _ := p.f.st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Minute))
	if fleet.PeopleOnline != 3 || fleet.Connections != 10 {
		t.Fatalf("fleet = %+v: a server missed for one sweep keeps its last reading", fleet)
	}
	if k := keyPresence(t, p)[p.ivan]; k.Connections != 3 || k.Devices != 1 {
		t.Fatalf("ivan = %+v, want him still online", k)
	}

	if _, err := p.f.st.Pool.Exec(ctx, `UPDATE nodes SET status = 'offline' WHERE id = $1`, p.b); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	fleet, _ = p.f.st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Minute))
	if fleet.PeopleOnline != 2 || fleet.Connections != 7 {
		t.Fatalf("fleet = %+v: a server marked offline no longer counts", fleet)
	}
	if k := keyPresence(t, p)[p.ivan]; k.Connections != 0 || k.Devices != 0 || k.Devices15m != 1 {
		t.Fatalf("ivan = %+v, want him offline but seen in the window", k)
	}
}

func TestStatsCountsPeopleFromOldAgents(t *testing.T) {
	p := newPresenceFleet(t)
	ctx := context.Background()
	team := domain.ProfileName(p.team)
	p.mock.SetStats(p.a, map[string]string{"user." + team + ".connections": "6", "user." + team + ".active_ips": "3"})
	p.mock.SetStats(p.b, map[string]string{})
	s := worker.NewStats(p.f.st, p.mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	fleet, err := p.f.st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if fleet.PeopleOnline != 3 {
		t.Fatalf("fleet = %+v: without lists the shared key counts its addresses", fleet)
	}
	if k := keyPresence(t, p)[p.team]; k.Devices != 3 {
		t.Fatalf("team = %+v", k)
	}
}

func TestStatsCountsTproxySessionsAsPeople(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 2\ntproxy_streams_live 5\n")
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snap, err := f.st.Q.NodeLiveSnapshot(ctx, db.NodeLiveSnapshotParams{NodeID: f.node.ID, Since: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.PeopleOnline.Valid || snap.PeopleOnline.Int32 != 2 {
		t.Fatalf("node people = %+v, want the two WEB sessions", snap.PeopleOnline)
	}
	fleet, _ := f.st.Q.LatestFleetSnapshot(ctx, time.Now().Add(-time.Minute))
	if fleet.PeopleOnline != 2 || fleet.Connections != 2 {
		t.Fatalf("fleet = %+v", fleet)
	}
}

type presenceSample struct {
	people, connections int
	nodes               map[uuid.UUID]struct {
		People      int `json:"people"`
		Connections int `json:"connections"`
	}
}

func presenceSamples(t *testing.T, p presenceFleet, key uuid.UUID) []presenceSample {
	t.Helper()
	rows, err := p.f.st.Pool.Query(context.Background(),
		`SELECT people, connections, nodes FROM key_presence_samples WHERE access_key_id = $1 ORDER BY taken_at, id`, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []presenceSample
	for rows.Next() {
		var s presenceSample
		var raw []byte
		if err := rows.Scan(&s.people, &s.connections, &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &s.nodes); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestStatsKeepsHistoryForSharedKeysOnly(t *testing.T) {
	p := newPresenceFleet(t)
	ctx := context.Background()
	idle, err := p.f.keys.Create(ctx, keys.CreateInput{Label: "idle", Type: domain.KeyShared, CarrierMode: "https", NodeIDs: []uuid.UUID{p.a}})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := p.f.keys.Create(ctx, keys.CreateInput{Label: "paused", Type: domain.KeyShared, CarrierMode: "https", NodeIDs: []uuid.UUID{p.a}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.f.st.Pool.Exec(ctx, `UPDATE access_keys SET disabled_at = now() WHERE id = $1`, paused.ID); err != nil {
		t.Fatal(err)
	}
	p.mock.SetStats(p.a, p.userStats("7",
		map[uuid.UUID][]string{p.ivan: {"198.51.100.7"}, p.team: {"203.0.113.1", "203.0.113.2"}},
		map[uuid.UUID]string{p.ivan: "3", p.team: "4"}))
	p.mock.SetStats(p.b, p.userStats("5",
		map[uuid.UUID][]string{p.team: {"203.0.113.2", "203.0.113.3", "203.0.113.4"}},
		map[uuid.UUID]string{p.team: "5"}))
	s := worker.NewStats(p.f.st, p.mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	team := presenceSamples(t, p, p.team)
	if len(team) != 1 || team[0].people != 4 || team[0].connections != 9 || len(team[0].nodes) != 2 {
		t.Fatalf("team samples = %+v", team)
	}
	if n := team[0].nodes[p.a]; n.People != 2 || n.Connections != 4 {
		t.Fatalf("team on a = %+v", n)
	}
	if n := team[0].nodes[p.b]; n.People != 3 || n.Connections != 5 {
		t.Fatalf("team on b = %+v", n)
	}
	if got := presenceSamples(t, p, p.ivan); len(got) != 0 {
		t.Fatalf("personal key has %d samples", len(got))
	}
	if got := presenceSamples(t, p, idle.ID); len(got) != 1 || got[0].people != 0 || len(got[0].nodes) != 0 {
		t.Fatalf("an idle shared key records a zero, got %+v", got)
	}
	if got := presenceSamples(t, p, paused.ID); len(got) != 0 {
		t.Fatalf("a turned off shared key has %d samples", len(got))
	}

	var leaked int
	if err := p.f.st.Pool.QueryRow(ctx, `SELECT count(*) FROM key_presence_samples WHERE nodes::text LIKE '%203.0.113%'`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d samples kept addresses", leaked)
	}

	p.mock.SetStats(p.a, p.userStats("0", map[uuid.UUID][]string{p.team: {}}, map[uuid.UUID]string{p.team: "0"}))
	p.mock.SetStats(p.b, p.userStats("0", map[uuid.UUID][]string{p.team: {}}, map[uuid.UUID]string{p.team: "0"}))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	team = presenceSamples(t, p, p.team)
	if len(team) != 2 || team[1].people != 0 || team[1].connections != 0 || len(team[1].nodes) != 0 {
		t.Fatalf("after everyone left: %+v", team)
	}
}
