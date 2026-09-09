// Package admincli holds the panel's administrative command-line operations.
package admincli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

const TOTPResetAction = "auth.totp_reset"

// TOTPResetIP is the value stored in audit_log.ip for a reset.
const TOTPResetIP = "cli"

// ResetTOTP turns two-factor authentication off for one named admin and deletes their recovery codes.
func ResetTOTP(ctx context.Context, st *store.Store, username string) (db.AdminUser, error) {
	u, err := st.Q.GetAdminByUsername(ctx, username)
	if err != nil {
		return db.AdminUser{}, fmt.Errorf("no admin named %q", username)
	}
	meta, err := json.Marshal(map[string]any{"username": u.Username})
	if err != nil {
		return db.AdminUser{}, err
	}
	err = st.Tx(ctx, func(q *db.Queries) error {
		if err := q.SetAdminTOTP(ctx, db.SetAdminTOTPParams{ID: u.ID, TotpSecretEnc: nil, TotpEnabled: false}); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, u.ID); err != nil {
			return err
		}
		return q.InsertAudit(ctx, db.InsertAuditParams{
			AdminUserID: uuid.NullUUID{},
			Action:      TOTPResetAction,
			TargetType:  "admin",
			TargetID:    u.ID.String(),
			Meta:        meta,
			Ip:          TOTPResetIP,
		})
	})
	if err != nil {
		return db.AdminUser{}, err
	}
	return u, nil
}
