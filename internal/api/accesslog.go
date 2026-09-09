package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// quietPaths are polled constantly by Docker and Prometheus and say nothing about who is
// using the panel.
var quietPaths = map[string]bool{"/healthz": true, "/metrics": true}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// accessLog records one line per request. It logs chi's route pattern rather than the URL:
// /s/{token} and the install routes carry a secret in the path, and an access log is exactly
// the place those must not end up. A served request is Debug, since the SPA polls; anything
// the panel refused or failed is Warn or Error, so a scan or a broken deploy is visible at
// the default level.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if quietPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		args := []any{
			"method", r.Method,
			"route", route,
			"status", rec.status,
			"bytes", rec.bytes,
			"ms", time.Since(start).Milliseconds(),
			"ip", ipFrom(r.Context()),
			"request_id", middleware.GetReqID(r.Context()),
		}
		if p, ok := PrincipalFrom(r.Context()); ok {
			args = append(args, "user", p.Username)
		}
		switch {
		case rec.status >= 500:
			s.log.Error("request", args...)
		case rec.status >= 400:
			s.log.Warn("request", args...)
		default:
			s.log.Debug("request", args...)
		}
	})
}
