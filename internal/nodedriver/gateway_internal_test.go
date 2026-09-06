package nodedriver

import (
	"errors"
	"testing"

	agentv1 "tgwebproxy/proto/agent/v1"
)

// TestApplyResultFromEmptyResponse covers debt item 6: a reply carrying no
// ApplyResponse must not read as a successful apply.
func TestApplyResultFromEmptyResponse(t *testing.T) {
	for name, resp := range map[string]*agentv1.Response{
		"nil response":  nil,
		"no apply body": {Body: &agentv1.Response_Health{Health: &agentv1.HealthReport{}}},
	} {
		res, err := applyResultFrom(resp, nil)
		if err == nil {
			t.Errorf("%s: got nil error, want %q", name, "empty apply response")
		} else if err.Error() != "empty apply response" {
			t.Errorf("%s: got %v", name, err)
		}
		if res.OK {
			t.Errorf("%s: result reports OK", name)
		}
	}
}

func TestApplyResultFromKeepsTransportError(t *testing.T) {
	want := errors.New("deadline exceeded")
	if _, err := applyResultFrom(nil, want); !errors.Is(err, want) {
		t.Fatalf("got %v, want the transport error", err)
	}
}

func TestApplyResultFromOKAndFailure(t *testing.T) {
	ok := &agentv1.Response{Body: &agentv1.Response_Apply{Apply: &agentv1.ApplyResult{Ok: true, RestartedRelay: true, Log: "done"}}}
	res, err := applyResultFrom(ok, nil)
	if err != nil || !res.OK || !res.RestartedRelay || res.Log != "done" {
		t.Fatalf("ok apply mapped wrong: %+v %v", res, err)
	}
	bad := &agentv1.Response{Body: &agentv1.Response_Apply{Apply: &agentv1.ApplyResult{Ok: false, RolledBack: true, Log: "boom"}}}
	res, err = applyResultFrom(bad, nil)
	if err == nil || res.Log != "boom" || !res.RolledBack {
		t.Fatalf("failed apply mapped wrong: %+v %v", res, err)
	}
}
