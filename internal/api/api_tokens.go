package api

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

const (
	maxAPITokens    = 50
	maxAPITokenDays = 365
)

var (
	errAPITokenLimit            = errors.New("API token limit reached")
	errAPITokenSessionExpired   = errors.New("API token issuing session expired")
	errAPITokenScopeUnavailable = errors.New("API token scope unavailable")
)

type apiTokenScope struct {
	ID       string `json:"id"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

func apiTokenScopes(role string) []apiTokenScope {
	out := make([]apiTokenScope, 0, 13)
	for _, resource := range []string{"nodes", "users", "monitoring", "sites", "branding", "settings", "audit"} {
		for _, action := range []string{"read", "write"} {
			if resource == "audit" && action == "write" {
				continue
			}
			id := resource + ":" + action
			if !tokenScopeAllowed(role, id) || (role == RoleViewer && resource == "branding") || (role != RoleOwner && id == "settings:write") {
				continue
			}
			out = append(out, apiTokenScope{ID: id, Resource: resource, Action: action})
		}
	}
	return out
}

type apiTokenMetadata struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

func (s *Server) handleAPITokenScopes(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	writeJSON(w, 200, map[string]any{"scopes": apiTokenScopes(p.Role), "max_expires_in_days": maxAPITokenDays, "max_tokens": maxAPITokens})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	rows, err := s.store.Q.ListAPITokens(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": rows, "total": len(rows)})
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req struct {
		Name          string   `json:"name"`
		ExpiresInDays int      `json:"expires_in_days"`
		Scopes        []string `json:"scopes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	fields := map[string]string{}
	if !utf8.ValidString(req.Name) || utf8.RuneCountInString(req.Name) < 1 || utf8.RuneCountInString(req.Name) > 80 {
		fields["name"] = "1–80 characters"
	}
	if req.ExpiresInDays < 1 || req.ExpiresInDays > maxAPITokenDays {
		fields["expires_in_days"] = "1–365 days"
	}
	if len(req.Scopes) == 0 {
		fields["scopes"] = "at least one scope is required"
	}
	normalized := make([]string, 0, len(req.Scopes))
	for _, scope := range req.Scopes {
		if !slices.Contains(normalized, scope) {
			normalized = append(normalized, scope)
		}
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	secret, err := crypto.NewToken(32)
	if err != nil {
		internal(w)
		return
	}
	secret = "tgwp_" + secret
	hash := sha256.Sum256([]byte(secret))
	var token db.ApiToken
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		if _, err := q.LockAPITokenOwner(r.Context(), p.UserID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errAPITokenSessionExpired
			}
			return err
		}
		// Rotation holds this same account lock while deleting old sessions.
		// Authentication before the lock therefore cannot authorize issuance after rotation.
		session, err := q.GetSession(r.Context(), p.SessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errAPITokenSessionExpired
		}
		if err != nil {
			return err
		}
		if session.AdminUserID != p.UserID || !session.ExpiresAt.After(time.Now()) {
			return errAPITokenSessionExpired
		}
		available := map[string]bool{}
		for _, scope := range apiTokenScopes(string(session.Role)) {
			available[scope.ID] = true
		}
		for _, scope := range normalized {
			if !available[scope] {
				return errAPITokenScopeUnavailable
			}
		}
		count, err := q.CountActiveAPITokens(r.Context(), p.UserID)
		if err != nil {
			return err
		}
		if count >= maxAPITokens {
			return errAPITokenLimit
		}
		token, err = q.CreateAPIToken(r.Context(), db.CreateAPITokenParams{AdminUserID: p.UserID, Name: req.Name, Prefix: secret[:13], TokenHash: hash[:], Scopes: normalized, ExpiresAt: time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)})
		return err
	})
	if errors.Is(err, errAPITokenSessionExpired) {
		unauthorized(w)
		return
	}
	if errors.Is(err, errAPITokenScopeUnavailable) {
		validation(w, map[string]string{"scopes": "unknown or unavailable scope"})
		return
	}
	if errors.Is(err, errAPITokenLimit) {
		conflict(w, "maximum 50 active API tokens")
		return
	}
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "api_token.create", "api_token", token.ID.String(), map[string]any{"name": token.Name, "scopes": token.Scopes})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{"token": secret, "api_token": apiTokenMetadata{ID: token.ID, Name: token.Name, Prefix: token.Prefix, Scopes: token.Scopes, CreatedAt: token.CreatedAt, ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt, RevokedAt: token.RevokedAt}})
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return
	}
	p, _ := PrincipalFrom(r.Context())
	count, err := s.store.Q.RevokeAPIToken(r.Context(), db.RevokeAPITokenParams{ID: id, AdminUserID: p.UserID})
	if err != nil {
		internal(w)
		return
	}
	if count == 0 {
		notFound(w)
		return
	}
	s.Audit(r.Context(), "api_token.revoke", "api_token", id.String(), nil)
	w.WriteHeader(204)
}

// authenticateManagement gives an Authorization header priority over any cookie.
// Only management routes use this middleware: dedicated service/agent credentials
// retain their independent authentication.
func (s *Server) authenticateManagement(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values, present := r.Header["Authorization"]
		if !present {
			requireAuth(next).ServeHTTP(w, r)
			return
		}
		if len(values) != 1 {
			unauthorized(w)
			return
		}
		parts := strings.Split(values[0], " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "tgwp_") {
			unauthorized(w)
			return
		}
		bytes, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(parts[1], "tgwp_"))
		if err != nil || len(bytes) != 32 {
			unauthorized(w)
			return
		}
		hash := sha256.Sum256([]byte(parts[1]))
		token, err := s.store.Q.GetActiveAPIToken(r.Context(), hash[:])
		if err != nil {
			unauthorized(w)
			return
		}
		p := Principal{UserID: token.AdminUserID, Username: token.Username, Role: string(token.Role), APITokenID: token.ID, APITokenScopes: token.Scopes}
		_ = s.store.Q.TouchAPIToken(r.Context(), token.ID)
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}
