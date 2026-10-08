package worker

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"tgwebproxy/internal/alerttext"
)

// Destination acknowledgements are retained independently: retrying Telegram must
// not resend a successfully delivered webhook, and vice versa.
type notificationState struct {
	LastProblem time.Time
	Pending     bool
	Telegram    bool
	Webhook     bool
}

func (a *Alerts) withState(ctx context.Context, nodeID, kind string, fn func(*notificationState)) {
	a.mu.Lock()
	key := nodeID + "|" + kind
	lock := a.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		a.locks[key] = lock
	}
	a.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	if a.pool == nil {
		a.mu.Lock()
		state := a.states[key]
		if state == nil {
			state = &notificationState{}
			a.states[key] = state
		}
		a.mu.Unlock()
		fn(state)
		return
	}
	// Reserve DB connections for observations while external delivery is slow.
	select {
	case a.deliverySlots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-a.deliverySlots }()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		a.log.Error("notification state begin", "err", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO notification_states(node_id,kind) VALUES($1,$2) ON CONFLICT DO NOTHING`, nodeID, kind); err != nil {
		a.log.Error("notification state insert", "err", err)
		return
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT state FROM notification_states WHERE node_id=$1 AND kind=$2 FOR UPDATE`, nodeID, kind).Scan(&raw); err != nil {
		a.log.Error("notification state lock", "err", err)
		return
	}
	var state notificationState
	if err = json.Unmarshal(raw, &state); err != nil {
		a.log.Error("notification state decode", "err", err)
		return
	}
	fn(&state)
	raw, err = json.Marshal(state)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE notification_states SET state=$3 WHERE node_id=$1 AND kind=$2`, nodeID, kind, raw)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil && err != pgx.ErrTxClosed {
		a.log.Error("notification state save", "err", err)
	}
}

func (a *Alerts) sendProblem(ctx context.Context, nodeID, kind string, msg alerttext.Message, state *notificationState) {
	complete := true
	if a.Webhook != nil && a.Webhook.URL != "" && !state.Webhook {
		if err := a.Webhook.Send(ctx, nodeID, kind, msg.Plain); err != nil {
			a.log.Warn("webhook delivery", "kind", kind, "err", err)
			complete = false
		} else {
			state.Webhook = true
		}
	}
	enabled, token, chatID, err := a.src(ctx)
	if err != nil {
		a.log.Error("telegram config", "err", err)
		complete = false
	}
	if enabled && token != "" && chatID != "" && !state.Telegram {
		if err := a.tg.SendWith(ctx, token, chatID, msg.HTML); err != nil {
			a.log.Warn("telegram send failed", "kind", kind, "err", err)
			complete = false
		} else {
			state.Telegram = true
		}
	}
	if complete && (state.Telegram || state.Webhook) {
		state.Pending = false
		state.LastProblem = time.Now()
	}
}

func (a *Alerts) sendRecovery(ctx context.Context, nodeID, kind string, msg alerttext.Message, state *notificationState) {
	if state.Webhook && a.Webhook != nil {
		if err := a.Webhook.Send(ctx, nodeID, kind+"_recovered", msg.Plain); err != nil {
			a.log.Warn("webhook recovery", "kind", kind, "err", err)
		} else {
			state.Webhook = false
		}
	}
	if state.Telegram {
		enabled, token, chatID, err := a.src(ctx)
		if err != nil {
			a.log.Error("telegram config", "err", err)
			return
		}
		if enabled && token != "" && chatID != "" {
			if err := a.tg.SendWith(ctx, token, chatID, msg.HTML); err != nil {
				a.log.Warn("telegram recovery failed", "kind", kind, "err", err)
			} else {
				state.Telegram = false
			}
		}
	}
}
