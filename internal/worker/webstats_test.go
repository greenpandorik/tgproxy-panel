package worker_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func webSnapshot(t *testing.T, report nodedriver.HealthReport) db.NodeStatsSnapshot {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 1\n")
	if err := f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.Q.SetNodeHeartbeat(ctx, db.SetNodeHeartbeatParams{ID: f.node.ID, Status: db.NodeStatusOnline, LastHealth: raw}); err != nil {
		t.Fatal(err)
	}
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, err := f.st.Q.LatestSnapshots(ctx)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots %+v: %v", snaps, err)
	}
	return snaps[0]
}

func TestSnapshotWritesTheReportedWebCounters(t *testing.T) {
	got := webSnapshot(t, nodedriver.HealthReport{
		RelayActive: true,
		Web: &nodedriver.WebTelemetry{
			CarrierSelections: &nodedriver.WebCounterFamily{Samples: []nodedriver.WebCounterSample{
				{Carrier: "https", Label: "applied", Value: 12},
				{Carrier: "https", Label: "rejected", Value: 5},
				{Carrier: "websocket-lanes", Label: "applied", Value: 30},
			}},
			Rejections: &nodedriver.WebCounterFamily{Samples: []nodedriver.WebCounterSample{
				{Label: "capacity", Value: 4}, {Label: "policy", Value: 1},
			}},
			LearningEntries: &nodedriver.WebCounterFamily{Samples: []nodedriver.WebCounterSample{{Label: "carrier", Value: 9}}},
		},
	})
	if !got.WebCarrierSelectionsHttps.Valid || got.WebCarrierSelectionsHttps.Int64 != 12 {
		t.Fatalf("https selections = %+v, want the applied disposition only", got.WebCarrierSelectionsHttps)
	}
	if !got.WebCarrierSelectionsWebsocketLanes.Valid || got.WebCarrierSelectionsWebsocketLanes.Int64 != 30 {
		t.Fatalf("websocket-lanes selections = %+v", got.WebCarrierSelectionsWebsocketLanes)
	}
	if !got.WebRejectedAttempts.Valid || got.WebRejectedAttempts.Int64 != 5 {
		t.Fatalf("rejections = %+v, want the family total", got.WebRejectedAttempts)
	}
	if !got.WebLearningEntries.Valid || got.WebLearningEntries.Int32 != 9 {
		t.Fatalf("learning entries = %+v", got.WebLearningEntries)
	}
	for name, col := range map[string]bool{
		"web_carrier_selections_websocket": got.WebCarrierSelectionsWebsocket.Valid,
		"web_carrier_failures":             got.WebCarrierFailures.Valid,
		"web_evicted_sessions":             got.WebEvictedSessions.Valid,
		"web_bridge_recoveries":            got.WebBridgeRecoveries.Valid,
	} {
		if col {
			t.Fatalf("%s was not reported and must stay NULL, not 0", name)
		}
	}
}

func TestSnapshotLeavesEveryWebColumnNullWithoutTelemetry(t *testing.T) {
	got := webSnapshot(t, nodedriver.HealthReport{RelayActive: true, CPUPercent: 5})
	if got.CpuPercent != 5 {
		t.Fatalf("the rest of the heartbeat must still land: %+v", got.CpuPercent)
	}
	for name, valid := range map[string]bool{
		"web_carrier_selections_https":           got.WebCarrierSelectionsHttps.Valid,
		"web_carrier_selections_https_lanes":     got.WebCarrierSelectionsHttpsLanes.Valid,
		"web_carrier_selections_websocket":       got.WebCarrierSelectionsWebsocket.Valid,
		"web_carrier_selections_websocket_lanes": got.WebCarrierSelectionsWebsocketLanes.Valid,
		"web_carrier_failures":                   got.WebCarrierFailures.Valid,
		"web_rejected_attempts":                  got.WebRejectedAttempts.Valid,
		"web_evicted_sessions":                   got.WebEvictedSessions.Valid,
		"web_bridge_recoveries":                  got.WebBridgeRecoveries.Valid,
		"web_learning_entries":                   got.WebLearningEntries.Valid,
	} {
		if valid {
			t.Fatalf("%s must be NULL for a node that reported no WEB telemetry", name)
		}
	}
}

func TestSnapshotKeepsADeclaredEmptyFamilyAsZero(t *testing.T) {
	got := webSnapshot(t, nodedriver.HealthReport{
		RelayActive: true,
		Web:         &nodedriver.WebTelemetry{CarrierFailures: &nodedriver.WebCounterFamily{}},
	})
	if !got.WebCarrierFailures.Valid || got.WebCarrierFailures.Int64 != 0 {
		t.Fatalf("a family telemt declared and never incremented is a measured zero: %+v", got.WebCarrierFailures)
	}
}
