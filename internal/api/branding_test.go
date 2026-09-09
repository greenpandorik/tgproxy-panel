package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/branding"
)

func TestPublicBrandingAndUpdate(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var pub struct {
		PanelName    string `json:"panel_name"`
		PrimaryColor string `json:"primary_color"`
		ThemeDefault string `json:"theme_default"`
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &pub)
	// The defaults come from migration 00008 and must agree with the Go constants
	// (a fresh install, the login page and the subscription page all read them).
	if pub.PanelName != branding.DefaultPanelName || pub.PrimaryColor != branding.DefaultPrimaryColor || pub.ThemeDefault != "dark" {
		t.Fatalf("defaults %+v", pub)
	}
	var list struct {
		Items []struct {
			ID       uuid.UUID `json:"id"`
			IsActive bool      `json:"is_active"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	id := list.Items[0].ID
	resp := c.Put("/api/v1/branding/profiles/"+id.String(), map[string]any{"name": "default", "panel_name": "Acme Proxy", "primary_color": "#112233", "accent_color": "#445566", "theme_default": "light", "custom_css": "@import url(https://x); .a{color:red}"})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update %d %s", resp.StatusCode, b)
	}
	var updated struct {
		CustomCSS string `json:"custom_css"`
	}
	c.JSON(resp, &updated)
	if bytes.Contains([]byte(updated.CustomCSS), []byte("@import")) {
		t.Fatal("css not sanitised")
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &pub)
	if pub.PanelName != "Acme Proxy" || pub.PrimaryColor != "#112233" || pub.ThemeDefault != "light" {
		t.Fatalf("public not updated %+v", pub)
	}
	if resp := c.Put("/api/v1/branding/profiles/"+id.String(), map[string]any{"name": "default", "panel_name": "x", "primary_color": "red", "accent_color": "#445566", "theme_default": "dark"}); resp.StatusCode != 422 {
		t.Fatalf("bad color expected 422, got %d", resp.StatusCode)
	}
}

func TestBrandingUpload(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	id := list.Items[0].ID
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "logo.svg")
	_, _ = fw.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`))
	_ = mw.Close()
	resp := c.PostRaw("/api/v1/branding/profiles/"+id.String()+"/upload?kind=logo", mw.FormDataContentType(), body.Bytes())
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload %d %s", resp.StatusCode, b)
	}
	var pub struct {
		LogoURL string `json:"logo_url"`
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &pub)
	if pub.LogoURL == "" {
		t.Fatal("logo url missing")
	}
	if r := h.Anonymous().Get(pub.LogoURL); r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("asset fetch %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
}

// uploadBrandingAsset posts filename/content as the "file" field of a kind=logo upload for
// profile id and returns the resulting logo_url.
func uploadBrandingAsset(t *testing.T, c interface {
	PostRaw(path, contentType string, body []byte) *http.Response
	JSON(resp *http.Response, out any)
}, id uuid.UUID, filename string, content []byte,
) string {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()
	resp := c.PostRaw("/api/v1/branding/profiles/"+id.String()+"/upload?kind=logo", mw.FormDataContentType(), body.Bytes())
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload %d %s", resp.StatusCode, b)
	}
	var pub struct {
		LogoURL string `json:"logo_url"`
	}
	c.JSON(resp, &pub)
	return pub.LogoURL
}

func TestBrandingProfileCreateCopiesAssetFiles(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	activeID := list.Items[0].ID
	logoURL := uploadBrandingAsset(t, c, activeID, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`))
	if r := h.Anonymous().Get(logoURL); r.StatusCode != http.StatusOK {
		t.Fatalf("active logo fetch %d", r.StatusCode)
	}

	resp := c.Post("/api/v1/branding/profiles", map[string]any{"name": "copy"})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	var created struct {
		ID      uuid.UUID `json:"id"`
		LogoURL string    `json:"logo_url"`
	}
	c.JSON(resp, &created)
	if created.LogoURL == "" {
		t.Fatal("new profile has no logo_url")
	}
	if created.LogoURL == logoURL {
		t.Fatalf("new profile reused active profile's asset url: %s", created.LogoURL)
	}
	if r := h.Anonymous().Get(created.LogoURL); r.StatusCode != http.StatusOK {
		t.Fatalf("copied logo fetch %d", r.StatusCode)
	}
}

func TestBrandingProfilesListRequiresWriter(t *testing.T) {
	h, _, _ := ownerWithNode(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")
	if resp := viewer.Get("/api/v1/branding/profiles"); resp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("viewer list expected 403, got %d %s", resp.StatusCode, b)
	}
}

func TestBrandingUploadReplacesStaleExtension(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	id := list.Items[0].ID

	svgURL := uploadBrandingAsset(t, c, id, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`))
	r := h.Anonymous().Get(svgURL)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("initial svg fetch %d", r.StatusCode)
	}
	// I4: these headers neutralise SVG XSS even if the sanitiser is ever bypassed. The asset
	// is served publicly, from the panel's origin, on the pre-auth login page.
	if got := r.Header.Get("Content-Security-Policy"); got != "default-src 'none'; style-src 'unsafe-inline'; sandbox" {
		t.Errorf("branding asset CSP = %q", got)
	}
	if got := r.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("branding asset nosniff = %q", got)
	}

	pngURL := uploadBrandingAsset(t, c, id, "logo.png", append([]byte("\x89PNG\r\n\x1a\n"), []byte("not a real png but sniffs as one")...))
	if pngURL == svgURL {
		t.Fatalf("expected a new url after extension change, got same %s", pngURL)
	}
	if r := h.Anonymous().Get(svgURL); r.StatusCode != http.StatusNotFound {
		t.Fatalf("stale svg still served: %d", r.StatusCode)
	}
	if r := h.Anonymous().Get(pngURL); r.StatusCode != http.StatusOK {
		t.Fatalf("new png fetch %d", r.StatusCode)
	}
}

// TestDeleteBrandingReportsZeroRows covers debt item 12: the DELETE carries
// "AND is_active = false", so a profile activated between the handler's
// pre-check and the statement matches no row. The handler now reads the row
// count and answers 409 instead of a misleading 204.
func TestDeleteBrandingReportsZeroRows(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID       uuid.UUID `json:"id"`
			IsActive bool      `json:"is_active"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	var activeID uuid.UUID
	for _, it := range list.Items {
		if it.IsActive {
			activeID = it.ID
		}
	}
	if activeID == uuid.Nil {
		t.Fatal("no active branding profile")
	}

	// The query itself must report the no-op rather than swallowing it.
	rows, err := h.Store.Q.DeleteBranding(t.Context(), activeID)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("deleting the active profile affected %d rows, want 0", rows)
	}

	var created struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON(c.Post("/api/v1/branding/profiles", map[string]any{"name": "spare"}), &created)
	rows, err = h.Store.Q.DeleteBranding(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("deleting an inactive profile affected %d rows, want 1", rows)
	}

	// The handler refuses the active profile outright.
	resp := c.Delete("/api/v1/branding/profiles/" + activeID.String())
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete active profile: %d, want 409", resp.StatusCode)
	}
}

// TestBrandingAssetCopyFailureIsAudited covers debt item 13: clearing the path
// when the copy fails is deliberate, but it used to be invisible.
func TestBrandingAssetCopyFailureIsAudited(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	activeID := list.Items[0].ID
	uploadBrandingAsset(t, c, activeID, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`))

	// Remove the file behind the DB path: the copy on create now fails.
	if err := os.RemoveAll(filepath.Join(h.Deps.Cfg.DataDir, "branding", activeID.String())); err != nil {
		t.Fatal(err)
	}

	var created struct {
		ID      uuid.UUID `json:"id"`
		LogoURL string    `json:"logo_url"`
	}
	c.JSON(c.Post("/api/v1/branding/profiles", map[string]any{"name": "copy"}), &created)
	if created.LogoURL != "" {
		t.Fatalf("logo_url = %q, want it cleared when the copy failed", created.LogoURL)
	}

	var audit struct {
		Items []struct {
			Action string         `json:"action"`
			Meta   map[string]any `json:"meta"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/audit?action=branding.asset_copy_failed"), &audit)
	var found bool
	for _, it := range audit.Items {
		if it.Action == "branding.asset_copy_failed" && it.Meta["kind"] == "logo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no branding.asset_copy_failed audit entry with kind=logo: %+v", audit.Items)
	}
}

// Dark logos use the same validated upload and public serving path as the main
// logo. Cloning a profile must copy the file into the clone's own directory.
func TestBrandingDarkLogoUploadAndClone(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var profiles struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &profiles)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "dark.svg")
	_, _ = fw.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="white"/></svg>`))
	_ = mw.Close()
	response := c.PostRaw("/api/v1/branding/profiles/"+profiles.Items[0].ID.String()+"/upload?kind=logo_dark", mw.FormDataContentType(), body.Bytes())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload: %d", response.StatusCode)
	}
	_ = response.Body.Close()
	var active struct {
		URL string `json:"logo_dark_url"`
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &active)
	if active.URL == "" {
		t.Fatal("dark logo missing from public branding")
	}
	var clone struct {
		URL string `json:"logo_dark_url"`
	}
	c.JSON(c.Post("/api/v1/branding/profiles", map[string]any{"name": "Dark clone"}), &clone)
	if clone.URL == "" || clone.URL == active.URL {
		t.Fatal("clone did not receive its own dark logo")
	}
	for _, url := range []string{active.URL, clone.URL} {
		r := h.Anonymous().Get(url)
		if r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != "image/svg+xml" {
			t.Fatalf("asset unavailable: %s (%d)", url, r.StatusCode)
		}
		_ = r.Body.Close()
	}
}

// A replacement with the same extension must not keep the browser's cached URL.
func TestBrandingUploadVersionsSameExtension(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	var profiles struct {
		Items []struct {
			ID uuid.UUID `json:"id"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &profiles)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`)
	first := uploadBrandingAsset(t, c, profiles.Items[0].ID, "logo.svg", svg)
	second := uploadBrandingAsset(t, c, profiles.Items[0].ID, "logo.svg", svg)
	if first == second {
		t.Fatal("replacement retained cached asset URL")
	}
}
