package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/store/db"
)

type quotaResetQuerier interface {
	ListTelemtNodesWithQuotaPeriod(ctx context.Context, period string) ([]uuid.UUID, error)
	SetNodeDirty(ctx context.Context, arg db.SetNodeDirtyParams) error
}

// QuotaReset marks a node dirty when a quota period of one of its keys starts, so the next apply
// carries the new period and the agent resets the consumed quota. The first pass after the panel
// starts marks every such node: a boundary that passed while the panel was down is caught then,
// and an agent already in the current period treats the apply as a no-op.
type QuotaReset struct {
	q     quotaResetQuerier
	log   *slog.Logger
	now   func() time.Time
	start map[domain.QuotaPeriod]time.Time
}

func NewQuotaReset(q quotaResetQuerier, log *slog.Logger) *QuotaReset {
	return &QuotaReset{q: q, log: log, now: time.Now, start: map[domain.QuotaPeriod]time.Time{}}
}

// RunOnce returns how many nodes it marked dirty.
func (r *QuotaReset) RunOnce(ctx context.Context) (int, error) {
	now := r.now()
	marked := 0
	for _, p := range []domain.QuotaPeriod{domain.QuotaPeriodWeek, domain.QuotaPeriodMonth} {
		start := p.Start(now)
		if prev, ok := r.start[p]; ok && prev.Equal(start) {
			continue
		}
		nodes, err := r.q.ListTelemtNodesWithQuotaPeriod(ctx, string(p))
		if err != nil {
			return marked, err
		}
		for _, id := range nodes {
			if err := r.q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: id, Dirty: true}); err != nil {
				return marked, err
			}
			marked++
		}
		r.start[p] = start
		if len(nodes) > 0 {
			r.log.Info("quota period started", "period", string(p), "start", start, "nodes", len(nodes))
		}
	}
	return marked, nil
}

func (r *QuotaReset) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := r.RunOnce(ctx); err != nil {
			r.log.Error("quota reset", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
