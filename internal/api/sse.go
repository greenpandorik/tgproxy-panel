package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f, _ := w.(http.Flusher)
	if f != nil {
		f.Flush()
	}
	return &sseWriter{w: w, f: f}
}

func (s *sseWriter) send(event string, v any) error {
	b, _ := json.Marshal(v)
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	if s.f != nil {
		s.f.Flush()
	}
	return nil
}
