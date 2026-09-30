package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"tgwebproxy/internal/api/apitest"
)

type subCreateResp struct {
	URL       string `json:"url"`
	QRDataURI string `json:"qr_data_uri"`
}

func twoNodeKey(t *testing.T) (*apitest.Harness, *apitest.Client, string) {
	t.Helper()
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n1, _ := createNode(t, c, "fra1.example.com")
	n2, _ := createNode(t, c, "ams1.example.com")
	var k keyResp
	resp := c.Post("/api/v1/keys", map[string]any{
		"label": "top-secret-label", "type": "SHARED", "carrier_mode": "https",
		"node_ids": []string{n1.ID.String(), n2.ID.String()},
	})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create key %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)
	return h, c, k.ID.String()
}

func createSubscription(t *testing.T, c *apitest.Client, keyID string) subCreateResp {
	t.Helper()
	resp := c.Post("/api/v1/keys/"+keyID+"/subscription", nil)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create subscription %d %s", resp.StatusCode, b)
	}
	var out subCreateResp
	c.JSON(resp, &out)
	return out
}

func tokenFromURL(t *testing.T, rawURL string) string {
	t.Helper()
	i := strings.LastIndex(rawURL, "/s/")
	if i < 0 {
		t.Fatalf("url has no /s/ segment: %s", rawURL)
	}
	return rawURL[i+len("/s/"):]
}

func TestSubscriptionCreateServesPublicPageAndJSON(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	sub := createSubscription(t, c, keyID)
	if sub.URL == "" || !strings.HasPrefix(sub.QRDataURI, "data:image/png;base64,") {
		t.Fatalf("unexpected create response %+v", sub)
	}
	token := tokenFromURL(t, sub.URL)

	anon := h.Anonymous()

	pageResp := anon.Get("/s/" + token)
	body, _ := io.ReadAll(pageResp.Body)
	page := string(body)
	if pageResp.StatusCode != 200 {
		t.Fatalf("page status %d: %s", pageResp.StatusCode, page)
	}
	for _, want := range []string{"fra1.example.com", "ams1.example.com"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(page, "top-secret-label") {
		t.Error("page must never reveal the key label")
	}
	if cc := pageResp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rt := pageResp.Header.Get("X-Robots-Tag"); rt != "noindex" {
		t.Errorf("X-Robots-Tag = %q, want noindex", rt)
	}
	if csp := pageResp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Error("missing Content-Security-Policy header")
	}

	jsonResp := anon.Get("/s/" + token + ".json")
	if jsonResp.StatusCode != 200 {
		b, _ := io.ReadAll(jsonResp.Body)
		t.Fatalf("json status %d: %s", jsonResp.StatusCode, b)
	}
	var out struct {
		PanelName string `json:"panel_name"`
		Locations []struct {
			Name     string `json:"name"`
			Hostname string `json:"hostname"`
			TMe      string `json:"tme"`
			Tg       string `json:"tg"`
		} `json:"locations"`
	}
	anon.JSON(jsonResp, &out)
	if len(out.Locations) != 2 {
		t.Fatalf("locations = %+v, want 2", out.Locations)
	}
	hosts := map[string]bool{}
	for _, l := range out.Locations {
		hosts[l.Hostname] = true
		if l.TMe == "" || l.Tg == "" {
			t.Errorf("location %+v missing links", l)
		}
	}
	if !hosts["fra1.example.com"] || !hosts["ams1.example.com"] {
		t.Errorf("locations = %+v, missing a hostname", out.Locations)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "top-secret-label") {
		t.Error(".json must never reveal the key label")
	}
}

func TestNewKeyShowsItsSubscriptionLinkEveryTime(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	var first, again keyResp2
	c.JSON(c.Get("/api/v1/keys/"+keyID), &first)
	if !first.SubscriptionActive || first.SubscriptionURL == nil || first.SubscriptionLegacy {
		t.Fatalf("a new key should come with a visible subscription link: %+v", first)
	}
	c.JSON(c.Get("/api/v1/keys/"+keyID), &again)
	if again.SubscriptionURL == nil || *again.SubscriptionURL != *first.SubscriptionURL {
		t.Fatal("the same link must be shown every time")
	}
	if resp := h.Anonymous().Get("/s/" + tokenFromURL(t, *first.SubscriptionURL)); resp.StatusCode != 200 {
		t.Fatalf("the shown link does not open: %d", resp.StatusCode)
	}
	fresh := createSubscription(t, c, keyID)
	c.JSON(c.Get("/api/v1/keys/"+keyID), &again)
	if again.SubscriptionURL == nil || *again.SubscriptionURL != fresh.URL {
		t.Fatal("after a new link the key shows the new one")
	}
	if resp := c.Delete("/api/v1/keys/" + keyID + "/subscription"); resp.StatusCode != 204 {
		t.Fatalf("revoke %d", resp.StatusCode)
	}
	c.JSON(c.Get("/api/v1/keys/"+keyID), &again)
	if again.SubscriptionActive || again.SubscriptionURL != nil {
		t.Fatal("a revoked link must not be shown")
	}
}

type keyResp2 struct {
	SubscriptionActive   bool    `json:"subscription_active"`
	SubscriptionURL      *string `json:"subscription_url"`
	SubscriptionShortURL *string `json:"subscription_short_url"`
	SubscriptionLegacy   bool    `json:"subscription_legacy"`
	State                string  `json:"state"`
}

func TestSharedKeyShortAddressAndStates(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	anon := h.Anonymous()
	if resp := c.Patch("/api/v1/keys/"+keyID, map[string]any{"sub_slug": "Bad Slug"}); resp.StatusCode != 422 {
		t.Fatalf("bad slug accepted: %d", resp.StatusCode)
	}
	if resp := c.Patch("/api/v1/keys/"+keyID, map[string]any{"sub_slug": "team"}); resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("set slug %d %s", resp.StatusCode, b)
	}
	var k keyResp2
	c.JSON(c.Get("/api/v1/keys/"+keyID), &k)
	if k.SubscriptionShortURL == nil || !strings.HasSuffix(*k.SubscriptionShortURL, "/s/team") {
		t.Fatalf("short url %+v", k.SubscriptionShortURL)
	}
	if resp := anon.Get("/s/team"); resp.StatusCode != 200 {
		t.Fatalf("short address does not open: %d", resp.StatusCode)
	}
	for _, q := range []string{"", "?short=1&size=300"} {
		resp := c.Get("/api/v1/keys/" + keyID + "/subscription/qr" + q)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("qr%s %d %s", q, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	if resp := h.Anonymous().Get("/api/v1/keys/" + keyID + "/subscription/qr"); resp.StatusCode != 401 {
		t.Fatalf("anonymous qr %d", resp.StatusCode)
	}

	var personal keyResp
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "p", "type": "PERSONAL", "carrier_mode": "https", "node_ids": nodeIDsOf(t, c)}), &personal)
	if resp := c.Patch("/api/v1/keys/"+personal.ID.String(), map[string]any{"sub_slug": "team"}); resp.StatusCode != 422 {
		t.Fatalf("a personal key got a short address: %d", resp.StatusCode)
	}

	if resp := c.Post("/api/v1/keys/"+keyID+"/disable", nil); resp.StatusCode != 200 {
		t.Fatalf("disable %d", resp.StatusCode)
	}
	c.JSON(c.Get("/api/v1/keys/"+keyID), &k)
	if k.State != "disabled" {
		t.Fatalf("state %s", k.State)
	}
	resp := anon.Get("/s/team")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 403 || !strings.Contains(string(body), "временно выключен") {
		t.Fatalf("disabled page %d", resp.StatusCode)
	}
	var sum map[string]int64
	c.JSON(c.Get("/api/v1/keys/summary"), &sum)
	if sum["disabled"] != 1 || sum["total"] != 2 {
		t.Fatalf("summary %v", sum)
	}
	var list struct {
		Items []keyResp2 `json:"items"`
		Total int        `json:"total"`
	}
	c.JSON(c.Get("/api/v1/keys?state=disabled"), &list)
	if list.Total != 1 {
		t.Fatalf("state filter %+v", list)
	}
	if resp := c.Post("/api/v1/keys/"+keyID+"/enable", nil); resp.StatusCode != 200 {
		t.Fatalf("enable %d", resp.StatusCode)
	}
	if resp := anon.Get("/s/team"); resp.StatusCode != 200 {
		t.Fatalf("enabled key page %d", resp.StatusCode)
	}

	past := time.Now().Add(-time.Hour)
	if resp := c.Patch("/api/v1/keys/"+keyID, map[string]any{"expires_at": past}); resp.StatusCode != 200 {
		t.Fatalf("set past expiry %d", resp.StatusCode)
	}
	resp = anon.Get("/s/team")
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 410 || !strings.Contains(string(body), "Срок доступа") {
		t.Fatalf("expired page %d", resp.StatusCode)
	}
}

func nodeIDsOf(t *testing.T, c *apitest.Client) []string {
	t.Helper()
	var nodes struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &nodes)
	out := make([]string, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		out = append(out, n.ID)
	}
	return out
}

func TestSubscriptionRotateInvalidatesOldToken(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	first := createSubscription(t, c, keyID)
	oldToken := tokenFromURL(t, first.URL)

	second := createSubscription(t, c, keyID)
	newToken := tokenFromURL(t, second.URL)
	if newToken == oldToken {
		t.Fatal("rotate must mint a new token")
	}

	anon := h.Anonymous()
	if resp := anon.Get("/s/" + oldToken); resp.StatusCode != 404 {
		t.Errorf("old token status = %d, want 404", resp.StatusCode)
	}
	if resp := anon.Get("/s/" + newToken); resp.StatusCode != 200 {
		t.Errorf("new token status = %d, want 200", resp.StatusCode)
	}
}

func TestSubscriptionRevokedKeyReturns410(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	sub := createSubscription(t, c, keyID)
	token := tokenFromURL(t, sub.URL)

	if resp := c.Post("/api/v1/keys/"+keyID+"/revoke", nil); resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("revoke key %d %s", resp.StatusCode, b)
	}

	anon := h.Anonymous()
	if resp := anon.Get("/s/" + token); resp.StatusCode != http.StatusGone {
		t.Errorf("status = %d, want 410", resp.StatusCode)
	}
	if resp := anon.Get("/s/" + token + ".json"); resp.StatusCode != http.StatusGone {
		t.Errorf("json status = %d, want 410", resp.StatusCode)
	}
}

func TestSubscriptionDeleteRevokesToken(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	sub := createSubscription(t, c, keyID)
	token := tokenFromURL(t, sub.URL)

	if resp := c.Delete("/api/v1/keys/" + keyID + "/subscription"); resp.StatusCode != 204 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete subscription %d %s", resp.StatusCode, b)
	}
	var k keyResp2
	c.JSON(c.Get("/api/v1/keys/"+keyID), &k)
	if k.SubscriptionActive {
		t.Fatal("subscription_active should be false after delete")
	}

	anon := h.Anonymous()
	if resp := anon.Get("/s/" + token); resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSubscriptionCreateOnRevokedKeyConflicts(t *testing.T) {
	_, c, keyID := twoNodeKey(t)
	if resp := c.Post("/api/v1/keys/"+keyID+"/revoke", nil); resp.StatusCode != 200 {
		t.Fatalf("revoke key %d", resp.StatusCode)
	}
	resp := c.Post("/api/v1/keys/"+keyID+"/subscription", nil)
	if resp.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 409: %s", resp.StatusCode, b)
	}
}

func TestSubscriptionUnknownTokenReturns404(t *testing.T) {
	h := apitest.New(t)
	anon := h.Anonymous()
	if resp := anon.Get("/s/does-not-exist"); resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if resp := anon.Get("/s/does-not-exist.json"); resp.StatusCode != 404 {
		t.Errorf("json status = %d, want 404", resp.StatusCode)
	}
}

func TestSubscriptionErrorPagesAreBrandedHTMLNotJSON(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	anon := h.Anonymous()

	// Unknown token: 404.
	resp := anon.Get("/s/does-not-exist")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("unknown token Content-Type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if !strings.Contains(page, "<html") {
		t.Errorf("unknown token body is not HTML: %s", page)
	}
	if strings.HasPrefix(strings.TrimSpace(page), "{") {
		t.Errorf("unknown token body looks like JSON, want HTML: %s", page)
	}

	// The .json twin still answers 404 with JSON.
	jsonResp := anon.Get("/s/does-not-exist.json")
	if jsonResp.StatusCode != http.StatusNotFound {
		t.Fatalf(".json unknown token status = %d, want 404", jsonResp.StatusCode)
	}
	if ct := jsonResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf(".json Content-Type = %q, want application/json", ct)
	}

	// Revoked key: 410, same HTML shape.
	sub := createSubscription(t, c, keyID)
	token := tokenFromURL(t, sub.URL)
	if resp := c.Post("/api/v1/keys/"+keyID+"/revoke", nil); resp.StatusCode != 200 {
		t.Fatalf("revoke key %d", resp.StatusCode)
	}
	goneResp := anon.Get("/s/" + token)
	if goneResp.StatusCode != http.StatusGone {
		t.Fatalf("revoked key status = %d, want 410", goneResp.StatusCode)
	}
	if ct := goneResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("revoked key Content-Type = %q, want text/html", ct)
	}
	goneBody, _ := io.ReadAll(goneResp.Body)
	if !strings.Contains(string(goneBody), "<html") {
		t.Errorf("revoked key body is not HTML: %s", goneBody)
	}
}

func TestSubscriptionRateLimited(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	sub := createSubscription(t, c, keyID)
	token := tokenFromURL(t, sub.URL)
	anon := h.Anonymous()

	var last *http.Response
	for i := 0; i < 61; i++ {
		last = anon.Get("/s/" + token)
		if i < 60 && last.StatusCode != 200 {
			t.Fatalf("request %d: status %d, want 200", i+1, last.StatusCode)
		}
		_, _ = io.Copy(io.Discard, last.Body)
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("61st request status = %d, want 429", last.StatusCode)
	}
}

func TestSubscriptionPageFollowsItsSettings(t *testing.T) {
	h, c, keyID := twoNodeKey(t)
	var nodes struct {
		Items []struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &nodes)
	hidden := ""
	for _, n := range nodes.Items {
		if n.Hostname == "ams1.example.com" {
			hidden = n.ID
		}
	}
	settings := map[string]any{
		"language": "en", "platform": "desktop", "title_ru": "Наш прокси", "title_en": "Our proxy", "intro_ru": "", "intro_en": "", "show_fake_tls": true, "show_web": true,
		"show_backup_domains": true, "show_guide": true, "show_status": true, "show_qr": false, "hidden_nodes": []string{hidden},
	}
	if resp := c.Put("/api/v1/settings", map[string]any{"subscription_page": settings}); resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("save %d %s", resp.StatusCode, b)
	}
	token := tokenFromURL(t, createSubscription(t, c, keyID).URL)
	body, _ := io.ReadAll(h.Anonymous().Get("/s/" + token).Body)
	page := string(body)
	for _, want := range []string{`lang="en"`, "Our proxy", "fra1.example.com", "Install Telegram"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(page, "ams1.example.com") || strings.Contains(page, "data:image/png") {
		t.Error("a hidden server or a QR code leaked onto the page")
	}
	jsonBody, _ := io.ReadAll(h.Anonymous().Get("/s/" + token + ".json").Body)
	if strings.Contains(string(jsonBody), "ams1.example.com") {
		t.Error("the JSON view ignores hidden servers")
	}

	preview := c.Get("/api/v1/settings/subscription-page/preview?platform=ios&language=ru")
	html, _ := io.ReadAll(preview.Body)
	if preview.StatusCode != 200 || !strings.Contains(string(html), "Открыть App Store") || !strings.Contains(string(html), "fra1.example.com") {
		t.Fatalf("preview %d: %.300s", preview.StatusCode, html)
	}

	all := []string{}
	for _, n := range nodes.Items {
		all = append(all, n.ID)
	}
	settings["hidden_nodes"] = all
	if resp := c.Put("/api/v1/settings", map[string]any{"subscription_page": settings}); resp.StatusCode != 422 {
		t.Fatalf("hiding every server was accepted: %d", resp.StatusCode)
	}
	settings["hidden_nodes"] = []string{hidden}
	settings["show_web"], settings["show_fake_tls"] = false, false
	if resp := c.Put("/api/v1/settings", map[string]any{"subscription_page": settings}); resp.StatusCode != 422 {
		t.Fatalf("hiding every link kind was accepted: %d", resp.StatusCode)
	}
}
