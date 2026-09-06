package worker

import (
	"context"
	"time"

	"github.com/google/uuid"
)

func ptrTime(t time.Time) *time.Time      { return &t }
func nullUUID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

// Start launches all workers until ctx is cancelled. The returned Stop blocks
// until the applies already in flight have finished (up to 30s); callers should
// invoke it after the HTTP and gRPC listeners have drained.
func Start(ctx context.Context, a *Apply, e *Expiry, s *Stats, b *Backup) func(context.Context) error {
	go a.Run(ctx)
	go e.Run(ctx, time.Minute)
	go s.Run(ctx, time.Minute)
	if b != nil {
		go b.Run(ctx, backupTick)
	}
	return a.Stop
}
