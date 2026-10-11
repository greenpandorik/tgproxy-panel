package api_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type webPolicyResp struct {
	Policy     domain.WebPolicy `json:"policy"`
	Default    domain.WebPolicy `json:"default"`
	Overridden bool             `json:"overridden"`
}

func TestNodeWebPolicyDefaultsForANodeThatHasNone(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")

	var got webPolicyResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/web-policy"), &got)
	want := domain.DefaultWebPolicy()
	if got.Overridden {
		t.Fatalf("a fresh node has no override: %+v", got)
	}
	if got.Policy.Carrier != want.Carrier || len(got.Policy.Carriers) != len(want.Carriers) ||
		!got.Policy.CarrierLearning || got.Policy.Aggressiveness != want.Aggressiveness {
		t.Fatalf("policy %+v", got.Policy)
	}
	if got.Policy.Timeouts.CarrierLearningSecs != want.Timeouts.CarrierLearningSecs ||
		len(got.Policy.Timeouts.NegotiationDeadlinesSecs) != len(want.Timeouts.NegotiationDeadlinesSecs) {
		t.Fatalf("timeouts %+v", got.Policy.Timeouts)
	}
	if got.Default.Carrier != want.Carrier {
		t.Fatalf("default %+v", got.Default)
	}
}

func TestNodeWebPolicyUpdateMarksTheNodeDirty(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")

	var got webPolicyResp
	resp := c.Put("/api/v1/nodes/"+n.ID.String()+"/web-policy", map[string]any{
		"preset":                             "prefer_websocket",
		"carriers":                           []string{"websocket-lanes", "websocket"},
		"carrier_negotiation_aggressiveness": "balanced",
		"overload":                           map[string]any{"preset": "high_load", "connection_capacity_action": "respond"},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("put %d", resp.StatusCode)
	}
	c.JSON(resp, &got)
	if !got.Overridden || len(got.Policy.Carriers) != 2 || got.Policy.Aggressiveness != domain.AggressivenessBalanced || got.Policy.Overload.ConnectionCapacityAction != domain.WebCapacityRespond {
		t.Fatalf("policy %+v", got.Policy)
	}
	if got.Policy.Timeouts.BridgeRetrySecs != domain.DefaultWebPolicy().Timeouts.BridgeRetrySecs {
		t.Fatalf("an untouched timeout must keep the default: %+v", got.Policy.Timeouts)
	}

	var node nodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &node)
	if !node.Dirty {
		t.Fatalf("a policy change must mark the node dirty: %+v", node)
	}

	// Re-sending the same policy changes nothing, so it must not bump the node again.
	if err := h.Store.Q.SetNodeDirty(t.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: false}); err != nil {
		t.Fatal(err)
	}
	c.JSON(c.Put("/api/v1/nodes/"+n.ID.String()+"/web-policy", map[string]any{
		"preset":                             "prefer_websocket",
		"carriers":                           []string{"websocket-lanes", "websocket"},
		"carrier_negotiation_aggressiveness": "balanced",
		"overload":                           map[string]any{"preset": "high_load", "connection_capacity_action": "respond"},
	}), &got)
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &node)
	if node.Dirty {
		t.Fatalf("an unchanged policy must not mark the node dirty")
	}
}

func TestNodeWebPolicyRejectsWhatTelemtWouldRefuse(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String() + "/web-policy"

	for name, body := range map[string]map[string]any{
		"learning window":  {"timeouts": map[string]any{"carrier_learning_secs": 99999}},
		"deadline count":   {"timeouts": map[string]any{"carrier_negotiation_deadlines_secs": []int{3, 5, 8}}},
		"deadlines order":  {"timeouts": map[string]any{"carrier_negotiation_deadlines_secs": []int{3, 5, 5, 8}}},
		"retry below wait": {"timeouts": map[string]any{"bridge_request_secs": 30, "bridge_retry_secs": 5}},
		"unknown carrier":  {"carriers": []string{"carrier-pigeon"}},
		"empty carriers":   {"carriers": []string{}},
		"duplicate":        {"carriers": []string{"websocket", "websocket"}},
		"aggressiveness":   {"carrier_negotiation_aggressiveness": "reckless"},
		"capacity action":  {"overload": map[string]any{"connection_capacity_action": "queue"}},
	} {
		resp := c.Put(path, body)
		if resp.StatusCode != 422 {
			t.Fatalf("%s: expected 422, got %d", name, resp.StatusCode)
		}
		var out struct {
			Error struct {
				Code   string            `json:"code"`
				Fields map[string]string `json:"fields"`
			} `json:"error"`
		}
		c.JSON(resp, &out)
		if out.Error.Code != "validation" || len(out.Error.Fields) != 1 {
			t.Fatalf("%s: expected one field detail, got %+v", name, out.Error)
		}
	}

	var got webPolicyResp
	c.JSON(c.Get(path), &got)
	if got.Overridden {
		t.Fatalf("a refused policy must not be stored: %+v", got)
	}
}

func TestWebAdvancedPolicyRequiresSupportedEngineAndAgent(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String() + "/web-policy"
	for _, versions := range [][2]string{{"3.5.13", "2.17.0"}, {"3.5.14", "2.16.1"}, {"", ""}, {"3.5.14", "2.17.0"}} {
		_, err := h.Store.Pool.Exec(t.Context(), `UPDATE nodes SET telemt_version=$2,agent_version=$3 WHERE id=$1`, n.ID, versions[0], versions[1])
		if err != nil {
			t.Fatal(err)
		}
		resp := c.Put(path, map[string]any{"base_path": "telegram/web"})
		want := 422
		if versions[0] == "3.5.14" && versions[1] == "2.17.0" {
			want = 200
		}
		if resp.StatusCode != want {
			t.Fatalf("versions %v: status %d want %d", versions, resp.StatusCode, want)
		}
		_ = resp.Body.Close()
	}
}

func TestHTTPDecoyHostnameAssignmentRequiresStartupOptIn(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	_, err := h.Store.Pool.Exec(t.Context(), `UPDATE nodes SET telemt_version='3.5.14',agent_version='2.17.0',telemt_capabilities='{"HttpUpstreamDecoy":true}' WHERE id=$1`, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + n.ID.String() + "/site/upstream"
	resp := c.Post(path, map[string]any{"origin": "http://origin.internal:8080"})
	if resp.StatusCode != 422 {
		t.Fatalf("implicit hostname DNS status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = c.Put("/api/v1/nodes/"+n.ID.String()+"/web-policy", map[string]any{"decoy_resolve": "startup"})
	if resp.StatusCode != 200 {
		t.Fatalf("startup policy status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = c.Post(path, map[string]any{"origin": "http://origin.internal:8080"})
	if resp.StatusCode != 200 {
		t.Fatalf("startup assignment status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	var site struct {
		Origin string `json:"origin"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/site"), &site)
	if site.Origin != "http://origin.internal:8080" {
		t.Fatalf("hostname origin lost: %+v", site)
	}
	resp = c.Post(path, map[string]any{"origin": "http://8.8.8.8:8080"})
	if resp.StatusCode != 422 {
		t.Fatal("startup enabled public literal origin")
	}
	_ = resp.Body.Close()
}

type policyPrefixAckDriver struct{ *nodedriver.Mock }

func (d policyPrefixAckDriver) Apply(ctx context.Context, id uuid.UUID, req nodedriver.ApplyRequest) (nodedriver.ApplyResult, error) {
	res, err := d.Mock.Apply(ctx, id, req)
	if req.WebPolicy != nil {
		res.AppliedWebBasePath = req.WebPolicy.BasePath
	}
	return res, err
}

// JSON decoding must not mutate the snapshot used to detect an unchanged PUT.
func TestWebPolicyRepeatedPutPersistsFalseRootAndStartup(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	ctx := t.Context()
	_, err := h.Store.Pool.Exec(ctx, `UPDATE nodes SET telemt_version='3.5.14',agent_version='2.17.0',telemt_capabilities='{"HttpUpstreamDecoy":true}' WHERE id=$1`, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + n.ID.String() + "/web-policy"
	resp := c.Put(path, map[string]any{"carrier_method": "put", "conveyor": true, "base_path": "old/web", "decoy_resolve": "never"})
	if resp.StatusCode != 200 {
		t.Fatalf("first PUT %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	var key struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "prefix", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}}), &key)
	h.Mock.SetOnline(n.ID, true)
	apply := worker.NewApply(h.Store, h.Box, policyPrefixAckDriver{h.Mock}, time.Hour, slog.New(slog.DiscardHandler))
	if err := apply.ApplyNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	before, err := h.Store.Q.GetNode(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got webPolicyResp
	c.JSON(c.Put(path, map[string]any{"carrier_method": "post", "conveyor": false, "base_path": "", "decoy_resolve": "startup"}), &got)
	if got.Policy.CarrierMethod == nil || *got.Policy.CarrierMethod != "post" || got.Policy.Conveyor == nil || *got.Policy.Conveyor || got.Policy.BasePath == nil || *got.Policy.BasePath != "" || got.Policy.DecoyResolve == nil || *got.Policy.DecoyResolve != "startup" {
		t.Fatalf("second PUT ignored pointer changes: %+v", got.Policy)
	}
	stored, err := h.Store.Q.GetNode(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	var overrides map[string]any
	if err := json.Unmarshal(stored.TelemtWebPolicy, &overrides); err != nil {
		t.Fatal(err)
	}
	if overrides["carrier_method"] != "post" || overrides["conveyor"] != false || overrides["base_path"] != "" || overrides["decoy_resolve"] != "startup" {
		t.Fatalf("second PUT not persisted: %s", stored.TelemtWebPolicy)
	}
	if !stored.Dirty || stored.DirtySeq <= before.DirtySeq {
		t.Fatal("changed policy did not schedule apply")
	}
	links, err := h.Deps.Keys.NodeLinks(ctx, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(links[0].Links[0].TMe, "server=n1.test%2Fold%2Fweb") {
		t.Fatal("desired root reset changed links before apply")
	}
	if err := apply.ApplyNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	links, err = h.Deps.Keys.NodeLinks(ctx, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(links[0].Links[0].TMe, "server=n1.test&secret=") {
		t.Fatalf("applied root reset did not restore root link: %+v", links)
	}
	c.JSON(c.Get(path), &got)
	if got.Policy.BasePath == nil || *got.Policy.BasePath != "" || got.Policy.Conveyor == nil || *got.Policy.Conveyor {
		t.Fatal("GET did not retain reset values")
	}
	resp = c.Post("/api/v1/nodes/"+n.ID.String()+"/site/upstream", map[string]any{"origin": "http://origin.internal:8080"})
	if resp.StatusCode != 200 {
		t.Fatalf("persisted startup did not allow hostname origin: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestWebPolicyDeadlinePutUsesIndependentCurrentSnapshot(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String() + "/web-policy"
	for _, deadlines := range [][]int{{4, 6, 9, 13}, {5, 7, 10, 14}} {
		var got webPolicyResp
		c.JSON(c.Put(path, map[string]any{"timeouts": map[string]any{"carrier_negotiation_deadlines_secs": deadlines}}), &got)
		if !reflect.DeepEqual(got.Policy.Timeouts.NegotiationDeadlinesSecs, deadlines) {
			t.Fatalf("valid deadline edit was treated as unchanged: %v", got.Policy.Timeouts.NegotiationDeadlinesSecs)
		}
		c.JSON(c.Get(path), &got)
		if !reflect.DeepEqual(got.Policy.Timeouts.NegotiationDeadlinesSecs, deadlines) {
			t.Fatal("deadline edit not persisted")
		}
	}
}

func TestWebPolicyPutDistinguishesNullFromOmittedFields(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	path := "/api/v1/nodes/" + n.ID.String() + "/web-policy"
	_, err := h.Store.Pool.Exec(t.Context(), `UPDATE nodes SET telemt_version='3.5.14',agent_version='2.17.0' WHERE id=$1`, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got webPolicyResp
	c.JSON(c.Put(path, map[string]any{"carrier_method": "put", "conveyor": true, "base_path": "keep/web", "decoy_resolve": "never"}), &got)
	got = webPolicyResp{}
	c.JSON(c.Put(path, map[string]any{"conveyor": nil}), &got)
	if got.Policy.Conveyor != nil || got.Policy.CarrierMethod == nil || *got.Policy.CarrierMethod != "put" || got.Policy.BasePath == nil || *got.Policy.BasePath != "keep/web" || got.Policy.DecoyResolve == nil || *got.Policy.DecoyResolve != "never" {
		t.Fatalf("null/omitted semantics changed: %+v", got.Policy)
	}
	got = webPolicyResp{}
	c.JSON(c.Get(path), &got)
	if got.Policy.Conveyor != nil || got.Policy.BasePath == nil || *got.Policy.BasePath != "keep/web" {
		t.Fatal("null or omitted policy not persisted")
	}
}
