package worker

import (
	"context"
	"log/slog"
	"time"

	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store"
)

type Expiry struct {
	st   *store.Store
	keys *keys.Service
	log  *slog.Logger
}

func NewExpiry(st *store.Store, k *keys.Service, log *slog.Logger) *Expiry {
	return &Expiry{st: st, keys: k, log: log}
}

// RunOnce revokes every access key whose expires_at has passed and returns how many were revoked.
func (e *Expiry) RunOnce(ctx context.Context) (int, error) {
	rows, err := e.st.Q.ListExpiredActiveKeys(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range rows {
		if err := e.keys.Revoke(ctx, k.ID); err != nil {
			e.log.Error("expire key", "key", k.ID, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

func (e *Expiry) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := e.RunOnce(ctx); err != nil {
			e.log.Error("expiry", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
