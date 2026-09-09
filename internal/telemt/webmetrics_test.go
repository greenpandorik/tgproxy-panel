package telemt

import (
	"context"
	"net/http"
	"testing"
)

const webMetricsText = `# HELP telemt_web_carrier_selections_total Carrier selections
# TYPE telemt_web_carrier_selections_total counter
telemt_web_carrier_selections_total{carrier="https",disposition="applied"} 41
telemt_web_carrier_selections_total{carrier="https",disposition="cold"} 0
telemt_web_carrier_selections_total{carrier="websocket",disposition="applied"} 7
# TYPE telemt_web_carrier_reported_failures_total counter
telemt_web_carrier_reported_failures_total{carrier="websocket",phase="handshake",reason="timeout"} 3
telemt_web_carrier_reported_failures_total{carrier="websocket",phase="stream",reason="reset"} 1
# TYPE telemt_web_carrier_learning_entries gauge
telemt_web_carrier_learning_entries{kind="active"} 12
telemt_web_carrier_learning_entries{kind="expired"} 0
# TYPE telemt_web_rejections_total counter
telemt_web_rejections_total{reason="admission_closed"} 5
# TYPE telemt_web_session_closures_total counter
telemt_web_session_closures_total{carrier="https",reason="evicted"} 2
# TYPE telemt_web_bridge_recovery_events_total counter
telemt_web_bridge_recovery_events_total{event="recovered"} 4
telemt_connections_total 99
`

func TestParseWebMetricsKeepsCarriersAndLabels(t *testing.T) {
	m := ParseWebMetrics(webMetricsText)
	if !m.CarrierSelections.Present {
		t.Fatal("selections family missing")
	}
	https := m.CarrierSelections.Carrier(CarrierHTTPS)
	if v, ok := https.Get("applied"); !ok || v != 41 {
		t.Fatalf("https applied: %v %v", v, ok)
	}
	if v, ok := https.Get("cold"); !ok || v != 0 {
		t.Fatalf("a reported zero must stay a zero: %v %v", v, ok)
	}
	if got := m.CarrierSelections.CarrierTotals(); got[CarrierHTTPS] != 41 || got[CarrierWebsocket] != 7 {
		t.Fatalf("carrier totals: %+v", got)
	}
	if m.CarrierSelections.Total() != 48 {
		t.Fatalf("total: %v", m.CarrierSelections.Total())
	}
	if !m.CarrierFailures.Present {
		t.Fatal("failures family missing")
	}
	ws := m.CarrierFailures.Values[CarrierWebsocket]
	if ws[CarrierFailureKey{Phase: "handshake", Reason: "timeout"}] != 3 || ws[CarrierFailureKey{Phase: "stream", Reason: "reset"}] != 1 {
		t.Fatalf("failures: %+v", ws)
	}
	if m.CarrierFailures.CarrierTotals()[CarrierWebsocket] != 4 {
		t.Fatalf("failure totals: %+v", m.CarrierFailures.CarrierTotals())
	}
	if v, ok := m.LearningEntries.Get("active"); !ok || v != 12 {
		t.Fatalf("learning entries: %v %v", v, ok)
	}
	if v, ok := m.Rejections.Get("admission_closed"); !ok || v != 5 {
		t.Fatalf("rejections: %v %v", v, ok)
	}
	if m.SessionClosures.Carrier(CarrierHTTPS).Values["evicted"] != 2 {
		t.Fatalf("closures: %+v", m.SessionClosures.Values)
	}
	if v, ok := m.BridgeRecovery.Get("recovered"); !ok || v != 4 {
		t.Fatalf("bridge recovery: %v %v", v, ok)
	}
	if got := m.CarrierSelections.Carriers(); len(got) != 2 || got[0] != CarrierHTTPS {
		t.Fatalf("carriers: %+v", got)
	}
}

func TestAbsentFamilyIsAbsentAndNotZero(t *testing.T) {
	m := ParseWebMetrics("telemt_connections_total 5\n")
	if m.CarrierSelections.Present || m.CarrierFailures.Present || m.LearningOutcomes.Present {
		t.Fatalf("families invented out of nothing: %+v", m)
	}
	if m.LearningEntries.Present || m.Rejections.Present || m.SessionClosures.Present || m.BridgeRecovery.Present {
		t.Fatalf("families invented out of nothing: %+v", m)
	}
	if v, ok := m.Rejections.Get("admission_closed"); ok || v != 0 {
		t.Fatalf("absent family must not answer with a value: %v %v", v, ok)
	}
	if m.CarrierSelections.Carrier(CarrierHTTPS).Present {
		t.Fatal("carrier slice of an absent family must be absent too")
	}
	if m.CarrierSelections.CarrierTotals() != nil {
		t.Fatal("absent family must not produce totals")
	}

	present := ParseWebMetrics("# TYPE telemt_web_rejections_total counter\n")
	if !present.Rejections.Present {
		t.Fatal("a family with metadata but no samples is present with nothing reported")
	}
	if len(present.Rejections.Values) != 0 || present.Rejections.Total() != 0 {
		t.Fatalf("no samples means no values: %+v", present.Rejections)
	}
	zero := ParseWebMetrics(`telemt_web_rejections_total{reason="admission_closed"} 0` + "\n")
	if v, ok := zero.Rejections.Get("admission_closed"); !ok || v != 0 {
		t.Fatalf("a reported zero must be readable as zero: %v %v", v, ok)
	}
}

func TestParseWebMetricsSurvivesOddLines(t *testing.T) {
	text := `
# a bare comment
telemt_web_rejections_total{reason="quoted \"x\", y",extra="1"} 2 1757000000000
telemt_web_rejections_total{reason="broken" 4
not_a_metric
telemt_web_carrier_learning_outcomes_total{carrier="https",outcome="promoted"} 1.5e+01
`
	m := ParseWebMetrics(text)
	if v, ok := m.Rejections.Get(`quoted "x", y`); !ok || v != 2 {
		t.Fatalf("quoted label: %v %v %+v", v, ok, m.Rejections.Values)
	}
	if len(m.Rejections.Values) != 1 {
		t.Fatalf("malformed line kept: %+v", m.Rejections.Values)
	}
	if m.LearningOutcomes.Carrier(CarrierHTTPS).Values["promoted"] != 15 {
		t.Fatalf("exponent value: %+v", m.LearningOutcomes.Values)
	}
}

func TestClientWebMetrics(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(webMetricsText))
	})
	m, err := c.WebMetrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !m.CarrierSelections.Present || m.CarrierSelections.Carrier(CarrierHTTPS).Values["applied"] != 41 {
		t.Fatalf("metrics: %+v", m.CarrierSelections)
	}
}
