package api_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

type webCarrierShare struct {
	Carrier    string   `json:"carrier"`
	Selections *int64   `json:"selections"`
	Share      *float64 `json:"share"`
}

type webCarriersResp struct {
	Samples           int64             `json:"samples"`
	CounterResets     int64             `json:"counter_resets"`
	CarrierSelections *int64            `json:"carrier_selections"`
	CarrierFailures   *int64            `json:"carrier_failures"`
	RejectedAttempts  *int64            `json:"rejected_attempts"`
	EvictedSessions   *int64            `json:"evicted_sessions"`
	BridgeRecoveries  *int64            `json:"bridge_recoveries"`
	LearningEntries   *int64            `json:"learning_entries"`
	Distribution      []webCarrierShare `json:"carrier_selection_distribution"`
}

func i8(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: true} }

// insertWebSnapshots writes one snapshot per element, a minute apart, ending just before now.
func insertWebSnapshots(t *testing.T, h *apitest.Harness, nodeID uuid.UUID, rows []db.InsertSnapshotParams) time.Time {
	t.Helper()
	base := time.Now().Add(-time.Duration(len(rows)+5) * time.Minute)
	for i, row := range rows {
		row.NodeID = nodeID
		row.MtproxyRaw = []byte("{}")
		if err := h.Store.Q.InsertSnapshot(t.Context(), row); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Minute)
		if _, err := h.Store.Pool.Exec(t.Context(),
			`UPDATE node_stats_snapshots SET taken_at = $1 WHERE node_id = $2 AND taken_at > $1`, at, nodeID); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func windowQuery(from time.Time) string {
	return "?from=" + from.Add(-time.Minute).Format(time.RFC3339) + "&to=" + time.Now().Format(time.RFC3339)
}

func TestWebCarriersReportsUnmeasuredMetricsAsNull(t *testing.T) {
	h, c, n := ownerWithNode(t)
	from := insertWebSnapshots(t, h, n.ID, []db.InsertSnapshotParams{
		{WebCarrierSelectionsHttps: i8(10)},
		{WebCarrierSelectionsHttps: i8(40)},
	})

	var out webCarriersResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/web/carriers"+windowQuery(from)), &out)

	if out.CarrierSelections == nil || *out.CarrierSelections != 30 {
		t.Fatalf("carrier_selections = %v, want 30", out.CarrierSelections)
	}
	for name, v := range map[string]*int64{
		"carrier_failures":  out.CarrierFailures,
		"rejected_attempts": out.RejectedAttempts,
		"evicted_sessions":  out.EvictedSessions,
		"bridge_recoveries": out.BridgeRecoveries,
		"learning_entries":  out.LearningEntries,
	} {
		if v != nil {
			t.Fatalf("%s was never reported and must be null, got %d", name, *v)
		}
	}
	if len(out.Distribution) != 4 {
		t.Fatalf("distribution = %+v", out.Distribution)
	}
	for _, row := range out.Distribution {
		switch row.Carrier {
		case "https":
			if row.Selections == nil || *row.Selections != 30 || row.Share == nil || *row.Share != 1 {
				t.Fatalf("https row = %+v", row)
			}
		default:
			if row.Selections != nil || row.Share != nil {
				t.Fatalf("%s was never reported and must be null, got %+v", row.Carrier, row)
			}
		}
	}
}

func TestWebCarriersDerivesSharesFromTheCounters(t *testing.T) {
	h, c, n := ownerWithNode(t)
	from := insertWebSnapshots(t, h, n.ID, []db.InsertSnapshotParams{
		{WebCarrierSelectionsHttps: i8(0), WebCarrierSelectionsWebsocketLanes: i8(0), WebCarrierFailures: i8(0), WebLearningEntries: pgtype.Int4{Int32: 4, Valid: true}},
		{WebCarrierSelectionsHttps: i8(10), WebCarrierSelectionsWebsocketLanes: i8(20), WebCarrierFailures: i8(3), WebLearningEntries: pgtype.Int4{Int32: 7, Valid: true}},
		{WebCarrierSelectionsHttps: i8(25), WebCarrierSelectionsWebsocketLanes: i8(75), WebCarrierFailures: i8(3), WebLearningEntries: pgtype.Int4{Int32: 11, Valid: true}},
	})

	var out webCarriersResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/web/carriers"+windowQuery(from)), &out)

	if out.CarrierSelections == nil || *out.CarrierSelections != 100 {
		t.Fatalf("carrier_selections = %v, want 25+75", out.CarrierSelections)
	}
	if out.CarrierFailures == nil || *out.CarrierFailures != 3 {
		t.Fatalf("carrier_failures = %v", out.CarrierFailures)
	}
	if out.LearningEntries == nil || *out.LearningEntries != 11 {
		t.Fatalf("learning_entries = %v, want the last gauge reading", out.LearningEntries)
	}
	shares := map[string]float64{}
	for _, row := range out.Distribution {
		if row.Share != nil {
			shares[row.Carrier] = *row.Share
		}
	}
	if shares["https"] != 0.25 || shares["websocket-lanes"] != 0.75 {
		t.Fatalf("shares = %+v", shares)
	}
}

func TestWebCarriersTreatsACounterResetAsARestart(t *testing.T) {
	h, c, n := ownerWithNode(t)
	from := insertWebSnapshots(t, h, n.ID, []db.InsertSnapshotParams{
		{WebCarrierFailures: i8(100)},
		{WebCarrierFailures: i8(300)},
		{WebCarrierFailures: i8(50)}, // telemt restarted: the counter went back to zero
		{WebCarrierFailures: i8(90)},
	})

	var out webCarriersResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/web/carriers"+windowQuery(from)), &out)

	if out.CarrierFailures == nil || *out.CarrierFailures != 240 {
		t.Fatalf("carrier_failures = %v, want 200 before the restart plus 40 after it", out.CarrierFailures)
	}
	if out.CounterResets != 1 {
		t.Fatalf("counter_resets = %d, want 1", out.CounterResets)
	}
	if out.Samples != 4 {
		t.Fatalf("samples = %d", out.Samples)
	}
}

func TestWebCarriersOverAnEmptyWindowIsAllNull(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var out webCarriersResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/web/carriers"+windowQuery(time.Now().Add(-time.Hour))), &out)
	if out.CarrierSelections != nil || out.CarrierFailures != nil || out.LearningEntries != nil {
		t.Fatalf("a window with no snapshots measures nothing: %+v", out)
	}
	if out.Samples != 0 || out.CounterResets != 0 {
		t.Fatalf("samples/resets = %d/%d", out.Samples, out.CounterResets)
	}
}

func TestFleetWebCarriersSumsTheNodes(t *testing.T) {
	h, c, n := ownerWithNode(t)
	second, _ := createNode(t, c, "n2.test")
	from := insertWebSnapshots(t, h, n.ID, []db.InsertSnapshotParams{
		{WebCarrierSelectionsHttps: i8(0)},
		{WebCarrierSelectionsHttps: i8(40)},
	})
	insertWebSnapshots(t, h, second.ID, []db.InsertSnapshotParams{
		{WebCarrierSelectionsWebsocket: i8(10)},
		{WebCarrierSelectionsWebsocket: i8(70)},
	})

	var out webCarriersResp
	c.JSON(c.Get("/api/v1/monitoring/web/carriers"+windowQuery(from)), &out)

	if out.CarrierSelections == nil || *out.CarrierSelections != 100 {
		t.Fatalf("fleet carrier_selections = %v, want 40+60", out.CarrierSelections)
	}
	shares := map[string]float64{}
	for _, row := range out.Distribution {
		if row.Share != nil {
			shares[row.Carrier] = *row.Share
		}
	}
	if shares["https"] != 0.4 || shares["websocket"] != 0.6 {
		t.Fatalf("fleet shares = %+v", shares)
	}
	if out.CarrierFailures != nil {
		t.Fatalf("no node reported failures, so the fleet must report null: %d", *out.CarrierFailures)
	}
}

func TestNodeHealthCarriesTheWebRuntimeState(t *testing.T) {
	h, c, n := ownerWithNode(t)
	report := nodedriver.HealthReport{RelayActive: true}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Health map[string]any `json:"health"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &out)
	if v, ok := out.Health["web_runtime"]; !ok || v != nil {
		t.Fatalf("a node that reported no WEB runtime must say null, got %v", v)
	}

	report.Web = &nodedriver.WebTelemetry{Runtime: &nodedriver.WebRuntimeState{
		RuntimeInstance: "inst-1",
		Lifecycle:       &nodedriver.WebLifecycleState{State: "draining"},
		Capacity: &nodedriver.WebCapacityState{
			ConnectionCapacityAction: "wait",
			Resources:                []nodedriver.WebCapacityResource{{Resource: "http_connections", Unit: "slots", Used: 7, Limit: 32, Available: 25}},
			OverloadOutcomes:         &nodedriver.WebCounterFamily{Samples: []nodedriver.WebCounterSample{{Label: "wait_admitted", Value: 3}}},
		},
	}}
	raw, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &out)
	rt, _ := out.Health["web_runtime"].(map[string]any)
	if rt == nil || rt["runtime_instance"] != "inst-1" {
		t.Fatalf("web_runtime = %+v", out.Health["web_runtime"])
	}
	if rt["learning"] != nil {
		t.Fatalf("a runtime with no learning section must say null, got %v", rt["learning"])
	}
	lc, _ := rt["lifecycle"].(map[string]any)
	if lc == nil || lc["state"] != "draining" || lc["drain"] != nil {
		t.Fatalf("lifecycle = %+v", rt["lifecycle"])
	}
	capacity, _ := rt["capacity"].(map[string]any)
	if capacity == nil || capacity["connection_capacity_action"] != "wait" {
		t.Fatalf("capacity = %+v", rt["capacity"])
	}
	outcomes, _ := capacity["overload_outcomes"].(map[string]any)
	if outcomes["wait_admitted"] != float64(3) {
		t.Fatalf("overload outcomes = %+v", outcomes)
	}
}

// A healthy node saturates nothing, and that is the common case: the list is empty, not absent.
// It has to reach the browser as [] - the WEB tab reads its length - and a nil Go slice would
// marshal to null and take the tab down.
func TestWebCapacityNameListsAreArraysWhenEmpty(t *testing.T) {
	h, c, n := ownerWithNode(t)
	report := nodedriver.HealthReport{RelayActive: true, Web: &nodedriver.WebTelemetry{
		Runtime: &nodedriver.WebRuntimeState{
			RuntimeInstance: "inst-1",
			Capacity: &nodedriver.WebCapacityState{
				ConnectionCapacityAction: "wait",
				Resources:                []nodedriver.WebCapacityResource{{Resource: "http_connections", Unit: "slots", Used: 7, Limit: 32, Available: 25}},
			},
		},
	}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Health map[string]any `json:"health"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &out)
	rt, _ := out.Health["web_runtime"].(map[string]any)
	capacity, _ := rt["capacity"].(map[string]any)
	if capacity == nil {
		t.Fatalf("web_runtime = %+v", out.Health["web_runtime"])
	}
	for _, field := range []string{"saturated_resources", "partial"} {
		v, ok := capacity[field]
		if !ok {
			t.Fatalf("%s is missing from capacity %+v", field, capacity)
		}
		list, isList := v.([]any)
		if !isList {
			t.Fatalf("%s = %v (%T), want an empty array", field, v, v)
		}
		if len(list) != 0 {
			t.Fatalf("%s = %v, want empty", field, list)
		}
	}

	report.Web.Runtime.Capacity.SaturatedResources = []string{"http_connections"}
	report.Web.Runtime.Capacity.Partial = []string{"websockets"}
	if raw, err = json.Marshal(report); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{ID: n.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &out)
	rt, _ = out.Health["web_runtime"].(map[string]any)
	capacity, _ = rt["capacity"].(map[string]any)
	if got, _ := capacity["saturated_resources"].([]any); len(got) != 1 || got[0] != "http_connections" {
		t.Fatalf("saturated_resources = %v", capacity["saturated_resources"])
	}
	if got, _ := capacity["partial"].([]any); len(got) != 1 || got[0] != "websockets" {
		t.Fatalf("partial = %v", capacity["partial"])
	}
}
