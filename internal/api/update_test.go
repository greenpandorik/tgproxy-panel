package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tgwebproxy/internal/api"
	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/updates"
	"tgwebproxy/internal/version"
)

type updateStatusResp struct {
	Enabled         bool   `json:"enabled"`
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	LatestURL       string `json:"latest_url"`
	PublishedAt     string `json:"published_at"`
	Stars           int    `json:"stars"`
	RepoURL         string `json:"repo_url"`
	UpdateAvailable bool   `json:"update_available"`
	CheckedAt       string `json:"checked_at"`
	Stale           bool   `json:"stale"`
}

// fakeGitHub serves the two GitHub endpoints the checker reads, for one repo.
func fakeGitHub(t *testing.T, tag string, stars int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/panel/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag, "html_url": "https://github.com/acme/panel/releases/tag/" + tag,
			"published_at": "2026-09-06T10:00:00Z",
		})
	})
	mux.HandleFunc("/repos/acme/panel", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"stargazers_count": stars})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestUpdateStatusForEveryRole(t *testing.T) {
	gh := fakeGitHub(t, "v99.0.0", 6096)
	c := updates.New("acme/panel", "")
	c.BaseURL = gh.URL
	h := apitest.New(t, apitest.WithUpdates(c))
	h.CreateAdmin("v", "pass-123456", "viewer")
	viewer := h.Login("v", "pass-123456")

	resp := viewer.Get("/api/v1/status/update")
	if resp.StatusCode != 200 {
		t.Fatalf("viewer: %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=300" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	var got updateStatusResp
	viewer.JSON(resp, &got)
	if !got.Enabled || got.Stale {
		t.Fatalf("enabled=%v stale=%v: %+v", got.Enabled, got.Stale, got)
	}
	if got.Current != version.Version || got.Latest != "99.0.0" || !got.UpdateAvailable {
		t.Fatalf("current=%q latest=%q update_available=%v", got.Current, got.Latest, got.UpdateAvailable)
	}
	if got.Stars != 6096 || got.RepoURL != "https://github.com/acme/panel" {
		t.Fatalf("stars=%d repo_url=%q", got.Stars, got.RepoURL)
	}
	if got.LatestURL != "https://github.com/acme/panel/releases/tag/v99.0.0" || got.PublishedAt != "2026-09-06T10:00:00Z" || got.CheckedAt == "" {
		t.Fatalf("release metadata: %+v", got)
	}

	if resp := h.Anonymous().Get("/api/v1/status/update"); resp.StatusCode != 401 {
		t.Fatalf("anonymous: %d, want 401", resp.StatusCode)
	}
}

// failingTransport turns any outbound request into a test failure.
type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("UPDATE_CHECK=false but an outbound request was made: %s %s", r.Method, r.URL)
	return nil, errors.New("outbound call with update check disabled")
}

func TestUpdateStatusDisabledMakesNoCalls(t *testing.T) {
	c := updates.New("acme/panel", "")
	c.HTTP = &http.Client{Transport: failingTransport{t}}
	h := apitest.New(t, func(d *api.Deps) {
		d.Updates = c
		d.Cfg.UpdateCheck = false
		d.Cfg.GitHubRepo = "acme/panel"
	})
	h.CreateAdmin("root", "pass-123456", "owner")
	owner := h.Login("root", "pass-123456")

	resp := owner.Get("/api/v1/status/update")
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got updateStatusResp
	owner.JSON(resp, &got)
	if got.Enabled {
		t.Fatalf("enabled with UPDATE_CHECK=false: %+v", got)
	}
	if got.Current != version.Version || got.RepoURL != "https://github.com/acme/panel" {
		t.Fatalf("current/repo_url: %+v", got)
	}
	if got.Latest != "" || got.LatestURL != "" || got.PublishedAt != "" || got.UpdateAvailable || got.CheckedAt != "" || got.Stale {
		t.Fatalf("disabled response carries data: %+v", got)
	}
}
