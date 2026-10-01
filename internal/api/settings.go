package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/backup"
	"tgwebproxy/internal/branding"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/subscription"
)

const (
	settingApplyInterval  = "apply_interval"
	settingOfflineAfter   = "offline_after"
	settingTelegramAlerts = "telegram_alerts"
	settingBackupSchedule = "backup_schedule"
)

type telegramSender interface {
	SendWith(ctx context.Context, botToken, chatID, text string) error
}

func (s *Server) mountSettings(r chi.Router) {
	r.Get("/settings", s.handleGetSettings)
	r.With(RequireRole(RoleOwner)).Put("/settings", s.handlePutSettings)
	r.With(RequireRole(RoleOwner)).Post("/settings/telegram/test", s.handleTelegramTest)
	r.With(RequireRole(writers...)).Get("/settings/subscription-page/preview", s.handleSubscriptionPreview)
}

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

// telegramAlertsStored is the shape persisted in the settings table.
type telegramAlertsStored struct {
	Enabled     bool   `json:"enabled"`
	BotTokenEnc string `json:"bot_token_enc"`
	ChatID      string `json:"chat_id"`
	Language    string `json:"language,omitempty"`
}

// telegramAlertsOut is the shape returned to clients: bot_token_set instead of the token itself.
type telegramAlertsOut struct {
	Enabled     bool   `json:"enabled"`
	BotTokenSet bool   `json:"bot_token_set"`
	ChatID      string `json:"chat_id"`
	Language    string `json:"language"`
}

// AlertLanguage is the language notifications are written in; Russian until the owner picks one.
func (s *Server) AlertLanguage(ctx context.Context) string {
	return string(alerttext.ParseLang(s.getTelegramAlerts(ctx).Language))
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
			Language: string(alerttext.ParseLang(stored.Language)),
		},
		"backup_schedule":          s.backupSchedule(ctx),
		"subscription_page":        s.subscriptionSettings(ctx),
		"alert_webhook_configured": s.cfg.AlertWebhookURL != "",
		"metrics_token_set":        s.cfg.MetricsToken != "",
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.settingsJSON(r.Context()))
}

type putTelegramAlertsReq struct {
	Enabled  bool    `json:"enabled"`
	BotToken *string `json:"bot_token"`
	ChatID   string  `json:"chat_id"`
	Language *string `json:"language"`
}

type putSettingsReq struct {
	ApplyInterval    *int                   `json:"apply_interval"`
	OfflineAfter     *int                   `json:"offline_after"`
	TelegramAlerts   *putTelegramAlertsReq  `json:"telegram_alerts"`
	BackupSchedule   *backup.Schedule       `json:"backup_schedule"`
	SubscriptionPage *subscription.Settings `json:"subscription_page"`
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
	if req.SubscriptionPage != nil {
		for k, v := range req.SubscriptionPage.Normalized().Validate() {
			fields[k] = v
		}
		if s.hidesEveryServer(r.Context(), req.SubscriptionPage.HiddenNodes) {
			fields["subscription_page.hidden_nodes"] = "keep at least one server"
		}
	}
	if t := req.TelegramAlerts; t != nil && t.Language != nil && *t.Language != string(alerttext.RU) && *t.Language != string(alerttext.EN) {
		fields["telegram_alerts.language"] = "must be ru or en"
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
		previous := s.getTelegramAlerts(ctx)
		stored := telegramAlertsStored{Enabled: req.TelegramAlerts.Enabled, ChatID: req.TelegramAlerts.ChatID, Language: previous.Language}
		if l := req.TelegramAlerts.Language; l != nil {
			stored.Language = *l
		}
		switch {
		case req.TelegramAlerts.BotToken == nil:
			// Omitted: preserve whatever token (if any) is already stored.
			stored.BotTokenEnc = previous.BotTokenEnc
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
	if req.SubscriptionPage != nil {
		if !upsert(settingSubscriptionPage, req.SubscriptionPage.Normalized()) {
			return
		}
	}

	s.Audit(ctx, "settings.update", "settings", "", map[string]any{"keys": changed})
	writeJSON(w, 200, s.settingsJSON(ctx))
}

type postTelegramTestReq struct {
	BotToken *string `json:"bot_token"`
	ChatID   *string `json:"chat_id"`
	Language *string `json:"language"`
}

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

	panelName := branding.DefaultPanelName
	if b, err := s.store.Q.GetActiveBranding(ctx); err == nil && b.PanelName != "" {
		panelName = b.PanelName
	}

	lang := alerttext.ParseLang(s.AlertLanguage(ctx))
	if req.Language != nil {
		lang = alerttext.ParseLang(*req.Language)
	}
	if err := s.tg.SendWith(ctx, token, chatID, alerttext.Default().Test(lang, panelName).HTML); err != nil {
		s.Audit(ctx, "settings.telegram_test", "settings", "", map[string]any{"ok": false})
		writeError(w, 502, "telegram_error", err.Error(), nil)
		return
	}
	s.Audit(ctx, "settings.telegram_test", "settings", "", map[string]any{"ok": true})
	writeJSON(w, 200, map[string]any{"ok": true})
}
