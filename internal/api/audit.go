package api

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

// Audit records an admin action. meta must not contain secrets.
func (s *Server) Audit(ctx context.Context, action, targetType, targetID string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	if p, ok := PrincipalFrom(ctx); ok && p.APITokenID != uuid.Nil {
		copied := make(map[string]any, len(meta)+1)
		for k, v := range meta {
			copied[k] = v
		}
		copied["api_token_id"] = p.APITokenID.String()
		meta = copied
	}
	raw, _ := json.Marshal(meta)
	var uid uuid.NullUUID
	if p, ok := PrincipalFrom(ctx); ok {
		uid = uuid.NullUUID{UUID: p.UserID, Valid: true}
	}
	if err := s.store.Q.InsertAudit(ctx, db.InsertAuditParams{
		AdminUserID: uid, Action: action, TargetType: targetType, TargetID: targetID, Meta: raw, Ip: ipFrom(ctx),
	}); err != nil {
		s.log.Error("audit insert failed", "err", err, "action", action)
	}
}
