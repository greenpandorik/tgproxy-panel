// Package notify sends operator-facing alerts through the Telegram Bot API.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Telegram is a minimal client for the Telegram Bot API sendMessage call.
type Telegram struct {
	httpc *http.Client
	base  string
}

// NewTelegram builds a client.
func NewTelegram(httpc *http.Client, base string) *Telegram {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Telegram{httpc: httpc, base: strings.TrimSuffix(base, "/")}
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type sendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// SendWith posts text to chatID via the bot identified by botToken.
func (t *Telegram) SendWith(ctx context.Context, botToken, chatID, text string) error {
	url := t.base + "/bot" + botToken + "/sendMessage"
	body, err := json.Marshal(sendMessageRequest{
		ChatID: chatID, Text: text, ParseMode: "HTML", DisableWebPagePreview: true,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return stripToken(err, botToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpc.Do(req)
	if err != nil {
		return stripToken(err, botToken)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var out sendMessageResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK || !out.OK {
		desc := out.Description
		if desc == "" {
			desc = resp.Status
		}
		return fmt.Errorf("telegram: %s", desc)
	}
	return nil
}

func stripToken(err error, token string) error {
	if token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "***"))
}
