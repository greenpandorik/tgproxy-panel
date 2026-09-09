package api_test

import (
	"testing"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/store/db"
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
	})
	if resp.StatusCode != 200 {
		t.Fatalf("put %d", resp.StatusCode)
	}
	c.JSON(resp, &got)
	if !got.Overridden || len(got.Policy.Carriers) != 2 || got.Policy.Aggressiveness != domain.AggressivenessBalanced {
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
