package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/branding"
	"tgwebproxy/internal/store/db"
)

// maxBrandingAssetSize is the upload limit for logo/favicon/login background files.
const maxBrandingAssetSize = 512 << 10

// brandingAssetKinds are the upload slots a branding profile exposes.
var brandingAssetKinds = map[string]bool{"logo": true, "logo_dark": true, "favicon": true, "login_bg": true}

func (s *Server) mountBranding(r chi.Router) {
	r.With(RequireRole(writers...)).Get("/branding/profiles", s.handleListBrandingProfiles)
	r.With(RequireRole(writers...)).Post("/branding/profiles", s.handleCreateBrandingProfile)
	r.With(RequireRole(writers...)).Put("/branding/profiles/{id}", s.handleUpdateBrandingProfile)
	r.With(RequireRole(writers...)).Delete("/branding/profiles/{id}", s.handleDeleteBrandingProfile)
	r.With(RequireRole(writers...)).Post("/branding/profiles/{id}/activate", s.handleActivateBrandingProfile)
	r.With(RequireRole(writers...)).Post("/branding/profiles/{id}/upload", s.handleUploadBrandingAsset)
}

// brandingAssetURL maps a stored relative path to its public asset URL, or "" when unset.
func brandingAssetURL(id uuid.UUID, path string) string {
	if path == "" {
		return ""
	}
	return "/api/v1/branding/assets/" + id.String() + "/" + filepath.Base(path)
}

// copyBrandingAssetFile copies an asset file from the source profile's directory into the
// destination profile's directory, returning the same relative path on success or "" when
// the source file is missing (or the copy otherwise fails), so callers can clear the path
// rather than leave a dangling reference.
func (s *Server) copyBrandingAssetFile(ctx context.Context, srcID, dstID uuid.UUID, kind, path string) string {
	if path == "" {
		return ""
	}
	// Clearing the path is a deliberate degradation (better than a dangling
	// reference), but it is invisible in the UI, so it is audited with the kind.
	fail := func(err error) string {
		s.Audit(ctx, "branding.asset_copy_failed", "branding_profile", dstID.String(),
			map[string]any{"kind": kind, "source_profile_id": srcID.String(), "error": err.Error()})
		return ""
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "branding", srcID.String(), path))
	if err != nil {
		return fail(err)
	}
	dstDir := filepath.Join(s.cfg.DataDir, "branding", dstID.String())
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, path), data, 0o644); err != nil {
		return fail(err)
	}
	return path
}

// publicBrandingJSON is the shape served by the unauthenticated GET /branding endpoint.
func publicBrandingJSON(b db.BrandingProfile) map[string]any {
	// Replacing a logo keeps its filename; version the URL so browsers show
	// the new upload immediately despite the public asset cache.
	assetURL := func(path string) string {
		url := brandingAssetURL(b.ID, path)
		if url == "" {
			return ""
		}
		return url + "?v=" + strconv.FormatInt(b.UpdatedAt.UnixNano(), 10)
	}
	return map[string]any{
		"panel_name":    b.PanelName,
		"logo_url":      assetURL(b.LogoPath),
		"logo_dark_url": assetURL(b.LogoDarkPath),
		"favicon_url":   assetURL(b.FaviconPath),
		"primary_color": b.PrimaryColor,
		"accent_color":  b.AccentColor,
		"theme_default": b.ThemeDefault,
		"login_bg_url":  assetURL(b.LoginBgPath),
		"login_text":    b.LoginText,
		"support_link":  b.SupportLink,
		"footer_text":   b.FooterText,
		"custom_css":    b.CustomCss,
	}
}

// brandingJSON is the full shape served by the protected profile endpoints.
func brandingJSON(b db.BrandingProfile) map[string]any {
	out := publicBrandingJSON(b)
	out["id"] = b.ID
	out["name"] = b.Name
	out["is_active"] = b.IsActive
	out["updated_at"] = b.UpdatedAt
	return out
}

func (s *Server) handleGetActiveBranding(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.Q.GetActiveBranding(r.Context())
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, publicBrandingJSON(b))
}

func (s *Server) loadBrandingProfile(w http.ResponseWriter, r *http.Request) (db.BrandingProfile, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.BrandingProfile{}, false
	}
	b, err := s.store.Q.GetBranding(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.BrandingProfile{}, false
	}
	return b, true
}

func (s *Server) handleListBrandingProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListBranding(r.Context())
	if err != nil {
		internal(w)
		return
	}
	items := make([]map[string]any, len(rows))
	for i, b := range rows {
		items[i] = brandingJSON(b)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleCreateBrandingProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		validation(w, map[string]string{"name": "required"})
		return
	}
	active, err := s.store.Q.GetActiveBranding(r.Context())
	if err != nil {
		internal(w)
		return
	}
	created, err := s.store.Q.CreateBranding(r.Context(), db.CreateBrandingParams{
		Name:         in.Name,
		PanelName:    active.PanelName,
		LogoPath:     active.LogoPath,
		FaviconPath:  active.FaviconPath,
		PrimaryColor: active.PrimaryColor,
		AccentColor:  active.AccentColor,
		ThemeDefault: active.ThemeDefault,
		LoginBgPath:  active.LoginBgPath,
		LoginText:    active.LoginText,
		SupportLink:  active.SupportLink,
		FooterText:   active.FooterText,
		CustomCss:    active.CustomCss,
	})
	if err != nil {
		internal(w)
		return
	}
	// CreateBranding copied the active profile's asset *paths*, but the files themselves
	// live under the active profile's own directory. Copy each file into the new profile's
	// directory, or clear the path when the source file is missing.
	for _, kv := range []struct{ kind, path string }{
		{"logo", active.LogoPath},
		{"logo_dark", active.LogoDarkPath},
		{"favicon", active.FaviconPath},
		{"login_bg", active.LoginBgPath},
	} {
		newPath := s.copyBrandingAssetFile(r.Context(), active.ID, created.ID, kv.kind, kv.path)
		if err := s.store.Q.SetBrandingAsset(r.Context(), db.SetBrandingAssetParams{ID: created.ID, Column2: kv.kind, LogoPath: newPath}); err != nil {
			internal(w)
			return
		}
	}
	created, err = s.store.Q.GetBranding(r.Context(), created.ID)
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "branding.create", "branding_profile", created.ID.String(), map[string]any{"name": created.Name})
	writeJSON(w, 201, brandingJSON(created))
}

func (s *Server) handleUpdateBrandingProfile(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadBrandingProfile(w, r)
	if !ok {
		return
	}
	var in struct {
		Name         string `json:"name"`
		PanelName    string `json:"panel_name"`
		PrimaryColor string `json:"primary_color"`
		AccentColor  string `json:"accent_color"`
		ThemeDefault string `json:"theme_default"`
		LoginText    string `json:"login_text"`
		SupportLink  string `json:"support_link"`
		FooterText   string `json:"footer_text"`
		CustomCSS    string `json:"custom_css"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	fields := map[string]string{}
	if strings.TrimSpace(in.Name) == "" {
		fields["name"] = "required"
	}
	if !branding.ValidateColor(in.PrimaryColor) {
		fields["primary_color"] = "must be a #rgb or #rrggbb hex color"
	}
	if !branding.ValidateColor(in.AccentColor) {
		fields["accent_color"] = "must be a #rgb or #rrggbb hex color"
	}
	if in.ThemeDefault != "dark" && in.ThemeDefault != "light" {
		fields["theme_default"] = "must be dark or light"
	}
	if in.SupportLink != "" && !strings.HasPrefix(in.SupportLink, "http://") && !strings.HasPrefix(in.SupportLink, "https://") {
		fields["support_link"] = "must be empty or an http(s) URL"
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	css, removed := branding.SanitizeCSS(in.CustomCSS)
	if removed == nil {
		removed = []string{}
	}
	updated, err := s.store.Q.UpdateBranding(r.Context(), db.UpdateBrandingParams{
		ID:           b.ID,
		Name:         in.Name,
		PanelName:    in.PanelName,
		PrimaryColor: in.PrimaryColor,
		AccentColor:  in.AccentColor,
		ThemeDefault: in.ThemeDefault,
		LoginText:    in.LoginText,
		SupportLink:  in.SupportLink,
		FooterText:   in.FooterText,
		CustomCss:    css,
	})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "branding.update", "branding_profile", updated.ID.String(), map[string]any{"name": updated.Name, "css_removed": removed})
	out := brandingJSON(updated)
	out["css_removed"] = removed
	writeJSON(w, 200, out)
}

func (s *Server) handleActivateBrandingProfile(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadBrandingProfile(w, r)
	if !ok {
		return
	}
	err := s.store.Tx(r.Context(), func(q *db.Queries) error {
		if err := q.DeactivateBranding(r.Context()); err != nil {
			return err
		}
		return q.ActivateBrandingOne(r.Context(), b.ID)
	})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "branding.activate", "branding_profile", b.ID.String(), map[string]any{"name": b.Name})
	updated, err := s.store.Q.GetBranding(r.Context(), b.ID)
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, brandingJSON(updated))
}

func (s *Server) handleDeleteBrandingProfile(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadBrandingProfile(w, r)
	if !ok {
		return
	}
	if b.IsActive {
		conflict(w, "the active branding profile cannot be deleted")
		return
	}
	rows, err := s.store.Q.DeleteBranding(r.Context(), b.ID)
	if err != nil {
		internal(w)
		return
	}
	if rows == 0 {
		// Someone activated this profile between the pre-check above and the DELETE.
		conflict(w, "the active branding profile cannot be deleted")
		return
	}
	s.Audit(r.Context(), "branding.delete", "branding_profile", b.ID.String(), map[string]any{"name": b.Name})
	w.WriteHeader(204)
}

// detectBrandingExt sniffs the upload's content type and returns the extension to store it
// under, or ok=false when the type is not one of the allowed image formats.
func detectBrandingExt(filename string, data []byte) (ext string, ok bool) {
	ct := http.DetectContentType(data)
	switch {
	case ct == "image/png":
		return ".png", true
	case ct == "image/jpeg":
		return ".jpg", true
	case ct == "image/x-icon", ct == "image/vnd.microsoft.icon":
		return ".ico", true
	case strings.HasPrefix(ct, "text/xml"), strings.HasPrefix(ct, "text/plain"):
		if strings.HasSuffix(strings.ToLower(filename), ".svg") {
			return ".svg", true
		}
	}
	return "", false
}

func (s *Server) handleUploadBrandingAsset(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadBrandingProfile(w, r)
	if !ok {
		return
	}
	kind := r.URL.Query().Get("kind")
	if !brandingAssetKinds[kind] {
		validation(w, map[string]string{"kind": "must be logo, logo_dark, favicon or login_bg"})
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		badRequest(w, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		validation(w, map[string]string{"file": "required"})
		return
	}
	defer file.Close() //nolint:errcheck
	if header.Size > maxBrandingAssetSize {
		validation(w, map[string]string{"file": "must be 512KB or smaller"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBrandingAssetSize+1))
	if err != nil {
		internal(w)
		return
	}
	if len(data) > maxBrandingAssetSize {
		validation(w, map[string]string{"file": "must be 512KB or smaller"})
		return
	}
	ext, ok := detectBrandingExt(header.Filename, data)
	if !ok {
		validation(w, map[string]string{"file": "must be png, jpeg, ico or svg"})
		return
	}
	if ext == ".svg" {
		clean, err := branding.SanitizeSVG(data)
		if err != nil {
			validation(w, map[string]string{"file": err.Error()})
			return
		}
		data = clean
	}
	dir := filepath.Join(s.cfg.DataDir, "branding", b.ID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internal(w)
		return
	}
	// A previous upload for this kind may have used a different extension (e.g. logo.svg
	// replaced by logo.png); remove any stale sibling files so they stop being served.
	if stale, err := filepath.Glob(filepath.Join(dir, kind+".*")); err == nil {
		for _, f := range stale {
			_ = os.Remove(f)
		}
	}
	rel := kind + ext
	if err := os.WriteFile(filepath.Join(dir, rel), data, 0o644); err != nil {
		internal(w)
		return
	}
	if err := s.store.Q.SetBrandingAsset(r.Context(), db.SetBrandingAssetParams{ID: b.ID, Column2: kind, LogoPath: rel}); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "branding.upload", "branding_profile", b.ID.String(), map[string]any{"kind": kind})
	updated, err := s.store.Q.GetBranding(r.Context(), b.ID)
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, brandingJSON(updated))
}

var brandingAssetContentTypes = map[string]string{
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".ico":  "image/x-icon",
}

// handleBrandingAsset serves an uploaded branding file publicly. filepath.Base defuses path
// traversal in the file segment and uuid.Parse rejects any non-UUID id segment outright.
func (s *Server) handleBrandingAsset(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return
	}
	file := filepath.Base(chi.URLParam(r, "file"))
	if file == "." || file == string(filepath.Separator) {
		notFound(w)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "branding", id.String(), file))
	if err != nil {
		notFound(w)
		return
	}
	ct := brandingAssetContentTypes[strings.ToLower(filepath.Ext(file))]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	// Branding assets are operator uploads served publicly from the panel's own origin, and
	// the login page renders them before authentication. These two headers neutralise the
	// whole SVG-XSS class regardless of how good the sanitiser is: the CSP denies the
	// document every capability (no scripts, no fetches, no framing) and sandboxes it into a
	// unique opaque origin, and nosniff stops a mislabelled upload being re-interpreted.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
