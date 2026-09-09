package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

const (
	sessionCookie = "tgwp_session"
	csrfCookie    = "tgwp_csrf"
	sessionTTL    = 24 * time.Hour
)

type Principal struct {
	UserID    uuid.UUID
	Username  string
	Role      string
	SessionID string
}

type ctxKey int

const (
	ctxPrincipal ctxKey = iota
	ctxIP
)

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxPrincipal).(Principal)
	return p, ok
}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxPrincipal, p)
}

func ipFrom(ctx context.Context) string {
	s, _ := ctx.Value(ctxIP).(string)
	return s
}

func clientIP(r *http.Request) string {
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		last := values[len(values)-1]
		if i := strings.LastIndex(last, ","); i >= 0 {
			last = last[i+1:]
		}
		if last = strings.TrimSpace(last); last != "" {
			// Some proxies write "ip:port" for IPv6-mapped peers; keep only the host.
			if host, _, err := net.SplitHostPort(last); err == nil {
				last = host
			}
			if net.ParseIP(last) != nil {
				return last
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// withIP stores the client IP in the context for audit and rate limiting.
func withIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxIP, clientIP(r))))
	})
}

// loadSession attaches a Principal when a valid signed session cookie is present. It never rejects.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		id, ok := s.signer.Verify(c.Value)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		row, err := s.store.Q.GetSession(r.Context(), id)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if time.Until(row.ExpiresAt) < sessionTTL/2 {
			_ = s.store.Q.TouchSession(r.Context(), db.TouchSessionParams{ID: id, ExpiresAt: time.Now().Add(sessionTTL)})
		}
		p := Principal{UserID: row.AdminUserID, Username: row.Username, Role: string(row.Role), SessionID: id}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
	})
}

func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom(r.Context()); !ok {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	id, err := crypto.NewToken(32)
	if err != nil {
		return err
	}
	if err := s.store.Q.CreateSession(r.Context(), db.CreateSessionParams{
		ID: id, AdminUserID: userID, ExpiresAt: time.Now().Add(sessionTTL),
		Ip: ipFrom(r.Context()), UserAgent: r.UserAgent(),
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.signer.Sign(id), Path: "/", HttpOnly: true,
		Secure: s.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	csrf, _ := crypto.NewToken(32)
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: csrf, Path: "/", HttpOnly: false,
		Secure: s.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

// clearSession expires both cookies.
func (s *Server) clearSession(w http.ResponseWriter) {
	for _, n := range []string{sessionCookie, csrfCookie} {
		http.SetCookie(w, &http.Cookie{
			Name: n, Value: "", Path: "/", MaxAge: -1, HttpOnly: n == sessionCookie,
			Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
		})
	}
}
