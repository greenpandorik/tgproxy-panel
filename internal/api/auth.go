package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(ipFrom(r.Context())) {
		writeError(w, 429, "rate_limited", "too many attempts, try later", nil)
		return
	}
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	u, err := s.store.Q.GetAdminByUsername(r.Context(), req.Username)
	if err != nil {
		// burn time to keep timing similar
		_, _ = crypto.VerifyPassword(req.Password, "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		writeError(w, 401, "invalid_credentials", "wrong username or password", nil)
		return
	}
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		writeError(w, 423, "locked", "account temporarily locked", nil)
		return
	}
	ok, _ := crypto.VerifyPassword(req.Password, u.PasswordHash)
	if !ok {
		_ = s.store.Q.RecordFailedLogin(r.Context(), u.ID)
		writeError(w, 401, "invalid_credentials", "wrong username or password", nil)
		return
	}
	if s.cfg.FeatureTOTP && u.TotpEnabled {
		writeJSON(w, 200, map[string]any{"totp_required": true, "challenge": s.newTOTPChallenge(u.ID, time.Now())})
		return
	}
	_ = s.store.Q.ResetFailedLogins(r.Context(), u.ID)
	if err := s.issueSession(w, r, u.ID); err != nil {
		s.log.Error("issue session", "err", err)
		internal(w)
		return
	}
	ctx := r.Context()
	s.Audit(withPrincipal(ctx, Principal{UserID: u.ID, Username: u.Username, Role: string(u.Role)}), "auth.login", "admin", u.ID.String(), nil)
	writeJSON(w, 200, s.meJSON(u.ID, u.Username, string(u.Role), u.TotpEnabled))
}

// meJSON is the identity payload.
func (s *Server) meJSON(id uuid.UUID, username, role string, totpEnabled bool) map[string]any {
	return map[string]any{
		"id": id, "username": username, "role": role, "totp_enabled": totpEnabled,
		"features": map[string]bool{"totp": s.cfg.FeatureTOTP},
	}
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	u, err := s.store.Q.GetAdmin(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, s.meJSON(p.UserID, p.Username, p.Role, u.TotpEnabled))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	_ = s.store.Q.DeleteSession(r.Context(), p.SessionID)
	s.clearSession(w)
	w.WriteHeader(204)
}

type changePasswordReq struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req changePasswordReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(req.New) < 10 {
		validation(w, map[string]string{"new": "at least 10 characters"})
		return
	}
	u, err := s.store.Q.GetAdmin(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	if ok, _ := crypto.VerifyPassword(req.Current, u.PasswordHash); !ok {
		validation(w, map[string]string{"current": "wrong password"})
		return
	}
	hash, err := crypto.HashPassword(req.New)
	if err != nil {
		internal(w)
		return
	}
	if err := s.store.Q.UpdateAdminPassword(r.Context(), db.UpdateAdminPasswordParams{ID: p.UserID, PasswordHash: hash}); err != nil {
		internal(w)
		return
	}
	_ = s.store.Q.DeleteUserSessions(r.Context(), p.UserID)
	if err := s.issueSession(w, r, p.UserID); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "auth.password_changed", "admin", p.UserID.String(), nil)
	w.WriteHeader(204)
}

type createAdminReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (s *Server) handleListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListAdmins(r.Context())
	if err != nil {
		internal(w)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, u := range rows {
		out = append(out, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "totp_enabled": u.TotpEnabled, "created_at": u.CreatedAt})
	}
	writeJSON(w, 200, map[string]any{"items": out, "total": len(out)})
}

func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req createAdminReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	fields := map[string]string{}
	if len(req.Username) < 2 {
		fields["username"] = "at least 2 characters"
	}
	if len(req.Password) < 10 {
		fields["password"] = "at least 10 characters"
	}
	if req.Role != RoleOwner && req.Role != RoleAdmin && req.Role != RoleViewer {
		fields["role"] = "owner, admin or viewer"
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		internal(w)
		return
	}
	u, err := s.store.Q.CreateAdmin(r.Context(), db.CreateAdminParams{Username: req.Username, PasswordHash: hash, Role: db.AdminRole(req.Role)})
	if err != nil {
		conflict(w, "username already exists")
		return
	}
	s.Audit(r.Context(), "admin.create", "admin", u.ID.String(), map[string]any{"username": u.Username, "role": u.Role})
	writeJSON(w, 201, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role})
}

func (s *Server) handleDeleteAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return
	}
	target, err := s.store.Q.GetAdmin(r.Context(), id)
	if err != nil {
		notFound(w)
		return
	}
	p, _ := PrincipalFrom(r.Context())
	if target.Role == db.AdminRoleOwner {
		lastOwner := false
		err := s.store.Tx(r.Context(), func(q *db.Queries) error {
			owners, err := q.CountOwnersForUpdate(r.Context())
			if err != nil {
				return err
			}
			if owners <= 1 {
				lastOwner = true
				return nil
			}
			if p.UserID == id {
				return nil // handled below, outside the transaction
			}
			return q.DeleteAdmin(r.Context(), id)
		})
		if err != nil {
			internal(w)
			return
		}
		if lastOwner {
			conflict(w, "cannot delete the last owner")
			return
		}
		if p.UserID == id {
			conflict(w, "cannot delete yourself")
			return
		}
		s.Audit(r.Context(), "admin.delete", "admin", id.String(), nil)
		w.WriteHeader(204)
		return
	}
	if p.UserID == id {
		conflict(w, "cannot delete yourself")
		return
	}
	if err := s.store.Q.DeleteAdmin(r.Context(), id); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "admin.delete", "admin", id.String(), nil)
	w.WriteHeader(204)
}
