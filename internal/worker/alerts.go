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

// Sender delivers a rendered alert message to a chat.
type Sender interface {
	SendWith(ctx context.Context, botToken, chatID, text string) error
}

const alertRateLimit = 5 * time.Minute

type Alerts struct {
	src func(ctx context.Context) (enabled bool, botToken, chatID string, err error)
	tg  Sender
	log *slog.Logger

	mu   sync.Mutex
	sent map[string]time.Time // key: "<nodeID>|<kind>"
}

// NewAlerts builds an Alerts notifier.
func NewAlerts(src func(context.Context) (bool, string, string, error), tg Sender, log *slog.Logger) *Alerts {
	return &Alerts{src: src, tg: tg, log: log, sent: map[string]time.Time{}}
}

// allow reports whether key is outside its rate-limit window.
func (a *Alerts) allow(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	last, ok := a.sent[key]
	return !ok || time.Since(last) >= alertRateLimit
}

// markSent opens the rate-limit window for key.
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

// ApplyFailed notifies that an apply job failed on node.
func (a *Alerts) ApplyFailed(ctx context.Context, node db.Node, jobErr string) {
	if a == nil {
		return
	}
	firstLine, _, _ := strings.Cut(jobErr, "\n")
	text := fmt.Sprintf("❌ Apply failed on %s: %s", html.EscapeString(node.Name), html.EscapeString(firstLine))
	a.send(ctx, node.ID.String(), "apply_failed", text)
}
