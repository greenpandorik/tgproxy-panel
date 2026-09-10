package api_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
)

const goodHTML = `<!doctype html><html><head><title>t</title><style>p{color:#333}</style></head><body><p>hello</p></body></html>`

func TestSiteTemplateCRUDAndValidate(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID       uuid.UUID `json:"id"`
			Name     string    `json:"name"`
			IsPreset bool      `json:"is_preset"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/site-templates"), &list)
	wantPresets := sitekit.PresetNames()
	if len(list.Items) != len(wantPresets) {
		t.Fatalf("presets not seeded: %+v", list.Items)
	}
	for _, it := range list.Items {
		if !it.IsPreset || !wantPresets[it.Name] {
			t.Fatalf("presets not seeded: %+v", list.Items)
		}
	}
	var val struct {
		Report struct {
			Errors   []string `json:"errors"`
			Warnings []string `json:"warnings"`
		} `json:"report"`
	}
	c.JSON(c.Post("/api/v1/site-templates/validate", map[string]any{"html": `<html><body><a onclick="x()">a</a></body></html>`}), &val)
	if len(val.Report.Errors) == 0 {
		t.Fatal("validate must report errors")
	}
	resp := c.Post("/api/v1/site-templates", map[string]any{"name": "bad", "html": `<html><body><script src="https://x/y.js"></script></body></html>`})
	if resp.StatusCode != 422 {
		t.Fatalf("bad template expected 422, got %d", resp.StatusCode)
	}
	var tpl struct {
		ID   uuid.UUID `json:"id"`
		HTML string    `json:"html"`
	}
	resp = c.Post("/api/v1/site-templates", map[string]any{"name": "mine", "html": goodHTML})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &tpl)
	if !strings.Contains(tpl.HTML, "<style>") {
		t.Fatal("stored html should keep the author's source")
	}
	if resp := c.Delete("/api/v1/site-templates/" + list.Items[0].ID.String()); resp.StatusCode != 409 {
		t.Fatalf("preset delete expected 409, got %d", resp.StatusCode)
	}
	if resp := c.Delete("/api/v1/site-templates/" + tpl.ID.String()); resp.StatusCode != 204 {
		t.Fatalf("delete %d", resp.StatusCode)
	}
}

func TestAssignSiteToNode(t *testing.T) {
	h, c, n := ownerWithNode(t)
	var tpl struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON(c.Post("/api/v1/site-templates", map[string]any{"name": "mine", "html": goodHTML}), &tpl)
	resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/site", map[string]any{"template_id": tpl.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("assign %d", resp.StatusCode)
	}
	var site struct {
		BundleHash   string   `json:"bundle_hash"`
		DeployedHash *string  `json:"deployed_hash"`
		Files        []string `json:"files"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/site"), &site)
	if site.BundleHash == "" || site.DeployedHash != nil || len(site.Files) != 2 {
		t.Fatalf("site %+v", site)
	}
	var got nodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if !got.Dirty {
		t.Fatal("assign must mark node dirty")
	}
	prev := c.Get("/api/v1/nodes/" + n.ID.String() + "/site/preview")
	body, _ := io.ReadAll(prev.Body)
	if prev.StatusCode != 200 || !strings.Contains(string(body), "color:#333") {
		t.Fatalf("preview %d: %s", prev.StatusCode, body)
	}
	// install script now embeds the assigned site
	_, cmd := createNode(t, c, "n2.test")
	_ = cmd
	files, err := h.Deps.SiteProvider(t.Context(), n.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("site provider %v %d", err, len(files))
	}
}

func TestAssignPrivateHTTPWebsiteToTelemtNode(t *testing.T) {
	h, c, n := ownerWithNode(t)
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{
		ID: n.ID, Status: db.NodeStatusOnline, TelemtCapabilities: []byte(`{"HttpUpstreamDecoy":true}`),
	}); err != nil {
		t.Fatal(err)
	}
	resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/site/upstream", map[string]any{"origin": "http://127.0.0.1:3000"})
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("assign upstream %d: %s", resp.StatusCode, body)
	}
	var site struct {
		Mode   string   `json:"mode"`
		Origin string   `json:"origin"`
		Files  []string `json:"files"`
	}
	c.JSON(resp, &site)
	if site.Mode != "upstream" || site.Origin != "http://127.0.0.1:3000" || len(site.Files) != 0 {
		t.Fatalf("upstream site response: %+v", site)
	}
	files, err := h.Deps.SiteProvider(t.Context(), n.ID)
	if err != nil || files != nil {
		t.Fatalf("installer must use its static fallback for an upstream assignment: %v %#v", err, files)
	}
	if preview := c.Get("/api/v1/nodes/" + n.ID.String() + "/site/preview"); preview.StatusCode != http.StatusConflict {
		t.Fatalf("upstream preview status %d", preview.StatusCode)
	}
	unsafe := c.Post("/api/v1/nodes/"+n.ID.String()+"/site/upstream", map[string]any{"origin": "http://8.8.8.8:3000"})
	if unsafe.StatusCode != 422 {
		t.Fatalf("public origin status %d", unsafe.StatusCode)
	}
}

func TestAssignSiteUniquifiesPerNode(t *testing.T) {
	_, c, n1 := ownerWithNode(t)
	n2, _ := createNode(t, c, "n2.test")

	var tpl struct {
		ID uuid.UUID `json:"id"`
	}
	c.JSON(c.Post("/api/v1/site-templates", map[string]any{"name": "mine", "html": goodHTML}), &tpl)

	var siteResp struct {
		BundleHash string `json:"bundle_hash"`
	}

	resp := c.Post("/api/v1/nodes/"+n1.ID.String()+"/site", map[string]any{"template_id": tpl.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("assign n1 %d", resp.StatusCode)
	}
	c.JSON(resp, &siteResp)
	hash1 := siteResp.BundleHash

	resp = c.Post("/api/v1/nodes/"+n2.ID.String()+"/site", map[string]any{"template_id": tpl.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("assign n2 %d", resp.StatusCode)
	}
	c.JSON(resp, &siteResp)
	hash2 := siteResp.BundleHash

	if hash1 == "" || hash2 == "" {
		t.Fatalf("empty bundle hash: %q %q", hash1, hash2)
	}
	if hash1 == hash2 {
		t.Fatal("two different nodes got the same bundle_hash; uniquification not applied")
	}

	resp = c.Post("/api/v1/nodes/"+n1.ID.String()+"/site", map[string]any{"template_id": tpl.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("re-assign n1 %d", resp.StatusCode)
	}
	c.JSON(resp, &siteResp)
	if siteResp.BundleHash != hash1 {
		t.Fatalf("re-assigning the same template to the same node changed bundle_hash: %q != %q", siteResp.BundleHash, hash1)
	}
}

func TestSiteTemplateSizeCap(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), (2<<20)+4096))
	body := map[string]any{"name": "huge", "html": goodHTML, "assets": map[string]string{"big.css": big}}

	for _, path := range []string{"/api/v1/site-templates", "/api/v1/site-templates/validate"} {
		resp := c.Post(path, body)
		if resp.StatusCode != 422 {
			t.Fatalf("%s: expected 422, got %d", path, resp.StatusCode)
		}
		var out struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		c.JSON(resp, &out)
		if out.Error.Code != "site_too_large" || !strings.Contains(out.Error.Message, "2 MB") {
			t.Fatalf("%s: error %+v must name the limit", path, out.Error)
		}
	}

	// A bundle under the cap is still accepted.
	small := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 1024))
	ok := c.Post("/api/v1/site-templates", map[string]any{"name": "small", "html": goodHTML, "assets": map[string]string{"s.css": small}})
	if ok.StatusCode != 201 {
		b, _ := io.ReadAll(ok.Body)
		t.Fatalf("small template rejected: %d %s", ok.StatusCode, b)
	}
}
