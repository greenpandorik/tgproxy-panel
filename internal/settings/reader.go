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
