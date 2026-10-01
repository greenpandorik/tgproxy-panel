package api_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/store/db"
)

func snapshotAt(t *testing.T, h *apitest.Harness, nodeID uuid.UUID, at time.Time, sessions int32, people pgtype.Int4) {
	t.Helper()
	ctx := context.Background()
	if err := h.Store.Q.InsertSnapshot(ctx, db.InsertSnapshotParams{
		NodeID: nodeID, SessionsLive: sessions, StreamsLive: sessions, MtproxyRaw: []byte("{}"), PeopleOnline: people,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Store.Pool.Exec(ctx,
		`UPDATE node_stats_snapshots SET taken_at = $1 WHERE id = (SELECT max(id) FROM node_stats_snapshots)`, at); err != nil {
		t.Fatal(err)
	}
}

func fleetAt(t *testing.T, h *apitest.Harness, at time.Time, people, people15m, conns int32) {
	t.Helper()
	ctx := context.Background()
	if err := h.Store.Q.InsertFleetSnapshot(ctx, db.InsertFleetSnapshotParams{PeopleOnline: people, People15m: people15m, Connections: conns}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Store.Pool.Exec(ctx,
		`UPDATE fleet_stats_snapshots SET taken_at = $1 WHERE id = (SELECT max(id) FROM fleet_stats_snapshots)`, at); err != nil {
		t.Fatal(err)
	}
}

func people(n int32) pgtype.Int4 { return pgtype.Int4{Int32: n, Valid: true} }

type summaryResp struct {
	SessionsLive    int    `json:"sessions_live"`
	PeopleOnline    *int32 `json:"people_online"`
	PeopleOnline15m *int32 `json:"people_online_15m"`
}

func metricsText(t *testing.T, h *apitest.Harness) string {
	t.Helper()
	resp := h.Anonymous().Get("/metrics")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("metrics %d", resp.StatusCode)
	}
	return string(body)
}

func TestDashboardAndMetricsCountOnlyFreshSnapshots(t *testing.T) {
	h, c, n := ownerWithNode(t)
	snapshotAt(t, h, n.ID, time.Now().Add(-time.Hour), 30, people(9))

	var sum summaryResp
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.SessionsLive != 0 {
		t.Fatalf("sessions_live = %d: a node silent for an hour must not count", sum.SessionsLive)
	}
	if body := metricsText(t, h); strings.Contains(body, n.ID.String()) {
		t.Fatalf("metrics still report the silent node:\n%s", body)
	}

	snapshotAt(t, h, n.ID, time.Now(), 4, people(2))
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.SessionsLive != 4 {
		t.Fatalf("sessions_live = %d, want the fresh 4", sum.SessionsLive)
	}
	body := metricsText(t, h)
	for _, want := range []string{
		`tgwp_node_sessions_live{node="` + n.ID.String() + `"} 4`,
		`tgwp_node_people_online{node="` + n.ID.String() + `"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics lack %s:\n%s", want, body)
		}
	}
}

func TestDashboardAndMetricsReportPeopleOnline(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var sum summaryResp
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.PeopleOnline != nil || sum.PeopleOnline15m != nil {
		t.Fatalf("summary = %+v: nothing counted yet must read as unknown, not zero", sum)
	}
	if body := metricsText(t, h); strings.Contains(body, "tgwp_people_online") {
		t.Fatalf("metrics invent a head count:\n%s", body)
	}

	fleetAt(t, h, time.Now(), 5, 8, 31)
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.PeopleOnline == nil || *sum.PeopleOnline != 5 || sum.PeopleOnline15m == nil || *sum.PeopleOnline15m != 8 {
		t.Fatalf("summary = %+v", sum)
	}
	body := metricsText(t, h)
	if !strings.Contains(body, "tgwp_people_online 5") || !strings.Contains(body, "tgwp_people_online_15m 8") {
		t.Fatalf("metrics:\n%s", body)
	}

	if _, err := h.Store.Pool.Exec(context.Background(), `UPDATE fleet_stats_snapshots SET taken_at = now() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	sum = summaryResp{}
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.PeopleOnline != nil {
		t.Fatalf("summary = %+v: a stale head count is not the present", sum)
	}
}

func TestNodesCarryPeopleOnline(t *testing.T) {
	h, c, n := ownerWithNode(t)
	quiet, _ := createNode(t, c, "n2.test")
	snapshotAt(t, h, n.ID, time.Now(), 11, people(3))
	snapshotAt(t, h, quiet.ID, time.Now().Add(-time.Hour), 7, people(2))

	type live struct {
		ID           uuid.UUID `json:"id"`
		PeopleOnline *int32    `json:"people_online"`
		Connections  *int32    `json:"connections"`
	}
	var list struct {
		Items []live `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	byID := map[uuid.UUID]live{}
	for _, it := range list.Items {
		byID[it.ID] = it
	}
	if got := byID[n.ID]; got.PeopleOnline == nil || *got.PeopleOnline != 3 || got.Connections == nil || *got.Connections != 11 {
		t.Fatalf("list n1 = %+v", got)
	}
	if got := byID[quiet.ID]; got.PeopleOnline != nil || got.Connections != nil {
		t.Fatalf("list n2 = %+v: an hour-old snapshot says nothing about now", got)
	}

	var one live
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &one)
	if one.PeopleOnline == nil || *one.PeopleOnline != 3 || one.Connections == nil || *one.Connections != 11 {
		t.Fatalf("get n1 = %+v", one)
	}

	snapshotAt(t, h, quiet.ID, time.Now(), 6, pgtype.Int4{})
	c.JSON(c.Get("/api/v1/nodes/"+quiet.ID.String()), &one)
	if one.PeopleOnline != nil || one.Connections == nil || *one.Connections != 6 {
		t.Fatalf("get n2 = %+v: connections known, people not counted", one)
	}

	if _, err := h.Store.Pool.Exec(context.Background(), `UPDATE nodes SET status = 'offline' WHERE id = $1`, n.ID); err != nil {
		t.Fatal(err)
	}
	one = live{}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &one)
	if one.PeopleOnline != nil || one.Connections != nil {
		t.Fatalf("get n1 = %+v: a server marked offline shows no live figures", one)
	}
	list.Items = nil
	c.JSON(c.Get("/api/v1/nodes"), &list)
	for _, it := range list.Items {
		if it.ID == n.ID && (it.PeopleOnline != nil || it.Connections != nil) {
			t.Fatalf("list n1 = %+v: a server marked offline shows no live figures", it)
		}
	}
	var sum summaryResp
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.SessionsLive != 6 {
		t.Fatalf("sessions_live = %d, want only the server that is still online", sum.SessionsLive)
	}
	if body := metricsText(t, h); strings.Contains(body, n.ID.String()) {
		t.Fatalf("metrics still report the offline server:\n%s", body)
	}
}

type liveResp struct {
	Live struct {
		Online      bool `json:"online"`
		Connections int  `json:"connections"`
		Devices     int  `json:"devices"`
		Devices15m  int  `json:"devices_15m"`
		IPs         int  `json:"ips"`
	} `json:"live"`
}

func TestKeyLiveComesFromPresence(t *testing.T) {
	h, c, n := ownerWithNode(t)
	k := keyOnNode(t, c, n.ID)
	other, _ := createNode(t, c, "n2.test")
	// What older panels summed per node: one address on two servers read as two.
	seedKeyStats(t, h, k.ID, n.ID, time.Now(), 3, 10)
	seedKeyStats(t, h, k.ID, other.ID, time.Now(), 2, 10)

	var got liveResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.Live.Online || got.Live.Connections != 0 {
		t.Fatalf("live = %+v: only the presence the stats worker counted is live", got.Live)
	}

	if _, err := h.Store.Pool.Exec(context.Background(),
		`INSERT INTO key_presence (access_key_id, connections, devices, devices_15m) VALUES ($1, 5, 1, 2)`, k.ID); err != nil {
		t.Fatal(err)
	}
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	if !got.Live.Online || got.Live.Connections != 5 || got.Live.Devices != 1 || got.Live.Devices15m != 2 || got.Live.IPs != 1 {
		t.Fatalf("get live = %+v", got.Live)
	}
	var list struct {
		Items []liveResp `json:"items"`
	}
	c.JSON(c.Get("/api/v1/keys"), &list)
	if len(list.Items) != 1 || !list.Items[0].Live.Online || list.Items[0].Live.Devices != 1 {
		t.Fatalf("list live = %+v", list.Items)
	}

	if _, err := h.Store.Pool.Exec(context.Background(), `UPDATE key_presence SET updated_at = now() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	got = liveResp{}
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.Live.Online || got.Live.Connections != 0 || got.Live.Devices15m != 0 {
		t.Fatalf("live = %+v: a row the worker stopped refreshing is not live", got.Live)
	}
}

func TestMonitoringCarriesPeopleOnline(t *testing.T) {
	h, c, n := ownerWithNode(t)
	base := time.Now().Add(-20 * time.Minute).Truncate(time.Minute)
	for i := range 10 {
		var p pgtype.Int4
		if i >= 5 {
			p = people(2)
		}
		snapshotAt(t, h, n.ID, base.Add(time.Duration(i)*time.Minute), 9, p)
	}
	fleetAt(t, h, time.Now(), 3, 4, 9)
	q := "from=" + base.Add(-time.Minute).Format(time.RFC3339) + "&to=" + time.Now().Format(time.RFC3339)

	type point struct {
		SessionsLive int    `json:"sessions_live"`
		PeopleOnline *int32 `json:"people_online"`
	}
	var out struct {
		Series map[string][]point `json:"series"`
		Fleet  struct {
			PeopleOnline    *int32 `json:"people_online"`
			PeopleOnline15m *int32 `json:"people_online_15m"`
			Connections     *int32 `json:"connections"`
		} `json:"fleet"`
	}
	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=60"), &out)
	pts := out.Series[n.ID.String()]
	if len(pts) != 10 || pts[0].PeopleOnline != nil || pts[9].PeopleOnline == nil || *pts[9].PeopleOnline != 2 || pts[9].SessionsLive != 9 {
		t.Fatalf("raw points = %+v: history before the count stays null", pts)
	}
	if f := out.Fleet; f.PeopleOnline == nil || *f.PeopleOnline != 3 || f.PeopleOnline15m == nil || *f.PeopleOnline15m != 4 ||
		f.Connections == nil || *f.Connections != 9 {
		t.Fatalf("fleet = %+v", out.Fleet)
	}

	c.JSON(c.Get("/api/v1/monitoring/overview?"+q+"&step=300"), &out)
	pts = out.Series[n.ID.String()]
	if len(pts) < 2 || pts[0].PeopleOnline != nil || pts[len(pts)-1].PeopleOnline == nil || *pts[len(pts)-1].PeopleOnline != 2 {
		t.Fatalf("bucketed points = %+v", pts)
	}

	var series struct {
		Points []point `json:"points"`
	}
	c.JSON(c.Get("/api/v1/monitoring/nodes/"+n.ID.String()+"/series?"+q), &series)
	if len(series.Points) != 10 || series.Points[0].PeopleOnline != nil || series.Points[9].PeopleOnline == nil {
		t.Fatalf("node series = %+v", series.Points)
	}
}
