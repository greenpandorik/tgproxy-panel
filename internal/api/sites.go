package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
)

func (s *Server) mountSites(r chi.Router) {
	r.Get("/site-templates", s.handleListTemplates)
	r.With(RequireRole(writers...)).Post("/site-templates", s.handleCreateTemplate)
	r.With(RequireRole(writers...)).Post("/site-templates/validate", s.handleValidateTemplate)
	r.Get("/site-templates/{id}", s.handleGetTemplate)
	r.With(RequireRole(writers...)).Put("/site-templates/{id}", s.handleUpdateTemplate)
	r.With(RequireRole(writers...)).Delete("/site-templates/{id}", s.handleDeleteTemplate)
}

type templateInput struct {
	Name   string            `json:"name"`
	HTML   string            `json:"html"`
	Assets map[string]string `json:"assets"`
}

func decodeAssets(in map[string]string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for p, b64 := range in {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(p, "/")] = raw
	}
	return out, nil
}

func encodeAssets(in map[string][]byte) map[string]string {
	out := map[string]string{}
	for p, c := range in {
		out[p] = base64.StdEncoding.EncodeToString(c)
	}
	return out
}

func templateJSON(t db.SiteTemplate, full bool) map[string]any {
	out := map[string]any{"id": t.ID, "name": t.Name, "is_preset": t.IsPreset, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt}
	if full {
		var assets map[string]string
		_ = json.Unmarshal(t.Assets, &assets)
		out["html"] = t.Html
		out["assets"] = assets
	}
	return out
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListSiteTemplates(r.Context())
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows, "total": len(rows)})
}

// maxBundleBytes caps a site template's total decoded size (HTML plus every asset). The
// bundle is shipped to nodes inside a single gRPC ApplyRequest; an oversized template would
// make *every* apply for *every* node it is assigned to fail with an opaque
// ResourceExhausted, including applies that were only meant to push a key. Rejecting at
// upload time tells the operator immediately, at the one moment they can still fix it.
const maxBundleBytes = 2 << 20

func bundleSize(html string, assets map[string][]byte) int {
	n := len(html)
	for p, c := range assets {
		n += len(p) + len(c)
	}
	return n
}

// checkBundleSize writes a 422 naming the limit and returns false when the site is too big.
func checkBundleSize(w http.ResponseWriter, html string, assets map[string][]byte) bool {
	n := bundleSize(html, assets)
	if n <= maxBundleBytes {
		return true
	}
	msg := fmt.Sprintf("site is %d bytes decoded; the limit is %d bytes (2 MB) of HTML plus assets", n, maxBundleBytes)
	writeJSON(w, 422, map[string]any{"error": map[string]any{
		"code": "site_too_large", "message": msg, "fields": map[string]string{"assets": msg},
	}})
	return false
}

// validateInput decodes assets and normalises html, writing an error response and returning
// ok=false when the input is malformed, too large, or violates relay restrictions.
func (s *Server) validateInput(w http.ResponseWriter, in templateInput) (assets map[string][]byte, bundle sitekit.Bundle, ok bool) {
	assets, err := decodeAssets(in.Assets)
	if err != nil {
		validation(w, map[string]string{"assets": "values must be base64"})
		return nil, sitekit.Bundle{}, false
	}
	if !checkBundleSize(w, in.HTML, assets) {
		return nil, sitekit.Bundle{}, false
	}
	bundle, rep, err := sitekit.Normalize(in.HTML, assets)
	if err != nil {
		validation(w, map[string]string{"html": "cannot parse HTML"})
		return nil, sitekit.Bundle{}, false
	}
	if len(rep.Errors) > 0 {
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "site_invalid", "message": "site violates relay restrictions", "fields": map[string]string{"html": strings.Join(rep.Errors, "; ")}}, "report": rep})
		return nil, sitekit.Bundle{}, false
	}
	return assets, bundle, true
}

func (s *Server) handleValidateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	assets, err := decodeAssets(in.Assets)
	if err != nil {
		validation(w, map[string]string{"assets": "values must be base64"})
		return
	}
	if !checkBundleSize(w, in.HTML, assets) {
		return
	}
	bundle, rep, err := sitekit.Normalize(in.HTML, assets)
	if err != nil {
		validation(w, map[string]string{"html": "cannot parse HTML"})
		return
	}
	writeJSON(w, 200, map[string]any{"report": rep, "files": bundle.Paths()})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		validation(w, map[string]string{"name": "required"})
		return
	}
	assets, _, ok := s.validateInput(w, in)
	if !ok {
		return
	}
	raw, _ := json.Marshal(encodeAssets(assets))
	t, err := s.store.Q.CreateSiteTemplate(r.Context(), db.CreateSiteTemplateParams{Name: in.Name, Html: in.HTML, Assets: raw, IsPreset: false})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.create", "site_template", t.ID.String(), map[string]any{"name": t.Name})
	writeJSON(w, 201, templateJSON(t, true))
}

func (s *Server) loadTemplate(w http.ResponseWriter, r *http.Request) (db.SiteTemplate, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.SiteTemplate{}, false
	}
	t, err := s.store.Q.GetSiteTemplate(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.SiteTemplate{}, false
	}
	return t, true
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, templateJSON(t, true))
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	var in templateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	assets, _, ok := s.validateInput(w, in)
	if !ok {
		return
	}
	raw, _ := json.Marshal(encodeAssets(assets))
	name := in.Name
	if t.IsPreset {
		name = t.Name
	}
	updated, err := s.store.Q.UpdateSiteTemplate(r.Context(), db.UpdateSiteTemplateParams{ID: t.ID, Name: name, Html: in.HTML, Assets: raw})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.update", "site_template", t.ID.String(), nil)
	writeJSON(w, 200, templateJSON(updated, true))
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	if t.IsPreset {
		conflict(w, "presets cannot be deleted")
		return
	}
	if err := s.store.Q.DeleteSiteTemplate(r.Context(), t.ID); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.delete", "site_template", t.ID.String(), nil)
	w.WriteHeader(204)
}

// --- node site assignment ---

func errorsJoin(errs []string) error { return errors.New(strings.Join(errs, "; ")) }

func (s *Server) renderTemplateBundle(t db.SiteTemplate) (sitekit.Bundle, error) {
	var assets map[string]string
	_ = json.Unmarshal(t.Assets, &assets)
	raw, err := decodeAssets(assets)
	if err != nil {
		return sitekit.Bundle{}, err
	}
	bundle, rep, err := sitekit.Normalize(t.Html, raw)
	if err != nil {
		return sitekit.Bundle{}, err
	}
	if len(rep.Errors) > 0 {
		return sitekit.Bundle{}, errorsJoin(rep.Errors)
	}
	return bundle, nil
}

func (s *Server) handleAssignSite(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	var in struct {
		TemplateID uuid.UUID `json:"template_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	t, err := s.store.Q.GetSiteTemplate(r.Context(), in.TemplateID)
	if err != nil {
		validation(w, map[string]string{"template_id": "unknown template"})
		return
	}
	bundle, err := s.renderTemplateBundle(t)
	if err != nil {
		writeError(w, 422, "site_invalid", err.Error(), nil)
		return
	}
	// Uniquify makes this node's copy of the template byte-different (markup, class names,
	// asset file names) from every other node running the same template, so the site can't be
	// blocklisted by a single static fingerprint. It is deterministic per node ID: re-assigning
	// the same template to a node that already runs it reproduces the identical bundle, so it
	// never looks like a change and never forces a needless relay restart.
	bundle, err = sitekit.Uniquify(bundle, n.ID.String())
	if err != nil {
		internal(w)
		return
	}
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		if err := q.UpsertNodeSite(r.Context(), db.UpsertNodeSiteParams{NodeID: n.ID, TemplateID: uuid.NullUUID{UUID: t.ID, Valid: true}, Bundle: bundle.JSON(), BundleHash: bundle.Hash()}); err != nil {
			return err
		}
		return q.SetNodeDirty(r.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: true})
	})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.site_assign", "node", n.ID.String(), map[string]any{"template_id": t.ID})
	s.handleGetNodeSite(w, r)
}

func (s *Server) handleGetNodeSite(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	site, err := s.store.Q.GetNodeSite(r.Context(), n.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"template_id": nil, "bundle_hash": "", "deployed_hash": nil, "files": []string{}})
		return
	}
	bundle, _ := sitekit.BundleFromJSON(site.Bundle)
	writeJSON(w, 200, map[string]any{"template_id": site.TemplateID, "bundle_hash": site.BundleHash, "deployed_hash": site.DeployedHash, "files": bundle.Paths(), "updated_at": site.UpdatedAt})
}

// handleSitePreview inlines the bundle's stylesheets so the admin can preview without deploying.
func (s *Server) handleSitePreview(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	site, err := s.store.Q.GetNodeSite(r.Context(), n.ID)
	if err != nil {
		notFound(w)
		return
	}
	bundle, err := sitekit.BundleFromJSON(site.Bundle)
	if err != nil {
		internal(w)
		return
	}
	page := string(bundle.Files["index.html"])
	for p, c := range bundle.Files {
		if strings.HasSuffix(p, ".css") {
			page = strings.Replace(page, `<link rel="stylesheet" href="/`+p+`"/>`, "<style>"+string(c)+"</style>", 1)
			page = strings.Replace(page, `<link rel="stylesheet" href="/`+p+`">`, "<style>"+string(c)+"</style>", 1)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The one route that is *meant* to be framed: the node Site tab renders it in
	// a same-origin, fully sandboxed <iframe> (web/src/pages/nodes/NodeSiteTab.tsx).
	// The router sets X-Frame-Options: DENY on everything (server.go, denyFraming),
	// which would blank the preview, so both framing rules are narrowed here to
	// "our own origin and nobody else's" - written after the middleware ran, which
	// is what makes this a narrowing rather than a hole. The rest of the CSP keeps
	// the preview unable to load or run anything of its own.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'self'")
	_, _ = w.Write([]byte(page))
}

// NodeSiteFiles is the SiteProvider used by the install script.
func (s *Server) NodeSiteFiles(ctx context.Context, nodeID uuid.UUID) (map[string][]byte, error) {
	site, err := s.store.Q.GetNodeSite(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	b, err := sitekit.BundleFromJSON(site.Bundle)
	if err != nil {
		return nil, err
	}
	return b.Files, nil
}
