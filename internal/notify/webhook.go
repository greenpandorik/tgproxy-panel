package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Webhook delivers independently of Telegram, with a timestamped HMAC and bounded retries.
// The receiver should deduplicate event_id and reject timestamps older than five minutes.
type Webhook struct {
	URL    string
	Secret string
	Client *http.Client
}

func (w *Webhook) Send(ctx context.Context, node, kind, message string) error {
	if w == nil || w.URL == "" {
		return nil
	}
	now := time.Now().UTC()
	id := sha256.Sum256([]byte(node + kind + now.Format(time.RFC3339Nano)))
	body, _ := json.Marshal(map[string]any{"event_id": hex.EncodeToString(id[:]), "node_id": node, "kind": kind, "message": message, "at": now})
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(w.Secret))
		mac.Write([]byte(stamp + "."))
		mac.Write(body)
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
		if e != nil {
			return errors.New("invalid webhook endpoint")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TGWP-Timestamp", stamp)
		req.Header.Set("X-TGWP-Signature", hex.EncodeToString(mac.Sum(nil)))
		resp, e := client.Do(req)
		if e == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			last = fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
			if resp.StatusCode < 500 && resp.StatusCode != 429 {
				return last
			}
		} else {
			last = errors.New("webhook request failed")
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return last
}
