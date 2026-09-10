package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
)

func (s *Server) handleImportWebsite(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBundleBytes+(64<<10))
	if err := r.ParseMultipartForm(maxBundleBytes); err != nil {
		validation(w, map[string]string{"file": "invalid or oversized ZIP"})
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		validation(w, map[string]string{"name": "required"})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		validation(w, map[string]string{"file": "ZIP required"})
		return
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxBundleBytes+1))
	if err != nil {
		badRequest(w, "cannot read ZIP")
		return
	}
	html, assets, err := sitekit.ImportZIP(raw, maxBundleBytes)
	if err != nil {
		validation(w, map[string]string{"file": err.Error()})
		return
	}
	_, _, ok := s.validateInput(w, templateInput{Name: name, HTML: html, Assets: encodeAssets(assets)})
	if !ok {
		return
	}
	encoded, _ := json.Marshal(encodeAssets(assets))
	tpl, err := s.store.Q.CreateSiteTemplate(r.Context(), db.CreateSiteTemplateParams{Name: name, Html: html, Assets: encoded, IsPreset: false})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.import", "site_template", tpl.ID.String(), map[string]any{"name": name})
	writeJSON(w, http.StatusCreated, templateJSON(tpl, true))
}

func (s *Server) handleCustomizeWebsite(w http.ResponseWriter, r *http.Request) {
	tpl, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	var body struct {
		Name      string            `json:"name"`
		Variables map[string]string `json:"variables"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		validation(w, map[string]string{"name": "required"})
		return
	}
	var defaults map[string]string
	if tpl.IsPreset {
		for _, p := range sitekit.Presets() {
			if p.Name == tpl.Name {
				defaults = p.Manifest.Variables
			}
		}
	}
	if defaults == nil {
		conflict(w, "only built-in websites have editable variables; use the HTML editor for custom websites")
		return
	}
	source, err := sitekit.Customize(tpl.Html, defaults, body.Variables)
	if err != nil {
		validation(w, map[string]string{"variables": err.Error()})
		return
	}
	var assets map[string]string
	_ = json.Unmarshal(tpl.Assets, &assets)
	_, _, valid := s.validateInput(w, templateInput{Name: body.Name, HTML: source, Assets: assets})
	if !valid {
		return
	}
	created, err := s.store.Q.CreateSiteTemplate(r.Context(), db.CreateSiteTemplateParams{Name: body.Name, Html: source, Assets: tpl.Assets, IsPreset: false})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.customize", "site_template", created.ID.String(), nil)
	writeJSON(w, http.StatusCreated, templateJSON(created, true))
}
