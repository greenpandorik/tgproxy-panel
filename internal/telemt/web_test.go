package telemt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const webStatusBody = `{
  "operator_lifecycle": {
    "state": "draining", "epoch": 4, "age_ms": 1200,
    "admission_open": false, "effective_new_work_admission": false,
    "drain": {
      "operation_id": 17, "state": "draining", "timeout_secs": 60,
      "started_epoch_millis": 1757000000000, "deadline_epoch_millis": 1757000060000,
      "remaining_sessions": 3, "remaining_streams": 5, "remaining_websockets": 1,
      "force_close_signalled": false
    }
  },
  "runtime": {"runtime_instance": "0123456789abcdef0123456789abcdef", "learning": {"enabled": true, "policy_generation": 2, "epoch": 9, "entries": 12, "capacity": 4096, "lifetime_secs": 600, "health_secs": 30, "age_ms": 55}},
  "capacity": {
    "http_connection_capacity_action": "wait", "max_http_overload_connections": 64,
    "http_overload_timeout_ms": 1000,
    "resources": [{"resource":"http_connections","unit":"slots","used":7,"available":25,"limit":32,"closed":false}],
    "saturated_resources": [], "partial": [],
    "http_connection_overload_outcomes": [{"outcome":"wait_admitted","total":3}]
  },
  "carrier_negotiation": {"selection": {"https": {"applied": 4}}, "failure": {}}
}`

func TestWebStatusDecodesLifecycleLearningAndInstance(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) { ok(w, webStatusBody) })
	st, err := c.WebStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].method != http.MethodGet || (*calls)[0].path != "/v1/runtime/web/status" {
		t.Fatalf("request: %+v", (*calls)[0])
	}
	if st.RuntimeInstance != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("instance: %q", st.RuntimeInstance)
	}
	if st.OperatorLifecycle == nil || st.OperatorLifecycle.State != WebLifecycleDraining || st.OperatorLifecycle.AdmissionOpen {
		t.Fatalf("lifecycle: %+v", st.OperatorLifecycle)
	}
	d := st.OperatorLifecycle.Drain
	if d == nil || d.OperationID != "17" || d.RemainingSessions != 3 || d.RemainingWebsockets != 1 || d.Done() {
		t.Fatalf("drain: %+v", d)
	}
	if d.CompletedEpochMillis != nil {
		t.Fatalf("completed must stay nil while draining: %v", *d.CompletedEpochMillis)
	}
	if st.Runtime.Learning == nil || !st.Runtime.Learning.Enabled || st.Runtime.Learning.Entries != 12 {
		t.Fatalf("learning: %+v", st.Runtime.Learning)
	}
	if st.Capacity == nil || st.Capacity.HTTPConnectionCapacityAction != "wait" || len(st.Capacity.Resources) != 1 || st.Capacity.Resources[0].Available != 25 {
		t.Fatalf("capacity: %+v", st.Capacity)
	}
	if len(st.Capacity.HTTPConnectionOverloadOutcomes) != 1 || st.Capacity.HTTPConnectionOverloadOutcomes[0].Total != 3 {
		t.Fatalf("overload outcomes: %+v", st.Capacity)
	}
	if !st.HasCarrierNegotiation() {
		t.Fatal("carrier negotiation section lost")
	}
}

func TestWebStatusWithoutOptionalSectionsStaysNil(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		ok(w, `{"runtime_instance":"aa","runtime":{}}`)
	})
	st, err := c.WebStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.OperatorLifecycle != nil || st.Runtime.Learning != nil || st.Capacity != nil || st.HasCarrierNegotiation() {
		t.Fatalf("absent sections must stay nil: %+v", st)
	}
}

func TestWebSessionsPagesWithCarrierFilter(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			ok(w, `{"sessions":[{"session_id":"s1","carrier":"websocket","state":"live"}],"next_cursor":"c2","scanned":1000,"scan_truncated":true,"partial_sessions":1,"partial":[{"carrier":"websocket","state":"partial"}]}`)
			return
		}
		ok(w, `{"sessions":[{"session_id":"s2","carrier":"websocket","state":"live"}],"next_cursor":"","scanned":12,"scan_truncated":false,"partial_sessions":0}`)
	})
	ctx := context.Background()
	first, err := c.WebSessions(ctx, WebSessionsQuery{Carrier: CarrierWebsocket, State: "live", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v1/runtime/web/sessions?carrier=websocket&limit=50&state=live" {
		t.Fatalf("path: %q", (*calls)[0].path)
	}
	if len(first.Sessions) != 1 || first.Sessions[0].Carrier != CarrierWebsocket || first.NextCursor != "c2" {
		t.Fatalf("first page: %+v", first)
	}
	if !first.ScanTruncated || first.Scanned != 1000 || first.PartialSessions != 1 || len(first.Partial) != 1 {
		t.Fatalf("truncation fields: %+v", first)
	}
	var row map[string]any
	if err := json.Unmarshal(first.Sessions[0].Raw, &row); err != nil {
		t.Fatal(err)
	}
	if row["session_id"] != "s1" {
		t.Fatalf("raw row lost fields: %s", first.Sessions[0].Raw)
	}
	second, err := c.WebSessions(ctx, WebSessionsQuery{Carrier: CarrierWebsocket, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[1].path != "/v1/runtime/web/sessions?carrier=websocket&cursor=c2" {
		t.Fatalf("second path: %q", (*calls)[1].path)
	}
	if second.NextCursor != "" || len(second.Sessions) != 1 || second.ScanTruncated {
		t.Fatalf("second page: %+v", second)
	}
}

func TestWebSessionsRejectsBadFilterWithoutCallingTheNode(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) { ok(w, `{}`) })
	ctx := context.Background()
	if _, err := c.WebSessions(ctx, WebSessionsQuery{Carrier: "quic"}); err == nil {
		t.Fatal("unknown carrier accepted")
	}
	if _, err := c.WebSessions(ctx, WebSessionsQuery{Limit: 201}); err == nil {
		t.Fatal("limit above 200 accepted")
	}
	if len(*calls) != 0 {
		t.Fatalf("bad query still hit the node: %+v", *calls)
	}
}

func statusWithInstance(instance string) string {
	return `{"runtime_instance":"` + instance + `","operator_lifecycle":{"state":"running","admission_open":true},"runtime":{}}`
}

func TestLifecycleFetchesInstanceAndCachesIt(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			ok(w, statusWithInstance("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
			return
		}
		ok(w, `{}`)
	})
	ctx := context.Background()
	if err := c.WebPause(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.WebResume(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /v1/runtime/web/status",
		"POST /v1/runtime/web/lifecycle/pause",
		"POST /v1/runtime/web/lifecycle/resume",
	}
	if len(*calls) != len(want) {
		t.Fatalf("calls: %+v", *calls)
	}
	for i, w := range want {
		if got := (*calls)[i].method + " " + (*calls)[i].path; got != w {
			t.Fatalf("call %d: got %q want %q", i, got, w)
		}
	}
	if (*calls)[1].body != `{"runtime_instance":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}` {
		t.Fatalf("pause body: %s", (*calls)[1].body)
	}
}

func TestDrainIsAcceptedWithoutWaiting(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			ok(w, statusWithInstance("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		ok(w, `{"operation_id":"18","state":"draining","timeout_secs":45,"deadline_epoch_millis":1757000045000}`)
	})
	acc, err := c.WebDrain(context.Background(), 45)
	if err != nil {
		t.Fatal(err)
	}
	if acc.OperationID != "18" || acc.State != "draining" || acc.TimeoutSecs != 45 {
		t.Fatalf("accepted: %+v", acc)
	}
	drain := (*calls)[1]
	if drain.method != http.MethodPost || drain.path != "/v1/runtime/web/lifecycle/drain" {
		t.Fatalf("drain request: %+v", drain)
	}
	if drain.body != `{"runtime_instance":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","timeout_secs":45}` {
		t.Fatalf("drain body: %s", drain.body)
	}
}

func TestDrainRejectsTimeoutsTelemtWouldRefuse(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) { ok(w, `{}`) })
	ctx := context.Background()
	for _, secs := range []int{0, -1, 3601} {
		if _, err := c.WebDrain(ctx, secs); err == nil {
			t.Fatalf("timeout %d accepted", secs)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("invalid timeout still hit the node: %+v", *calls)
	}
}

func TestConcurrentDrainSurfacesTheTelemtCode(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			ok(w, statusWithInstance("cccccccccccccccccccccccccccccccc"))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"web_lifecycle_in_progress","message":"drain already running"}}`))
	})
	_, err := c.WebDrain(context.Background(), 30)
	if !IsCode(err, ErrCodeLifecycleInProgress) {
		t.Fatalf("want web_lifecycle_in_progress, got %v", err)
	}
}

func mismatch(w http.ResponseWriter) {
	w.WriteHeader(http.StatusConflict)
	_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"web_runtime_mismatch","message":"runtime restarted"}}`))
}

func TestRuntimeMismatchRereadsTheInstanceAndRetriesOnce(t *testing.T) {
	instances := []string{"11111111111111111111111111111111", "22222222222222222222222222222222"}
	var reads, resets int
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			i := reads
			if i >= len(instances) {
				i = len(instances) - 1
			}
			reads++
			ok(w, statusWithInstance(instances[i]))
			return
		}
		resets++
		if resets == 1 {
			mismatch(w)
			return
		}
		ok(w, `{"entries_cleared":7,"epoch":10}`)
	})
	res, err := c.ResetCarrierLearning(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.EntriesCleared != 7 || res.Epoch != 10 {
		t.Fatalf("reset: %+v", res)
	}
	if reads != 2 || resets != 2 {
		t.Fatalf("want two status reads and two attempts, got %d/%d", reads, resets)
	}
	last := (*calls)[len(*calls)-1]
	if last.path != "/v1/runtime/web/carrier-learning/reset" || !strings.Contains(last.body, instances[1]) {
		t.Fatalf("retry did not use the re-read instance: %+v", last)
	}
}

func TestRuntimeMismatchTwiceStopsInsteadOfLooping(t *testing.T) {
	var reads, attempts int
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			reads++
			ok(w, statusWithInstance("33333333333333333333333333333333"))
			return
		}
		attempts++
		mismatch(w)
	})
	err := c.WebPause(context.Background())
	if !IsCode(err, ErrCodeRuntimeMismatch) {
		t.Fatalf("want web_runtime_mismatch, got %v", err)
	}
	if attempts != 2 || reads != 2 {
		t.Fatalf("retry must happen exactly once: %d attempts, %d status reads", attempts, reads)
	}
}

func TestLifecycleFailsWhenStatusCarriesNoInstance(t *testing.T) {
	c, calls := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			ok(w, `{"runtime":{}}`)
			return
		}
		ok(w, `{}`)
	})
	err := c.WebPause(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "web_runtime_unavailable" {
		t.Fatalf("want web_runtime_unavailable, got %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("a write went out without an instance: %+v", *calls)
	}
}
