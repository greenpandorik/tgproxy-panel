package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/notify"
	"tgwebproxy/internal/store/db"
)

// Sender delivers a rendered alert message to a chat.
type Sender interface {
	SendWith(ctx context.Context, botToken, chatID, text string) error
}

const (
	offlineCooldown  = 15 * time.Minute
	applyCooldown    = time.Hour
	incidentCooldown = 3 * time.Hour
)

type Alerts struct {
	Webhook *notify.Webhook
	// Lang reports the notification language setting; nil means English.
	Lang     func(ctx context.Context) string
	PanelURL string
	src      func(ctx context.Context) (enabled bool, botToken, chatID string, err error)
	tg       Sender
	log      *slog.Logger

	mu     sync.Mutex
	sent   map[string]time.Time // key: "<nodeID>|<kind>"
	pushed map[string]bool      // problems whose message went out and whose recovery has not
}

// NewAlerts builds an Alerts notifier.
func NewAlerts(src func(context.Context) (bool, string, string, error), tg Sender, log *slog.Logger) *Alerts {
	return &Alerts{src: src, tg: tg, log: log, sent: map[string]time.Time{}, pushed: map[string]bool{}}
}

func (a *Alerts) allow(key string, cooldown time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	last, ok := a.sent[key]
	return !ok || time.Since(last) >= cooldown
}

func (a *Alerts) markPushed(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent[key] = time.Now()
	a.pushed[key] = true
}

func (a *Alerts) takePushed(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	was := a.pushed[key]
	delete(a.pushed, key)
	return was
}

func (a *Alerts) lang(ctx context.Context) alerttext.Lang {
	if a.Lang == nil {
		return alerttext.EN
	}
	return alerttext.ParseLang(a.Lang(ctx))
}

func textNode(n db.Node) alerttext.Node {
	return alerttext.Node{ID: n.ID.String(), Name: n.Name, Hostname: n.Hostname}
}

// problem sends a problem at most once per cooldown for the same node and kind.
func (a *Alerts) problem(ctx context.Context, nodeID, kind string, cooldown time.Duration, msg alerttext.Message) {
	key := nodeID + "|" + kind
	if !a.allow(key, cooldown) {
		return
	}
	if a.deliver(ctx, nodeID, kind, msg) {
		a.markPushed(key)
	}
}

// recovered sends a recovery only for a problem whose message went out.
func (a *Alerts) recovered(ctx context.Context, nodeID, kind string, msg alerttext.Message) {
	if !a.takePushed(nodeID + "|" + kind) {
		return
	}
	a.deliver(ctx, nodeID, kind+"_recovered", msg)
}

func (a *Alerts) deliver(ctx context.Context, nodeID, kind string, msg alerttext.Message) bool {
	delivered := false
	if a.Webhook != nil {
		if err := a.Webhook.Send(ctx, nodeID, kind, msg.Plain); err != nil {
			a.log.Warn("webhook delivery", "kind", kind, "err", err)
		} else {
			delivered = true
		}
	}
	enabled, token, chatID, err := a.src(ctx)
	if err != nil {
		a.log.Error("telegram config", "err", err)
		return delivered
	}
	if !enabled || token == "" || chatID == "" {
		return delivered
	}
	if err := a.tg.SendWith(ctx, token, chatID, msg.HTML); err != nil {
		a.log.Warn("telegram send failed", "err", err, "kind", kind)
		return delivered
	}
	return true
}

// NodeOffline notifies that node stopped sending heartbeats.
func (a *Alerts) NodeOffline(ctx context.Context, node db.Node) {
	if a == nil {
		return
	}
	a.problem(ctx, node.ID.String(), "node_offline", offlineCooldown, alerttext.Default().Offline(a.lang(ctx), textNode(node), a.PanelURL))
}

// NodeOnline notifies that a node whose outage was announced is back.
func (a *Alerts) NodeOnline(ctx context.Context, node db.Node) {
	if a == nil {
		return
	}
	a.recovered(ctx, node.ID.String(), "node_offline", alerttext.Default().Online(a.lang(ctx), textNode(node)))
}

// ApplyFailed notifies that an apply job failed on node.
func (a *Alerts) ApplyFailed(ctx context.Context, node db.Node, jobErr string) {
	if a == nil {
		return
	}
	a.problem(ctx, node.ID.String(), "apply_failed", applyCooldown, alerttext.Default().ApplyFailed(a.lang(ctx), textNode(node), jobErr, a.PanelURL))
}
