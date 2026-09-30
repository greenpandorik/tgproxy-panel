package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
)

type serviceResp struct {
	PublicURL   string `json:"public_url"`
	HideOnPanel bool   `json:"hide_on_panel"`
	TokenSet    bool   `json:"token_set"`
	Online      bool   `json:"online"`
}

func TestSubscriptionPagesOnTheirOwnDomain(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	var k keyResp2
	c.JSON(c.Get("/api/v1/keys/"+keyID), &k)
	token := tokenFromURL(t, *k.SubscriptionURL)

	if resp := c.Put("/api/v1/subscription-service", map[string]any{"public_url": "sub.example.org/path"}); resp.StatusCode != 422 {
		t.Fatalf("a URL with a path accepted: %d", resp.StatusCode)
	}
	if resp := c.Put("/api/v1/subscription-service", map[string]any{"public_url": "Sub.Example.org"}); resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("save %d %s", resp.StatusCode, b)
	}
	c.JSON(c.Get("/api/v1/keys/"+keyID), &k)
	if !strings.HasPrefix(*k.SubscriptionURL, "https://sub.example.org/s/") {
		t.Fatalf("links should move to the page domain: %s", *k.SubscriptionURL)
	}

	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(h.Server.URL + "/s/" + token + "?lang=en")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "https://sub.example.org/s/"+token+"?lang=en" {
		t.Fatalf("old links should redirect: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	onSub := func(path string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "sub.example.org"
		rec := httptest.NewRecorder()
		h.Router().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := onSub("/s/" + token); code != 200 || !strings.Contains(body, "fra1.example.com") {
		t.Fatalf("the panel should serve pages on the page domain: %d", code)
	}
	for _, p := range []string{"/", "/login", "/api/v1/auth/me", "/users"} {
		if code, _ := onSub(p); code != 404 {
			t.Fatalf("%s on the page domain: %d, the panel must stay hidden there", p, code)
		}
	}

	if resp := c.Put("/api/v1/subscription-service", map[string]any{"public_url": "sub.example.org", "hide_on_panel": true}); resp.StatusCode != 200 {
		t.Fatalf("hide %d", resp.StatusCode)
	}
	if resp := h.Anonymous().Get("/s/" + token); resp.StatusCode != 404 {
		t.Fatalf("hidden on the panel domain: %d", resp.StatusCode)
	}

	if resp := h.Anonymous().Get("/api/v1/subpage/pages/" + token); resp.StatusCode != 401 {
		t.Fatalf("page data without a service token: %d", resp.StatusCode)
	}
	var issued struct {
		Token   string `json:"token"`
		Command string `json:"command"`
	}
	c.JSON(c.Post("/api/v1/subscription-service/token", nil), &issued)
	if issued.Token == "" || !strings.Contains(issued.Command, "--subpage --domain sub.example.org") || !strings.Contains(issued.Command, issued.Token) {
		t.Fatalf("token %+v", issued)
	}
	svc := h.Anonymous().SetHeader("Authorization", "Bearer "+issued.Token)
	resp = svc.Get("/api/v1/subpage/pages/" + token)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "fra1.example.com") || strings.Contains(string(body), "top-secret-label") {
		t.Fatalf("service page data %d %s", resp.StatusCode, body)
	}
	resp = svc.Get("/api/v1/subpage/pages/nope")
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"state":"not_found"`) {
		t.Fatalf("unknown token for the service: %s", body)
	}
	if resp := svc.Post("/api/v1/subpage/heartbeat", map[string]string{"version": "9.9.9"}); resp.StatusCode != 204 {
		t.Fatalf("heartbeat %d", resp.StatusCode)
	}
	var view serviceResp
	c.JSON(c.Get("/api/v1/subscription-service"), &view)
	if !view.Online || !view.TokenSet {
		t.Fatalf("service should be online after a heartbeat: %+v", view)
	}
	if resp := c.Delete("/api/v1/subscription-service/token"); resp.StatusCode != 204 {
		t.Fatalf("revoke token %d", resp.StatusCode)
	}
	if resp := svc.Get("/api/v1/subpage/pages/" + token); resp.StatusCode != 401 {
		t.Fatalf("a revoked service token still works: %d", resp.StatusCode)
	}
}

func TestSecondDomainShowsOnlyPagesBeforeItIsSaved(t *testing.T) {
	h := apitest.New(t, func(d *api.Deps) { d.Cfg.SubpageDomain = "sub.example.org" })
	onSub := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "sub.example.org"
		rec := httptest.NewRecorder()
		h.Router().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := onSub("/login"); code != 404 {
		t.Fatalf("the panel must stay hidden on the page domain even before it is saved: %d", code)
	}
	if code := onSub("/s/unknown"); code != 404 {
		t.Fatalf("pages are still answered there: %d", code)
	}
	if code := onSub("/healthz"); code != 200 {
		t.Fatalf("healthz on the page domain: %d", code)
	}
}
