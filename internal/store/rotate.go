package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

// Report counts the rows Rotate (or CountPending) touched, one field per
// encrypted column.
type Report struct {
	Profiles    int
	Keys        int
	TOTPSecrets int
	TOTPPending int
	Settings    int
}

// settingTelegramAlertsKey mirrors the unexported settingTelegramAlerts
// constant in internal/api. It is duplicated here rather than imported
// because internal/api imports internal/store, and the reverse import would
// cycle.
const settingTelegramAlertsKey = "telegram_alerts"

// encColumn names one bytea column encrypted with the master-key Box.
type encColumn struct{ table, column string }

// rotatableColumns lists every such column except settings.telegram_alerts,
// whose ciphertext sits base64-encoded inside a JSON value rather than in its
// own column and is handled separately by rotateTelegramAlerts.
var rotatableColumns = []encColumn{
	{"profiles", "secret_enc"},
	{"access_keys", "secret_enc"},
	{"admin_users", "totp_secret_enc"},
	{"admin_users", "totp_pending_enc"},
}

// Rotate re-encrypts every encrypted column from key version `from` to key
// version `to`, in one transaction. `from` must contain every key version any
// stored row might still be using; `to` is the new current version. A row
// already at to.CurrentVersion() is left untouched, which makes Rotate
// idempotent - running it again after a successful rotation reports all
// zeros. Every re-encrypted blob is decrypted with `to` and compared against
// the original plaintext before commit, as a self-check; any error - a
// decrypt failure under `from`, a failed verification, anything - rolls the
// whole transaction back, so a mid-way failure changes no row at all.
func Rotate(ctx context.Context, st *Store, from, to *crypto.Box) (Report, error) {
	tx, err := st.Pool.Begin(ctx)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var report Report
	for _, c := range rotatableColumns {
		n, err := rotateColumn(ctx, tx, c, from, to)
		if err != nil {
			return Report{}, err
		}
		switch {
		case c.table == "profiles":
			report.Profiles = n
		case c.table == "access_keys":
			report.Keys = n
		case c.column == "totp_secret_enc":
			report.TOTPSecrets = n
		case c.column == "totp_pending_enc":
			report.TOTPPending = n
		}
	}

	n, err := rotateTelegramAlerts(ctx, tx, from, to)
	if err != nil {
		return Report{}, err
	}
	report.Settings = n

	if err := tx.Commit(ctx); err != nil {
		return Report{}, err
	}
	return report, nil
}

// CountPending reports, per column, how many rows are not yet encrypted at
// to.CurrentVersion() - the counts `panel keys rotate --dry-run` prints. It
// only reads the version prefix of each blob: nothing is decrypted,
// re-encrypted, locked or written.
func CountPending(ctx context.Context, st *Store, to *crypto.Box) (Report, error) {
	var report Report
	for _, c := range rotatableColumns {
		rows, err := selectRows(ctx, st.Pool, c, false)
		if err != nil {
			return Report{}, err
		}
		n := 0
		for _, r := range rows {
			if to.Version(r.blob) != to.CurrentVersion() {
				n++
			}
		}
		switch {
		case c.table == "profiles":
			report.Profiles = n
		case c.table == "access_keys":
			report.Keys = n
		case c.column == "totp_secret_enc":
			report.TOTPSecrets = n
		case c.column == "totp_pending_enc":
			report.TOTPPending = n
		}
	}

	blob, ok, err := telegramAlertsBlob(ctx, st.Pool)
	if err != nil {
		return Report{}, err
	}
	if ok && to.Version(blob) != to.CurrentVersion() {
		report.Settings = 1
	}
	return report, nil
}

type pendingRow struct {
	id   uuid.UUID
	blob []byte
}

// selectRows reads every non-NULL blob in table.column. forUpdate locks the
// rows FOR UPDATE, which only makes sense (and is only used) inside Rotate's
// transaction; CountPending's read-only pass leaves it off.
//
// table and column are always one of the fixed literals in rotatableColumns,
// never caller/user input, so building the query with fmt.Sprintf here is
// safe.
func selectRows(ctx context.Context, dbtx db.DBTX, c encColumn, forUpdate bool) ([]pendingRow, error) {
	q := fmt.Sprintf(`SELECT id, %s FROM %s WHERE %s IS NOT NULL`, c.column, c.table, c.column)
	if forUpdate {
		q += " FOR UPDATE"
	}
	rows, err := dbtx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", c.table, c.column, err)
	}
	defer rows.Close()
	var out []pendingRow
	for rows.Next() {
		var r pendingRow
		if err := rows.Scan(&r.id, &r.blob); err != nil {
			return nil, fmt.Errorf("%s.%s: %w", c.table, c.column, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s.%s: %w", c.table, c.column, err)
	}
	return out, nil
}

// rotateColumn re-encrypts every row of one column that is not already at
// to.CurrentVersion(), returning how many rows it changed.
func rotateColumn(ctx context.Context, tx pgx.Tx, c encColumn, from, to *crypto.Box) (int, error) {
	rows, err := selectRows(ctx, tx, c, true)
	if err != nil {
		return 0, err
	}
	updateSQL := fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE id = $2`, c.table, c.column)
	count := 0
	for _, r := range rows {
		if to.Version(r.blob) == to.CurrentVersion() {
			continue
		}
		newBlob, err := reencrypt(r.blob, from, to)
		if err != nil {
			return 0, fmt.Errorf("%s.%s id=%s: %w", c.table, c.column, r.id, err)
		}
		if _, err := tx.Exec(ctx, updateSQL, newBlob, r.id); err != nil {
			return 0, fmt.Errorf("%s.%s id=%s: update: %w", c.table, c.column, r.id, err)
		}
		count++
	}
	return count, nil
}

// telegramAlertsBlob reads the raw bot_token_enc blob out of the
// telegram_alerts setting, decoding its base64. ok is false when the setting
// row does not exist, or exists but has no token configured (an empty
// bot_token_enc, the normal state for a panel that has never set one up).
func telegramAlertsBlob(ctx context.Context, dbtx db.DBTX) ([]byte, bool, error) {
	var raw []byte
	err := dbtx.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, settingTelegramAlertsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("settings.%s: %w", settingTelegramAlertsKey, err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, false, fmt.Errorf("settings.%s: %w", settingTelegramAlertsKey, err)
	}
	encStr, _ := stored["bot_token_enc"].(string)
	if encStr == "" {
		return nil, false, nil
	}
	blob, err := base64.StdEncoding.DecodeString(encStr)
	if err != nil {
		return nil, false, fmt.Errorf("settings.%s.bot_token_enc: invalid base64: %w", settingTelegramAlertsKey, err)
	}
	return blob, true, nil
}

// rotateTelegramAlerts re-encrypts settings.telegram_alerts.bot_token_enc in
// place, preserving every other field in the stored JSON. It locks the
// settings row FOR UPDATE for the rest of the transaction.
func rotateTelegramAlerts(ctx context.Context, tx pgx.Tx, from, to *crypto.Box) (int, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1 FOR UPDATE`, settingTelegramAlertsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("settings.%s: %w", settingTelegramAlertsKey, err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		return 0, fmt.Errorf("settings.%s: %w", settingTelegramAlertsKey, err)
	}
	encStr, _ := stored["bot_token_enc"].(string)
	if encStr == "" {
		return 0, nil
	}
	blob, err := base64.StdEncoding.DecodeString(encStr)
	if err != nil {
		return 0, fmt.Errorf("settings.%s.bot_token_enc: invalid base64: %w", settingTelegramAlertsKey, err)
	}
	if to.Version(blob) == to.CurrentVersion() {
		return 0, nil
	}
	newBlob, err := reencrypt(blob, from, to)
	if err != nil {
		return 0, fmt.Errorf("settings.%s.bot_token_enc: %w", settingTelegramAlertsKey, err)
	}
	stored["bot_token_enc"] = base64.StdEncoding.EncodeToString(newBlob)
	newRaw, err := json.Marshal(stored)
	if err != nil {
		return 0, fmt.Errorf("settings.%s: %w", settingTelegramAlertsKey, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE settings SET value = $1 WHERE key = $2`, newRaw, settingTelegramAlertsKey); err != nil {
		return 0, fmt.Errorf("settings.%s: update: %w", settingTelegramAlertsKey, err)
	}
	return 1, nil
}

// reencrypt decrypts blob with `from` and re-encrypts the plaintext with
// `to`, then decrypts its own output with `to` and compares it against the
// original plaintext as a self-check before handing the new blob back.
func reencrypt(blob []byte, from, to *crypto.Box) ([]byte, error) {
	plain, err := from.Decrypt(blob)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	newBlob, err := to.Encrypt(plain)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	verify, err := to.Decrypt(newBlob)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	if !bytes.Equal(verify, plain) {
		return nil, errors.New("verify: re-encrypted blob does not decrypt back to the original plaintext")
	}
	return newBlob, nil
}
