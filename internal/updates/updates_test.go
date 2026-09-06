package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// github is a fake api.github.com serving the two endpoints the checker reads.
// Both handlers count hits and can be switched to a failure status; block, when
// set, makes them wait until it is closed (for the single-flight test).
type github struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	releaseHits   int
	repoHits      int
	releaseStatus int
	repoStatus    int
	tag           string
	stars         int
	block         chan struct{}
	lastHeaders   http.Header
}

func newGitHub(t *testing.T) *github {
	t.Helper()
	g := &github{t: t, releaseStatus: 200, repoStatus: 200, tag: "v1.0.1", stars: 42}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/panel/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.releaseHits++
		g.lastHeaders = r.Header.Clone()
		status, tag, block := g.releaseStatus, g.tag, g.block
		g.mu.Unlock()
		if block != nil {
			<-block
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     tag,
			"html_url":     "https://github.com/acme/panel/releases/tag/" + tag,
			"published_at": "2026-09-06T10:00:00Z",
		})
	})
	mux.HandleFunc("/repos/acme/panel", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.repoHits++
		status, stars, block := g.repoStatus, g.stars, g.block
		g.mu.Unlock()
		if block != nil {
			<-block
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"stargazers_count": stars})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func (g *github) hits() (int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.releaseHits, g.repoHits
}

func (g *github) set(fn func(g *github)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fn(g)
}

// clock is a settable time source for the checker.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newChecker(g *github, current string) (*Checker, *clock) {
	ck := &clock{t: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
	c := New("acme/panel", "tok-secret")
	c.BaseURL = g.srv.URL
	c.Current = current
	c.Now = ck.now
	return c, ck
}

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.0.1", "1.0.0", true},
		{"v1.0.1", "1.0.0", true},
		{"v1.0.1", "v1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
		{"1.0.0", "dev", false},
		{"", "1.0.0", false},
		{"1.0", "1.0.0", false},
		{"latest", "1.0.0", false},
		{"1.0.0-rc1", "1.0.0", false},
		{"1.0.0", "1.0.0-rc1", true},
		{"1.0.1-rc1", "1.0.0", true},
		{"1.0.0-rc2", "1.0.0-rc1", true},
		{"1.0.0-rc1", "1.0.0-rc1", false},
	}
	for _, tc := range cases {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestStatusNewerTag(t *testing.T) {
	g := newGitHub(t)
	c, _ := newChecker(g, "1.0.0")
	st := c.Status(context.Background())

	if !st.Enabled || st.Stale {
		t.Fatalf("enabled=%v stale=%v, want enabled and fresh", st.Enabled, st.Stale)
	}
	if st.Current != "1.0.0" || st.Latest != "1.0.1" || !st.UpdateAvailable {
		t.Fatalf("current=%q latest=%q update_available=%v", st.Current, st.Latest, st.UpdateAvailable)
	}
	if st.LatestURL != "https://github.com/acme/panel/releases/tag/v1.0.1" {
		t.Fatalf("latest_url = %q", st.LatestURL)
	}
	if st.PublishedAt != "2026-09-06T10:00:00Z" || st.Stars != 42 {
		t.Fatalf("published_at=%q stars=%d", st.PublishedAt, st.Stars)
	}
	if st.RepoURL != "https://github.com/acme/panel" {
		t.Fatalf("repo_url = %q", st.RepoURL)
	}
	if st.CheckedAt != "2026-09-06T12:00:00Z" {
		t.Fatalf("checked_at = %q", st.CheckedAt)
	}

	g.mu.Lock()
	h := g.lastHeaders
	g.mu.Unlock()
	if h.Get("Accept") != "application/vnd.github+json" {
		t.Errorf("Accept = %q", h.Get("Accept"))
	}
	if h.Get("X-GitHub-Api-Version") != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q", h.Get("X-GitHub-Api-Version"))
	}
	if h.Get("Authorization") != "Bearer tok-secret" {
		t.Errorf("Authorization = %q", h.Get("Authorization"))
	}
	if !strings.HasPrefix(h.Get("User-Agent"), "tgproxy-panel/") {
		t.Errorf("User-Agent = %q", h.Get("User-Agent"))
	}
}

func TestStatusNoTokenSendsNoAuthorization(t *testing.T) {
	g := newGitHub(t)
	c, _ := newChecker(g, "1.0.0")
	c.Token = ""
	_ = c.Status(context.Background())
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.lastHeaders["Authorization"]; ok {
		t.Fatal("Authorization header sent without a token")
	}
}

func TestStatusEqualVersion(t *testing.T) {
	g := newGitHub(t)
	c, _ := newChecker(g, "1.0.1")
	st := c.Status(context.Background())
	if st.UpdateAvailable {
		t.Fatalf("equal versions reported as update: %+v", st)
	}
	if st.Latest != "1.0.1" {
		t.Fatalf("latest = %q", st.Latest)
	}
}

func TestStatusDevBuildNeverOutdated(t *testing.T) {
	g := newGitHub(t)
	c, _ := newChecker(g, "dev")
	st := c.Status(context.Background())
	if st.UpdateAvailable {
		t.Fatalf("dev build reported as outdated: %+v", st)
	}
	if st.Current != "dev" || st.Latest != "1.0.1" || st.Stars != 42 {
		t.Fatalf("dev build must still carry repo metadata: %+v", st)
	}
}

func TestStatusRateLimitedOnFirstFetch(t *testing.T) {
	g := newGitHub(t)
	g.set(func(g *github) { g.releaseStatus = 403; g.repoStatus = 403 })
	c, _ := newChecker(g, "1.0.0")
	st := c.Status(context.Background())
	if !st.Stale {
		t.Fatalf("failed fetch must be stale: %+v", st)
	}
	if st.Stars != -1 || st.Latest != "" || st.LatestURL != "" || st.PublishedAt != "" || st.CheckedAt != "" {
		t.Fatalf("unknown values must be sentinels: %+v", st)
	}
	if st.UpdateAvailable {
		t.Fatal("update_available with no known latest")
	}
	if st.Current != "1.0.0" || st.RepoURL != "https://github.com/acme/panel" || !st.Enabled {
		t.Fatalf("current/repo_url/enabled must survive a failure: %+v", st)
	}
}

func TestStatusFailureAfterSuccessKeepsLastGood(t *testing.T) {
	g := newGitHub(t)
	c, ck := newChecker(g, "1.0.0")
	first := c.Status(context.Background())
	if first.Stale || first.Latest != "1.0.1" {
		t.Fatalf("first fetch: %+v", first)
	}

	// Cache is fresh: no second call before TTL.
	ck.advance(30 * time.Minute)
	_ = c.Status(context.Background())
	if r, s := g.hits(); r != 1 || s != 1 {
		t.Fatalf("fresh cache refetched: releases=%d repo=%d", r, s)
	}

	// TTL expired and GitHub is now rate limiting: previous numbers + stale.
	g.set(func(g *github) { g.releaseStatus = 403; g.tag = "v9.9.9"; g.stars = 99 })
	ck.advance(31 * time.Minute)
	st := c.Status(context.Background())
	if !st.Stale {
		t.Fatalf("failure after success must be stale: %+v", st)
	}
	if st.Latest != "1.0.1" || st.Stars != 42 || !st.UpdateAvailable || st.CheckedAt != first.CheckedAt {
		t.Fatalf("last good values not kept: %+v", st)
	}
	if r, _ := g.hits(); r != 2 {
		t.Fatalf("expected a refetch after TTL, releases hits=%d", r)
	}

	// Backoff: a minute after the failure nothing is retried even though GitHub
	// is fine again.
	g.set(func(g *github) { g.releaseStatus = 200 })
	ck.advance(time.Minute)
	st = c.Status(context.Background())
	if !st.Stale || st.Latest != "1.0.1" {
		t.Fatalf("retried inside the backoff window: %+v", st)
	}
	if r, _ := g.hits(); r != 2 {
		t.Fatalf("retried inside the backoff window: releases hits=%d", r)
	}

	// Five minutes after the failure the retry happens and the cache is fresh.
	ck.advance(4*time.Minute + time.Second)
	st = c.Status(context.Background())
	if st.Stale || st.Latest != "9.9.9" || st.Stars != 99 {
		t.Fatalf("retry after backoff: %+v", st)
	}
	if st.CheckedAt != ck.now().UTC().Format(time.RFC3339) {
		t.Fatalf("checked_at = %q, want %q", st.CheckedAt, ck.now().UTC().Format(time.RFC3339))
	}
}

func TestStatusRetriesFirstFetchOnlyAfterBackoff(t *testing.T) {
	g := newGitHub(t)
	g.set(func(g *github) { g.repoStatus = 500 })
	c, ck := newChecker(g, "1.0.0")
	_ = c.Status(context.Background())
	_ = c.Status(context.Background())
	if _, s := g.hits(); s != 1 {
		t.Fatalf("hammered upstream after a failure: repo hits=%d", s)
	}
	ck.advance(5 * time.Minute)
	g.set(func(g *github) { g.repoStatus = 200 })
	st := c.Status(context.Background())
	if st.Stale || st.Stars != 42 {
		t.Fatalf("retry after backoff: %+v", st)
	}
}

func TestStatusNoReleasesYet(t *testing.T) {
	g := newGitHub(t)
	g.set(func(g *github) { g.releaseStatus = 404 })
	c, _ := newChecker(g, "1.0.0")
	st := c.Status(context.Background())
	if st.Stale {
		t.Fatalf("404 on releases/latest is not a failure: %+v", st)
	}
	if st.Latest != "" || st.LatestURL != "" || st.PublishedAt != "" || st.UpdateAvailable {
		t.Fatalf("no releases must leave latest empty: %+v", st)
	}
	if st.Stars != 42 || st.CheckedAt == "" {
		t.Fatalf("stars must still be filled: %+v", st)
	}
}

func TestStatusOversizedBodyIsAFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.1","stargazers_count":1,"pad":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", 2<<20)))
		_, _ = w.Write([]byte(`"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New("acme/panel", "")
	c.BaseURL = srv.URL
	c.Current = "1.0.0"
	st := c.Status(context.Background())
	if !st.Stale || st.Latest != "" {
		t.Fatalf("a body over 1 MiB must not be trusted: %+v", st)
	}
}

func TestStatusSingleFlight(t *testing.T) {
	g := newGitHub(t)
	gate := make(chan struct{})
	g.set(func(g *github) { g.block = gate })
	c, _ := newChecker(g, "1.0.0")

	const n = 25
	results := make([]Status, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.Status(context.Background())
		}()
	}
	// Let the goroutines pile up behind the in-flight fetch, then release it.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	if r, s := g.hits(); r != 1 || s != 1 {
		t.Fatalf("stampede: releases=%d repo=%d, want 1 each", r, s)
	}
	for i, st := range results {
		if st.Stale || st.Latest != "1.0.1" || st.Stars != 42 {
			t.Fatalf("caller %d got %+v", i, st)
		}
	}
}

func TestStatusCallerCancellationDoesNotPoisonWaiters(t *testing.T) {
	g := newGitHub(t)
	c, _ := newChecker(g, "1.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A caller whose request is already gone must not leave the checker with a
	// failure that makes the next caller wait out the backoff window.
	_ = c.Status(ctx)
	st := c.Status(context.Background())
	if st.Stale || st.Latest != "1.0.1" {
		t.Fatalf("cancelled caller poisoned the cache: %+v", st)
	}
}

func TestDisabledStatus(t *testing.T) {
	st := Disabled("acme/panel", "1.0.0")
	if st.Enabled || st.Current != "1.0.0" || st.RepoURL != "https://github.com/acme/panel" {
		t.Fatalf("disabled status: %+v", st)
	}
	if st.Latest != "" || st.Stars != -1 || st.UpdateAvailable || st.CheckedAt != "" || st.Stale {
		t.Fatalf("disabled status carries data: %+v", st)
	}
}
