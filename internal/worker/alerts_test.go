package worker_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type fakeSender struct {
	mu       sync.Mutex
	calls    []sentMsg
	failNext int
}

type sentMsg struct {
	token, chatID, text string
}

func (f *fakeSender) SendWith(_ context.Context, token, chatID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sentMsg{token, chatID, text})
	if f.failNext > 0 {
		f.failNext--
		return errors.New("telegram: bad gateway")
	}
	return nil
}

// failNextSends makes the next n SendWith calls fail, as a flaky uplink would.
func (f *fakeSender) failNextSends(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = n
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSender) last() sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return sentMsg{}
	}
	return f.calls[len(f.calls)-1]
}

func srcEnabled(token, chatID string) func(context.Context) (bool, string, string, error) {
	return func(context.Context) (bool, string, string, error) { return true, token, chatID, nil }
}

func srcDisabled() func(context.Context) (bool, string, string, error) {
	return func(context.Context) (bool, string, string, error) { return false, "", "", nil }
}

func testNode(name, hostname string) db.Node {
	return db.Node{ID: uuid.New(), Name: name, Hostname: hostname}
}

func TestAlertsRateLimitsPerNodeAndKind(t *testing.T) {
	sender := &fakeSender{}
	a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	node := testNode("n1", "n1.test")

	a.NodeOffline(t.Context(), node)
	a.NodeOffline(t.Context(), node)
	if sender.count() != 1 {
		t.Fatalf("expected 1 send within the rate-limit window, got %d", sender.count())
	}
	if !strings.Contains(sender.last().text, "offline") {
		t.Fatalf("text = %q", sender.last().text)
	}
	if sender.last().token != "tok" || sender.last().chatID != "42" {
		t.Fatalf("token/chatID not forwarded: %+v", sender.last())
	}
}

func TestAlertsIndependentRateLimitPerKind(t *testing.T) {
	sender := &fakeSender{}
	a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	node := testNode("n1", "n1.test")

	a.NodeOffline(t.Context(), node)
	a.NodeOnline(t.Context(), node)
	if sender.count() != 2 {
		t.Fatalf("expected 2 sends (different kinds must not share a rate-limit bucket), got %d", sender.count())
	}
}

func TestAlertsDisabledConfigSendsNothing(t *testing.T) {
	sender := &fakeSender{}
	a := worker.NewAlerts(srcDisabled(), sender, slog.New(slog.DiscardHandler))
	node := testNode("n1", "n1.test")

	a.NodeOffline(t.Context(), node)
	a.NodeOnline(t.Context(), node)
	a.ApplyFailed(t.Context(), node, "boom")
	if sender.count() != 0 {
		t.Fatalf("expected no sends when telegram_alerts.enabled is false, got %d", sender.count())
	}
}

func TestAlertsMissingTokenOrChatSendsNothing(t *testing.T) {
	sender := &fakeSender{}
	a := worker.NewAlerts(srcEnabled("", "42"), sender, slog.New(slog.DiscardHandler))
	a.NodeOffline(t.Context(), testNode("n1", "n1.test"))
	if sender.count() != 0 {
		t.Fatal("expected no send without a bot token")
	}

	a2 := worker.NewAlerts(srcEnabled("tok", ""), sender, slog.New(slog.DiscardHandler))
	a2.NodeOffline(t.Context(), testNode("n1", "n1.test"))
	if sender.count() != 0 {
		t.Fatal("expected no send without a chat id")
	}
}

func TestAlertsApplyFailedUsesFirstLineOnly(t *testing.T) {
	sender := &fakeSender{}
	a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	node := testNode("n1", "n1.test")

	a.ApplyFailed(t.Context(), node, "boom: first line\nstack trace\nmore detail")
	if sender.count() != 1 {
		t.Fatalf("expected 1 send, got %d", sender.count())
	}
	text := sender.last().text
	if !strings.Contains(text, "boom: first line") {
		t.Fatalf("text missing first line: %q", text)
	}
	if strings.Contains(text, "stack trace") || strings.Contains(text, "more detail") {
		t.Fatalf("text leaked lines past the first: %q", text)
	}
}

func TestAlertsEscapesNodeNameAndHostname(t *testing.T) {
	sender := &fakeSender{}
	a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	node := testNode("<b>n1</b>", "n1.test")

	a.NodeOffline(t.Context(), node)
	text := sender.last().text
	if strings.Contains(text, "<b>") {
		t.Fatalf("node name not HTML-escaped: %q", text)
	}
	if !strings.Contains(text, "&lt;b&gt;") {
		t.Fatalf("expected escaped name in text: %q", text)
	}
}

func TestAlertsNilPointerIsNoOp(t *testing.T) {
	var a *worker.Alerts
	node := testNode("n1", "n1.test")
	a.NodeOffline(t.Context(), node)
	a.NodeOnline(t.Context(), node)
	a.ApplyFailed(t.Context(), node, "boom")
}

func TestAlertsConfigErrorSendsNothing(t *testing.T) {
	sender := &fakeSender{}
	src := func(context.Context) (bool, string, string, error) {
		return false, "", "", errors.New("decrypt failed")
	}
	a := worker.NewAlerts(src, sender, slog.New(slog.DiscardHandler))
	a.NodeOffline(t.Context(), testNode("n1", "n1.test"))
	if sender.count() != 0 {
		t.Fatal("expected no send when the config source errors")
	}
}

func TestAlertsFailedSendDoesNotConsumeTheRateLimitSlot(t *testing.T) {
	sender := &fakeSender{}
	sender.failNextSends(1)
	a := worker.NewAlerts(srcEnabled("tok", "42"), sender, slog.New(slog.DiscardHandler))
	node := testNode("n1", "n1.test")

	a.NodeOffline(t.Context(), node)
	if sender.count() != 1 {
		t.Fatalf("expected the first (failing) attempt, got %d", sender.count())
	}
	// The retry must go out: nothing was actually delivered.
	a.NodeOffline(t.Context(), node)
	if sender.count() != 2 {
		t.Fatalf("a failed send consumed the rate-limit slot: %d attempts, want 2", sender.count())
	}
	// That one succeeded, so the window is now open and a third attempt is suppressed.
	a.NodeOffline(t.Context(), node)
	if sender.count() != 2 {
		t.Fatalf("successful send did not open the rate-limit window: %d attempts, want 2", sender.count())
	}
}
