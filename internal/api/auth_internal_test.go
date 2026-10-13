package api

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"tgwebproxy/internal/crypto"
)

func TestPasswordUnavailableAnswers503(t *testing.T) {
	for _, err := range []error{crypto.ErrBusy, fmt.Errorf("wrapped: %w", crypto.ErrBusy), context.Canceled} {
		rec := httptest.NewRecorder()
		if !passwordUnavailable(rec, err) {
			t.Fatalf("%v: not treated as unavailable", err)
		}
		if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%v: %d, Retry-After %q; want 503 with Retry-After", err, rec.Code, rec.Header().Get("Retry-After"))
		}
	}
	for _, err := range []error{nil, errors.New("unsupported hash format")} {
		rec := httptest.NewRecorder()
		if passwordUnavailable(rec, err) {
			t.Fatalf("%v: treated as unavailable", err)
		}
	}
}
