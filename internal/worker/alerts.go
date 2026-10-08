package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	mu            sync.Mutex
	pool          *pgxpool.Pool
	states        map[string]*notificationState
	locks         map[string]*sync.Mutex
	deliverySlots chan struct{}
	now           func() time.Time
}

// NewAlerts builds an Alerts notifier.
func NewAlerts(src func(context.Context) (bool, string, string, error), tg Sender, log *slog.Logger) *Alerts {
	return &Alerts{src: src, tg: tg, log: log, states: map[string]*notificationState{}, locks: map[string]*sync.Mutex{}, deliverySlots: make(chan struct{}, 2)}
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
	a.withState(ctx, nodeID, kind, func(state *notificationState) {
		if !state.Pending && !state.LastProblem.IsZero() && time.Since(state.LastProblem) < cooldown {
			return
		}
		// Apply/legacy incident reminders start a new delivery period after their
		// cooldown. An offline outage keeps one announcement until its recovery.
		if !state.Pending && kind != "node_offline" {
			state.Telegram = false
			state.Webhook = false
		}
		state.Pending = true
		a.sendProblem(ctx, nodeID, kind, msg, state)
	})
}

// recovered retains each successful problem destination until its recovery succeeds.
func (a *Alerts) recovered(ctx context.Context, nodeID, kind string, msg alerttext.Message) {
	a.withState(ctx, nodeID, kind, func(state *notificationState) {
		state.Pending = false
		a.sendRecovery(ctx, nodeID, kind, msg, state)
	})
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
