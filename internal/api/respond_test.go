package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 400, "bad_request", "nope", map[string]string{"name": "required"})
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"]["code"] != "bad_request" || body["error"]["message"] != "nope" {
		t.Fatalf("body %s", rec.Body.String())
	}
	if body["error"]["fields"].(map[string]any)["name"] != "required" {
		t.Fatalf("fields missing: %s", rec.Body.String())
	}
}
