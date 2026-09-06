// Package admincli holds the panel's administrative command-line operations. They
// live here rather than in cmd/panel so they can be exercised against a real
// database by tests: these are exactly the operations nobody runs twice, so the
// only chance to find out they are wrong is a test.
package admincli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

// TOTPResetAction is the audit action recorded by ResetTOTP. It is namespaced with
// the other auth events so an operator filtering the audit log by `auth.` sees the
// reset next to the logins it explains.
const TOTPResetAction = "auth.totp_reset"

// TOTPResetIP is the value stored in audit_log.ip for a reset. There is no request
// and therefore no client address; a literal marker is more honest than an empty
// string, which would be indistinguishable from "we failed to record it".
const TOTPResetIP = "cli"

// ResetTOTP turns two-factor authentication off for one named admin and deletes
// their recovery codes. It is the lockout escape hatch: an admin who has lost both
// their authenticator and their codes can no longer finish a login, and every route
// that could disable the second factor sits behind the session they cannot get.
//
// The reset, the code deletion and the audit entry go in one transaction, so the
// panel can never end up with a second factor silently removed and no record of it.
// The audit row has a NULL admin_user_id because the actor is a shell on the host
// rather than a panel account - the row still names the target and the action, which
// is what an operator reading the log afterwards needs.
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
		// SetAdminTOTP clears totp_secret_enc and totp_pending_enc together, so a
		// half-finished enrolment cannot survive the reset either.
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
