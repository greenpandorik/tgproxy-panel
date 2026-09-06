package worker

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"sync"
	"time"

	"tgwebproxy/internal/store/db"
)

// Sender delivers a rendered alert message to a chat. *notify.Telegram
// satisfies this through its SendWith method; kept as a narrow interface here
// so tests can inject a fake without a real HTTP round trip.
type Sender interface {
	SendWith(ctx context.Context, botToken, chatID, text string) error
}

// alertRateLimit caps how often the same (node, kind) pair may fire: a
// flapping node or a repeatedly failing apply notifies at most once per
// window rather than spamming the chat.
const alertRateLimit = 5 * time.Minute

// Alerts renders and rate-limits Telegram notifications for node offline/online
// transitions and apply failures. A nil *Alerts is safe to call methods on
// (every method is a no-op), so Stats and Apply can hold the pointer
// unconditionally and only get real notifications once main.go wires one up.
type Alerts struct {
	// src reports the current Telegram configuration on every call, so a
	// settings change takes effect without restarting the worker.
	src func(ctx context.Context) (enabled bool, botToken, chatID string, err error)
	tg  Sender
	log *slog.Logger

	mu   sync.Mutex
	sent map[string]time.Time // key: "<nodeID>|<kind>"
}

// NewAlerts builds an Alerts notifier. src is consulted on every send attempt;
// tg performs the actual delivery.
func NewAlerts(src func(context.Context) (bool, string, string, error), tg Sender, log *slog.Logger) *Alerts {
	return &Alerts{src: src, tg: tg, log: log, sent: map[string]time.Time{}}
}

// allow reports whether key is outside its rate-limit window. It does not record
// anything: the slot is only claimed once a send has actually succeeded, by markSent.
func (a *Alerts) allow(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	last, ok := a.sent[key]
	return !ok || time.Since(last) >= alertRateLimit
}

// markSent opens the rate-limit window for key. Claiming the slot before the send meant a
// transient Telegram failure - a DNS blip, a 502 from the API - silently suppressed that
// alert for the next five minutes, which is exactly the window in which an operator most
// needs to hear that a node went offline.
func (a *Alerts) markSent(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent[key] = time.Now()
}

func (a *Alerts) send(ctx context.Context, nodeID, kind, text string) {
	enabled, token, chatID, err := a.src(ctx)
	if err != nil {
		a.log.Error("telegram config", "err", err)
		return
	}
	if !enabled || token == "" || chatID == "" {
		return
	}
	key := nodeID + "|" + kind
	if !a.allow(key) {
		return
	}
	if err := a.tg.SendWith(ctx, token, chatID, text); err != nil {
		a.log.Warn("telegram send failed", "err", err, "kind", kind)
		return
	}
	a.markSent(key)
}

// NodeOffline notifies that node stopped sending heartbeats.
func (a *Alerts) NodeOffline(ctx context.Context, node db.Node) {
	if a == nil {
		return
	}
	text := fmt.Sprintf("⚠️ Node %s (%s) is offline", html.EscapeString(node.Name), html.EscapeString(node.Hostname))
	a.send(ctx, node.ID.String(), "node_offline", text)
}

// NodeOnline notifies that a previously offline node is back.
func (a *Alerts) NodeOnline(ctx context.Context, node db.Node) {
	if a == nil {
		return
	}
	text := fmt.Sprintf("✅ Node %s (%s) is back online", html.EscapeString(node.Name), html.EscapeString(node.Hostname))
	a.send(ctx, node.ID.String(), "node_online", text)
}

// ApplyFailed notifies that an apply job failed on node. Only the first line
// of jobErr is included in the message; the full error is kept in the apply
// job row and the alerts table.
func (a *Alerts) ApplyFailed(ctx context.Context, node db.Node, jobErr string) {
	if a == nil {
		return
	}
	firstLine, _, _ := strings.Cut(jobErr, "\n")
	text := fmt.Sprintf("❌ Apply failed on %s: %s", html.EscapeString(node.Name), html.EscapeString(firstLine))
	a.send(ctx, node.ID.String(), "apply_failed", text)
}
