package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookSignsExactBodyAndStopsOnPermanentFailure(t *testing.T) {
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		raw, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(r.Header.Get("X-TGWP-Timestamp") + "."))
		mac.Write(raw)
		if r.Header.Get("X-TGWP-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("invalid signature")
		}
		w.WriteHeader(400)
	}))
	defer s.Close()
	hook := Webhook{URL: s.URL, Secret: "secret"}
	if hook.Send(context.Background(), "node", "incident", "failure") == nil || count != 1 {
		t.Fatalf("permanent failure count=%d", count)
	}
}
