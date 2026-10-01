package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/blocklist"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
)

const blocklistRetry = 10 * time.Minute

type blocklistAttempt struct {
	revision int64
	at       time.Time
}

// Blocklists sends a server its blocklist again when the server reports another one: it was
// offline when the list changed, or was reinstalled, or came back as a new server with old rules.
type Blocklists struct {
	st     *store.Store
	driver nodedriver.Driver
	log    *slog.Logger
	tried  map[uuid.UUID]blocklistAttempt
}

func NewBlocklists(st *store.Store, driver nodedriver.Driver, log *slog.Logger) *Blocklists {
	return &Blocklists{st: st, driver: driver, log: log, tried: map[uuid.UUID]blocklistAttempt{}}
}

func (b *Blocklists) RunOnce(ctx context.Context) error {
	d, ok := b.driver.(nodedriver.FirewallDriver)
	if !ok {
		return nil
	}
	rows, err := b.st.Q.ListBlocklistsToSync(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var h nodedriver.HealthReport
		if json.Unmarshal(row.LastHealth, &h) != nil || h.Firewall == nil {
			continue
		}
		if h.Firewall.Revision == row.Revision && (row.Revision > 0 || h.Firewall.Entries == 0) {
			continue
		}
		if last, ok := b.tried[row.NodeID]; ok && last.revision == row.Revision && time.Since(last.at) < blocklistRetry {
			continue
		}
		var entries []blocklist.Entry
		if err := json.Unmarshal(row.Entries, &entries); err != nil {
			b.log.Error("blocklist unreadable", "node", row.NodeID, "err", err)
			continue
		}
		prefixes := make([]string, len(entries))
		for i, e := range entries {
			prefixes[i] = e.Prefix
		}
		b.tried[row.NodeID] = blocklistAttempt{revision: row.Revision, at: time.Now()}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := d.Firewall(callCtx, row.NodeID, true, row.Revision, prefixes)
		cancel()
		if err != nil {
			b.log.Warn("blocklist not delivered", "node", row.NodeID, "revision", row.Revision, "err", err)
			continue
		}
		b.log.Info("blocklist delivered", "node", row.NodeID, "revision", row.Revision, "entries", len(prefixes))
	}
	return nil
}

func (b *Blocklists) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := b.RunOnce(ctx); err != nil {
			b.log.Error("blocklists", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
