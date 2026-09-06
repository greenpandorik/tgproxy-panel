package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/store/db"
)

const (
	settingApplyInterval  = "apply_interval"
	settingOfflineAfter   = "offline_after"
	settingTelegramAlerts = "telegram_alerts"
	settingBackupSchedule = "backup_schedule"
)

// telegramSender is satisfied by *notify.Telegram; kept as a narrow,
// unexported interface here (rather than importing notify's concrete type
// everywhere) so tests can inject a fake sender without a real HTTP call.
type telegramSender interface {
	SendWith(ctx context.Context, botToken, chatID, text string) error
}

func (s *Server) mountSettings(r chi.Router) {
	r.Get("/settings", s.handleGetSettings)
	r.With(RequireRole(RoleOwner)).Put("/settings", s.handlePutSettings)
	r.With(RequireRole(RoleOwner)).Post("/settings/telegram/test", s.handleTelegramTest)
}

// TelegramConfig implements the worker.AlertSource contract: it reads the
// stored telegram_alerts setting and decrypts the bot token with the box. An
// unset token decrypts to "" rather than an error.
func (s *Server) TelegramConfig(ctx context.Context) (enabled bool, botToken, chatID string, err error) {
	stored := s.getTelegramAlerts(ctx)
	if stored.BotTokenEnc == "" {
		return stored.Enabled, "", stored.ChatID, nil
	}
	raw, err := base64.StdEncoding.DecodeString(stored.BotTokenEnc)
	if err != nil {
		return false, "", "", err
	}
	token, err := s.box.DecryptString(raw)
	if err != nil {
		return false, "", "", err
	}
	return stored.Enabled, token, stored.ChatID, nil
}

// telegramAlertsStored is the shape persisted in the settings table. The bot
// token is never stored in the clear: BotTokenEnc holds base64(box.Encrypt
// (token)). Never serialize this type to an HTTP response.
type telegramAlertsStored struct {
	Enabled     bool   `json:"enabled"`
	BotTokenEnc string `json:"bot_token_enc"`
	ChatID      string `json:"chat_id"`
}

// telegramAlertsOut is the shape returned to clients: bot_token_set instead
// of the token itself.
type telegramAlertsOut struct {
	Enabled     bool   `json:"enabled"`
	BotTokenSet bool   `json:"bot_token_set"`
	ChatID      string `json:"chat_id"`
}

func (s *Server) getSettingInt(ctx context.Context, key string, def int) int {
	raw, err := s.store.Q.GetSetting(ctx, key)
	if err != nil {
		return def
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return def
	}
	return v
}

func (s *Server) getTelegramAlerts(ctx context.Context) telegramAlertsStored {
	raw, err := s.store.Q.GetSetting(ctx, settingTelegramAlerts)
	if err != nil {
		return telegramAlertsStored{}
	}
	var stored telegramAlertsStored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return telegramAlertsStored{}
	}
	return stored
}

func (s *Server) settingsJSON(ctx context.Context) map[string]any {
	applyInterval := s.getSettingInt(ctx, settingApplyInterval, s.cfg.ApplyInterval)
	offlineAfter := s.getSettingInt(ctx, settingOfflineAfter, s.cfg.OfflineAfter)
	stored := s.getTelegramAlerts(ctx)

	return map[string]any{
		"apply_interval": applyInterval,
		"offline_after":  offlineAfter,
		"telegram_alerts": telegramAlertsOut{
			Enabled: stored.Enabled, BotTokenSet: stored.BotTokenEnc != "", ChatID: stored.ChatID,
		},
		"backup_schedule": s.backupSchedule(ctx),
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.settingsJSON(r.Context()))
}

type putTelegramAlertsReq struct {
	Enabled  bool    `json:"enabled"`
	BotToken *string `json:"bot_token"`
	ChatID   string  `json:"chat_id"`
}

type putSettingsReq struct {
	ApplyInterval  *int                  `json:"apply_interval"`
	OfflineAfter   *int                  `json:"offline_after"`
	TelegramAlerts *putTelegramAlertsReq `json:"telegram_alerts"`
	BackupSchedule *backup.Schedule      `json:"backup_schedule"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req putSettingsReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	fields := map[string]string{}
	if req.ApplyInterval != nil && (*req.ApplyInterval < 10 || *req.ApplyInterval > 3600) {
		fields["apply_interval"] = "must be 10..3600"
	}
	if req.OfflineAfter != nil && (*req.OfflineAfter < 30 || *req.OfflineAfter > 3600) {
		fields["offline_after"] = "must be 30..3600"
	}
	if req.BackupSchedule != nil {
		for k, v := range req.BackupSchedule.Validate() {
			fields[k] = v
		}
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}

	ctx := r.Context()
	changed := []string{}

	upsert := func(key string, v any) bool {
		raw, err := json.Marshal(v)
		if err != nil {
			internal(w)
			return false
		}
		if err := s.store.Q.UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: raw}); err != nil {
			internal(w)
			return false
		}
		changed = append(changed, key)
		return true
	}

	if req.ApplyInterval != nil {
		if !upsert(settingApplyInterval, *req.ApplyInterval) {
			return
		}
	}
	if req.OfflineAfter != nil {
		if !upsert(settingOfflineAfter, *req.OfflineAfter) {
			return
		}
	}
	if req.TelegramAlerts != nil {
		stored := telegramAlertsStored{Enabled: req.TelegramAlerts.Enabled, ChatID: req.TelegramAlerts.ChatID}
		switch {
		case req.TelegramAlerts.BotToken == nil:
			// Omitted: preserve whatever token (if any) is already stored.
			stored.BotTokenEnc = s.getTelegramAlerts(ctx).BotTokenEnc
		case *req.TelegramAlerts.BotToken == "":
			// Explicit empty string: clear the stored token.
			stored.BotTokenEnc = ""
		default:
			enc, err := s.box.EncryptString(*req.TelegramAlerts.BotToken)
			if err != nil {
				internal(w)
				return
			}
			stored.BotTokenEnc = base64.StdEncoding.EncodeToString(enc)
		}
		if !upsert(settingTelegramAlerts, stored) {
			return
		}
	}
	if req.BackupSchedule != nil {
		if !upsert(settingBackupSchedule, *req.BackupSchedule) {
			return
		}
	}

	s.Audit(ctx, "settings.update", "settings", "", map[string]any{"keys": changed})
	writeJSON(w, 200, s.settingsJSON(ctx))
}

type postTelegramTestReq struct {
	BotToken *string `json:"bot_token"`
	ChatID   *string `json:"chat_id"`
}

// handleTelegramTest sends a one-off "Test message from <panel name>" to the
// configured chat, using the bot token and chat id from the request body when
// given and falling back to the stored (decrypted) ones otherwise. It never
// returns or logs the token: only {"ok":true} on success, or the Telegram API's
// error description on failure.
//
// Both overrides exist for the same reason: the settings form enables "Send test"
// off its own in-progress values, so a chat id typed but not yet saved has to reach
// the handler or the button returns "must be configured" for a form that looks
// complete. Neither override is persisted - only the send uses them.
func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	var req postTelegramTestReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx := r.Context()

	_, storedToken, chatID, err := s.TelegramConfig(ctx)
	if err != nil {
		internal(w)
		return
	}
	token := storedToken
	if req.BotToken != nil && *req.BotToken != "" {
		token = *req.BotToken
	}
	if req.ChatID != nil && strings.TrimSpace(*req.ChatID) != "" {
		chatID = strings.TrimSpace(*req.ChatID)
	}
	if token == "" || chatID == "" {
		fields := map[string]string{}
		if token == "" {
			fields["bot_token"] = "required"
		}
		if chatID == "" {
			fields["chat_id"] = "required"
		}
		s.Audit(ctx, "settings.telegram_test", "settings", "", map[string]any{"ok": false})
		validation(w, fields)
		return
	}

	panelName := "panel"
	if b, err := s.store.Q.GetActiveBranding(ctx); err == nil && b.PanelName != "" {
		panelName = b.PanelName
	}

	if err := s.tg.SendWith(ctx, token, chatID, "Test message from "+panelName); err != nil {
		s.Audit(ctx, "settings.telegram_test", "settings", "", map[string]any{"ok": false})
		writeError(w, 502, "telegram_error", err.Error(), nil)
		return
	}
	s.Audit(ctx, "settings.telegram_test", "settings", "", map[string]any{"ok": true})
	writeJSON(w, 200, map[string]any{"ok": true})
}
