package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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

func TestSubscriptionKeyJSONReportsActive(t *testing.T) {
	_, c, keyID := twoNodeKey(t)
	var before keyResp2
	c.JSON(c.Get("/api/v1/keys/"+keyID), &before)
	if before.SubscriptionActive {
		t.Fatal("subscription_active should start false")
	}
	createSubscription(t, c, keyID)
	var after keyResp2
	c.JSON(c.Get("/api/v1/keys/"+keyID), &after)
	if !after.SubscriptionActive {
		t.Fatal("subscription_active should be true after create")
	}
}

type keyResp2 struct {
	SubscriptionActive bool `json:"subscription_active"`
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
