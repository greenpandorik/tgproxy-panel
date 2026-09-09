package notify_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tgwebproxy/internal/notify"
)

const testToken = "123456:AA-SUPER-SECRET-TEST-TOKEN"

func TestSendWithPostsExpectedRequest(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tg := notify.NewTelegram(srv.Client(), srv.URL)
	if err := tg.SendWith(t.Context(), testToken, "42", "hello world"); err != nil {
		t.Fatalf("SendWith: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/bot"+testToken+"/sendMessage" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["chat_id"] != "42" || gotBody["text"] != "hello world" {
		t.Fatalf("body = %+v", gotBody)
	}
	if gotBody["parse_mode"] != "HTML" {
		t.Fatalf("parse_mode = %+v", gotBody["parse_mode"])
	}
	if gotBody["disable_web_page_preview"] != true {
		t.Fatalf("disable_web_page_preview = %+v", gotBody["disable_web_page_preview"])
	}
}

func TestSendWithMapsAPIErrorDescriptionWithoutLeakingToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()

	tg := notify.NewTelegram(srv.Client(), srv.URL)
	err := tg.SendWith(t.Context(), testToken, "42", "hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("error = %v, want it to mention the API description", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the bot token: %v", err)
	}
}

func TestSendWithNetworkErrorNeverLeaksToken(t *testing.T) {
	tg := notify.NewTelegram(http.DefaultClient, "http://127.0.0.1:1")
	err := tg.SendWith(t.Context(), testToken, "42", "hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the bot token: %v", err)
	}
}
