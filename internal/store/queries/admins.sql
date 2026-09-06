-- name: CreateAdmin :one
INSERT INTO admin_users (username, password_hash, role) VALUES ($1, $2, $3) RETURNING *;

-- name: GetAdminByUsername :one
SELECT * FROM admin_users WHERE username = $1;

-- name: GetAdmin :one
SELECT * FROM admin_users WHERE id = $1;

-- name: ListAdmins :many
SELECT * FROM admin_users ORDER BY created_at;

-- name: CountAdmins :one
SELECT count(*) FROM admin_users;

-- CountOwnersForUpdate locks every owner row for the duration of the transaction, so two
-- concurrent "delete an owner" requests serialise instead of both seeing two owners and
-- both succeeding.
-- name: CountOwnersForUpdate :one
SELECT count(*) FROM (SELECT 1 FROM admin_users WHERE role = 'owner' FOR UPDATE) t;

-- name: UpdateAdminPassword :exec
UPDATE admin_users SET password_hash = $2 WHERE id = $1;

-- name: UpdateAdminRole :exec
UPDATE admin_users SET role = $2 WHERE id = $1;

-- name: RecordFailedLogin :exec
UPDATE admin_users SET failed_logins = failed_logins + 1,
  locked_until = CASE WHEN failed_logins + 1 >= 20 THEN now() + interval '15 minutes' ELSE locked_until END
WHERE id = $1;

-- name: ResetFailedLogins :exec
UPDATE admin_users SET failed_logins = 0, locked_until = NULL WHERE id = $1;

-- name: DeleteAdmin :exec
DELETE FROM admin_users WHERE id = $1;

-- SetAdminTOTP writes the live second factor. It always clears the pending secret:
-- on confirm the pending secret has just been promoted, and on disable nothing about
-- the old enrolment may survive.
-- name: SetAdminTOTP :exec
UPDATE admin_users SET totp_secret_enc = $2, totp_enabled = $3, totp_pending_enc = NULL WHERE id = $1;

-- name: SetAdminTOTPPending :exec
UPDATE admin_users SET totp_pending_enc = $2 WHERE id = $1;

-- name: GetAdminTOTP :one
SELECT totp_secret_enc, totp_pending_enc, totp_enabled FROM admin_users WHERE id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO recovery_codes (admin_user_id, code_hash) VALUES ($1, $2);

-- name: ListRecoveryCodes :many
SELECT * FROM recovery_codes WHERE admin_user_id = $1 ORDER BY id;

-- UseRecoveryCode burns a code and reports how many rows it changed (:execrows). The
-- `used_at IS NULL` predicate is what makes the burn atomic: two concurrent logins
-- presenting the same code produce one row updated and one row not, so exactly one of
-- them is let in.
-- name: UseRecoveryCode :execrows
UPDATE recovery_codes SET used_at = now()
WHERE admin_user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE admin_user_id = $1;
