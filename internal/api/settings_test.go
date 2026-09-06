package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
)

func TestSettingsTelegramBotTokenEncryptedAtRest(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	const plaintextToken = "123456:AA-SUPER-SECRET-BOT-TOKEN"

	resp := c.Put("/api/v1/settings", map[string]any{
		"telegram_alerts": map[string]any{"enabled": true, "bot_token": plaintextToken, "chat_id": "42"},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("put settings %d", resp.StatusCode)
	}

	var got struct {
		TelegramAlerts json.RawMessage `json:"telegram_alerts"`
	}
	c.JSON(c.Get("/api/v1/settings"), &got)
	if !strings.Contains(string(got.TelegramAlerts), `"bot_token_set":true`) {
		t.Fatalf("expected bot_token_set true: %s", got.TelegramAlerts)
	}
	if strings.Contains(string(got.TelegramAlerts), `"bot_token"`) {
		t.Fatalf("response must never contain a bot_token field: %s", got.TelegramAlerts)
	}

	raw, err := h.Store.Q.GetSetting(t.Context(), "telegram_alerts")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), plaintextToken) {
		t.Fatalf("plaintext bot token stored at rest: %s", raw)
	}

	resp = c.Put("/api/v1/settings", map[string]any{
		"telegram_alerts": map[string]any{"enabled": true, "bot_token": "", "chat_id": "42"},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("put settings (clear token) %d", resp.StatusCode)
	}
	c.JSON(c.Get("/api/v1/settings"), &got)
	if !strings.Contains(string(got.TelegramAlerts), `"bot_token_set":false`) {
		t.Fatalf("expected bot_token_set false after clearing: %s", got.TelegramAlerts)
	}
}

// fakeTGSender records every SendWith call so tests on the
// POST /settings/telegram/test endpoint can assert what would have been sent
// without making a real HTTP call to Telegram.
type fakeTGSender struct {
	mu    sync.Mutex
	calls []struct{ token, chatID, text string }
	err   error
}

func (f *fakeTGSender) SendWith(_ context.Context, token, chatID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct{ token, chatID, text string }{token, chatID, text})
	return f.err
}

func ownerHarnessWithSender(t *testing.T, sender *fakeTGSender) (*apitest.Harness, *apitest.Client) {
	t.Helper()
	h := apitest.New(t, func(d *api.Deps) { d.Notifier = sender })
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	return h, c
}

func TestTelegramTestEndpointSendsAndAuditsWithoutLeakingToken(t *testing.T) {
	sender := &fakeTGSender{}
	_, c := ownerHarnessWithSender(t, sender)

	resp := c.Put("/api/v1/settings", map[string]any{
		"telegram_alerts": map[string]any{"enabled": true, "bot_token": "111:AAA-STORED", "chat_id": "42"},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("put settings %d", resp.StatusCode)
	}

	var out struct {
		OK bool `json:"ok"`
	}
	resp = c.Post("/api/v1/settings/telegram/test", map[string]any{})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("test endpoint %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &out)
	if !out.OK {
		t.Fatal("expected ok:true")
	}
	if len(sender.calls) != 1 || sender.calls[0].token != "111:AAA-STORED" || sender.calls[0].chatID != "42" {
		t.Fatalf("sender calls = %+v", sender.calls)
	}
	if !strings.Contains(sender.calls[0].text, "Test message from") {
		t.Fatalf("text = %q", sender.calls[0].text)
	}

	var audit struct {
		Items []struct {
			Action string          `json:"action"`
			Meta   json.RawMessage `json:"meta"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/audit?action=settings.telegram_test"), &audit)
	if len(audit.Items) != 1 {
		t.Fatalf("audit entries = %+v", audit.Items)
	}
	if strings.Contains(string(audit.Items[0].Meta), "111:AAA-STORED") {
		t.Fatalf("audit meta leaked the bot token: %s", audit.Items[0].Meta)
	}
	if !strings.Contains(string(audit.Items[0].Meta), `"ok":true`) {
		t.Fatalf("audit meta missing ok:true: %s", audit.Items[0].Meta)
	}
}

func TestTelegramTestEndpointUsesBodyTokenOverride(t *testing.T) {
	sender := &fakeTGSender{}
	_, c := ownerHarnessWithSender(t, sender)
	c.Put("/api/v1/settings", map[string]any{
		"telegram_alerts": map[string]any{"enabled": true, "bot_token": "stored:tok", "chat_id": "42"},
	})

	resp := c.Post("/api/v1/settings/telegram/test", map[string]any{"bot_token": "override:tok"})
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(sender.calls) != 1 || sender.calls[0].token != "override:tok" {
		t.Fatalf("expected the body token to override the stored one: %+v", sender.calls)
	}
}

func TestTelegramTestEndpointReturns502WithAPIDescription(t *testing.T) {
	sender := &fakeTGSender{err: errors.New("telegram: chat not found")}
	_, c := ownerHarnessWithSender(t, sender)
	c.Put("/api/v1/settings", map[string]any{
		"telegram_alerts": map[string]any{"enabled": true, "bot_token": "111:AAA", "chat_id": "42"},
	})

	resp := c.Post("/api/v1/settings/telegram/test", map[string]any{})
	if resp.StatusCode != 502 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "chat not found") {
		t.Fatalf("body = %s", b)
	}
}

func TestTelegramTestEndpointRequiresConfig(t *testing.T) {
	sender := &fakeTGSender{}
	_, c := ownerHarnessWithSender(t, sender)

	resp := c.Post("/api/v1/settings/telegram/test", map[string]any{})
	if resp.StatusCode != 422 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(sender.calls) != 0 {
		t.Fatal("must not attempt to send without a configured token and chat id")
	}
}

// TestTelegramTestEndpointUsesBodyChatIDOverride: the settings form enables "Send test" off
// its own in-progress chat id, so a chat id typed but not yet saved has to reach the handler
// - it used to always read the stored one and reject a form that looked complete.
func TestTelegramTestEndpointUsesBodyChatIDOverride(t *testing.T) {
	sender := &fakeTGSender{}
	_, c := ownerHarnessWithSender(t, sender)

	// Nothing stored at all: the body alone must be enough to send.
	resp := c.Post("/api/v1/settings/telegram/test", map[string]any{"bot_token": "body:tok", "chat_id": "  777  "})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d %s", resp.StatusCode, b)
	}
	if len(sender.calls) != 1 || sender.calls[0].chatID != "777" || sender.calls[0].token != "body:tok" {
		t.Fatalf("sent %+v, want the trimmed body chat id and token", sender.calls)
	}

	// The override is for the send only; it must not be persisted.
	var got struct {
		TelegramAlerts json.RawMessage `json:"telegram_alerts"`
	}
	c.JSON(c.Get("/api/v1/settings"), &got)
	if strings.Contains(string(got.TelegramAlerts), "777") {
		t.Fatalf("chat id override was persisted: %s", got.TelegramAlerts)
	}

	// A body chat id with no token anywhere is still a 422, naming both fields it needs.
	resp = c.Post("/api/v1/settings/telegram/test", map[string]any{"chat_id": "777"})
	if resp.StatusCode != 422 {
		t.Fatalf("missing token: status = %d, want 422", resp.StatusCode)
	}
}

func TestTelegramTestEndpointForbiddenForNonOwner(t *testing.T) {
	sender := &fakeTGSender{}
	h, _ := ownerHarnessWithSender(t, sender)
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")

	resp := viewer.Post("/api/v1/settings/telegram/test", map[string]any{})
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("viewer expected 403, got %d %s", resp.StatusCode, b)
	}
	if len(sender.calls) != 0 {
		t.Fatal("viewer must not be able to trigger a send")
	}
}
