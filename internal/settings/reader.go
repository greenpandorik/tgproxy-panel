// Package settings reads panel settings stored as JSON values in the
// settings table, falling back to a caller-supplied default when a key is
// absent, invalid, or the read fails.
package settings

import (
	"context"
	"encoding/json"
	"time"

	"tgwebproxy/internal/store"
)

type Reader struct {
	st *store.Store
}

func New(st *store.Store) *Reader {
	return &Reader{st: st}
}

// Duration reads key as an integer-seconds JSON value and returns it as a
// time.Duration, falling back to def when the setting is missing, not a
// positive integer, or the read fails.
func (r *Reader) Duration(ctx context.Context, key string, def time.Duration) time.Duration {
	raw, err := r.st.Q.GetSetting(ctx, key)
	if err != nil {
		return def
	}
	var seconds int
	if err := json.Unmarshal(raw, &seconds); err != nil || seconds <= 0 {
		return def
	}
	return time.Duration(seconds) * time.Second
}
