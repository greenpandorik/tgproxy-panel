// Package updates asks GitHub whether a newer panel release exists.
//
// The only thing that ever leaves the panel is two anonymous (or token-bearing,
// if GITHUB_TOKEN is set) GETs against the public repository metadata: the
// latest release and the star count. Nothing about the installation - version,
// hostname, node count - is sent; the current version is compared locally.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"tgwebproxy/internal/version"
)

const (
	// DefaultTTL is how long a successful answer is served before GitHub is
	// asked again. Releases are rare; an hour keeps a busy panel well inside the
	// 60 req/h anonymous rate limit.
	DefaultTTL = time.Hour
	// retryAfter is the floor between attempts once one has failed, so a
	// rate-limited (403) panel does not keep hammering GitHub on every page load.
	retryAfter = 5 * time.Minute
	// maxBody caps what is read from either response: the two documents are a
	// few KB, and an upstream (or a proxy in the way) must not be able to make
	// the panel buffer arbitrary bytes.
	maxBody = 1 << 20

	defaultBaseURL = "https://api.github.com"
	httpTimeout    = 10 * time.Second
)

// Status is the answer served by GET /api/v1/status/update. Field names are
// the wire contract the SPA is built against.
type Status struct {
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

// Disabled is the response when UPDATE_CHECK=false: the version and the repo
// link are still useful to the UI, everything else is "unknown".
func Disabled(repo, current string) Status {
	return Status{Current: current, Stars: -1, RepoURL: RepoURL(repo)}
}

// RepoURL is the browsable home of an "owner/name" slug.
func RepoURL(repo string) string { return "https://github.com/" + repo }

// fetched is what one successful round trip to GitHub yields.
type fetched struct {
	latest, latestURL, publishedAt string
	stars                          int
	at                             time.Time
}

// Checker caches the GitHub answer and hands it out as a Status.
//
// Exported fields are set before the first Status call and read-only after:
// tests point BaseURL at an httptest server, freeze Now and shrink TTL.
type Checker struct {
	Repo  string
	Token string
	HTTP  *http.Client
	Now   func() time.Time
	TTL   time.Duration
	// BaseURL replaces https://api.github.com; no trailing slash.
	BaseURL string
	// Log receives one warning per failed fetch; nil logs nothing.
	Log *slog.Logger
	// Current is the version compared against the latest tag; empty means
	// version.Version. Tests set it to exercise "dev" and equal-version paths.
	Current string

	mu sync.Mutex
	// good is the last successful fetch, nil until there has been one.
	good *fetched
	// lastAttempt and lastFailed drive the post-failure backoff.
	lastAttempt time.Time
	lastFailed  bool
	// inflight is non-nil while a fetch is running and closed when it ends;
	// callers with nothing cached wait on it instead of starting their own.
	inflight chan struct{}
}

// New returns a Checker with production defaults: a one-hour TTL and a
// 10-second HTTP timeout.
func New(repo, token string) *Checker {
	return &Checker{
		Repo:  repo,
		Token: token,
		HTTP:  &http.Client{Timeout: httpTimeout},
		Now:   time.Now,
		TTL:   DefaultTTL,
	}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return DefaultTTL
}

func (c *Checker) current() string {
	if c.Current != "" {
		return c.Current
	}
	return version.Version
}

// Status returns the cached answer, refreshing it first when it is older than
// TTL. Only one refresh runs at a time: while it is in flight, callers that
// already have an earlier answer get that one back, and callers with nothing
// wait for the refresh. A failed refresh keeps the previous numbers, marks the
// answer stale, and is not retried for retryAfter.
func (c *Checker) Status(ctx context.Context) Status {
	c.mu.Lock()
	now := c.now()
	if c.inflight == nil && c.needsFetch(now) {
		done := make(chan struct{})
		c.inflight = done
		c.mu.Unlock()
		// Detached from the caller: an operator closing the tab mid-fetch must
		// not turn into a failure that every other caller then waits out.
		got, err := c.fetch(context.WithoutCancel(ctx))
		c.mu.Lock()
		defer c.mu.Unlock()
		c.lastAttempt = c.now()
		c.lastFailed = err != nil
		if err == nil {
			c.good = &got
		} else if c.Log != nil {
			// Errors never carry the token (verified by tests), so they are safe to log.
			c.Log.Warn("update check failed", "repo", c.Repo, "err", err)
		}
		c.inflight = nil
		close(done)
		return c.statusLocked()
	}
	if wait := c.inflight; wait != nil && c.good == nil {
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
		}
		c.mu.Lock()
	}
	defer c.mu.Unlock()
	return c.statusLocked()
}

// needsFetch is called with mu held.
func (c *Checker) needsFetch(now time.Time) bool {
	if c.lastFailed && now.Sub(c.lastAttempt) < retryAfter {
		return false
	}
	if c.good == nil {
		return true
	}
	return now.Sub(c.good.at) >= c.ttl()
}

// statusLocked builds the answer from the cached state; mu must be held.
func (c *Checker) statusLocked() Status {
	st := Status{Enabled: true, Current: c.current(), Stars: -1, RepoURL: RepoURL(c.Repo), Stale: c.lastFailed}
	if c.good == nil {
		st.Stale = true
		return st
	}
	st.Latest = c.good.latest
	st.LatestURL = c.good.latestURL
	st.PublishedAt = c.good.publishedAt
	st.Stars = c.good.stars
	st.CheckedAt = c.good.at.UTC().Format(time.RFC3339)
	st.UpdateAvailable = st.Current != "dev" && Newer(st.Latest, st.Current)
	return st
}

type releaseDoc struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

type repoDoc struct {
	Stars int `json:"stargazers_count"`
}

// fetch does the two round trips. Any failure discards both results: a
// half-updated cache (new stars, old release) would be more confusing than a
// stale one.
func (c *Checker) fetch(ctx context.Context) (fetched, error) {
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	var f fetched
	var rel releaseDoc
	status, err := c.get(ctx, base+"/repos/"+c.Repo+"/releases/latest", &rel)
	switch {
	case err != nil:
		return f, err
	case status == http.StatusNotFound:
		// No release published yet: not an error, just nothing to compare with.
	case status != http.StatusOK:
		return f, fmt.Errorf("releases/latest: HTTP %d", status)
	default:
		f.latest = strings.TrimPrefix(rel.TagName, "v")
		f.latestURL = releaseURL(rel.HTMLURL)
		f.publishedAt = rel.PublishedAt
	}
	var repo repoDoc
	status, err = c.get(ctx, base+"/repos/"+c.Repo, &repo)
	switch {
	case err != nil:
		return f, err
	case status != http.StatusOK:
		return f, fmt.Errorf("repo: HTTP %d", status)
	}
	f.stars = repo.Stars
	f.at = c.now()
	return f, nil
}

// get issues one GitHub API request. A non-2xx status is returned, not an
// error, so the caller can treat 404 specially; the body is decoded only on 200.
func (c *Checker) get(ctx context.Context, url string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// A fixed User-Agent: the docs promise that nothing about the installation
	// (the running version included) is sent to GitHub.
	req.Header.Set("User-Agent", "tgproxy-panel")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	// One byte past the cap is read so that an oversized document is detected
	// as such rather than silently truncated into a decode error or, worse,
	// a document that happens to parse.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return 0, err
	}
	if len(body) > maxBody {
		return 0, errors.New("response body over 1 MiB")
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return 0, fmt.Errorf("decode %s: %w", url, err)
	}
	return resp.StatusCode, nil
}

// releaseURL keeps html_url only when it is an https link on github.com. The
// value ends up as an href in the SPA, so a broken or hostile upstream must not
// be able to turn the version chip into a javascript: or off-site link.
func releaseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return ""
	}
	return raw
}

// Newer reports whether latest is a strictly higher release than current.
// Both are major.minor.patch with an optional "v" prefix and an optional
// "-pre" suffix, which sorts below the plain release of the same number.
// Anything that does not parse (including "dev") compares as not newer.
func Newer(latest, current string) bool {
	l, okL := parseSemver(latest)
	c, okC := parseSemver(current)
	if !okL || !okC {
		return false
	}
	for i := range 3 {
		if l.num[i] != c.num[i] {
			return l.num[i] > c.num[i]
		}
	}
	switch {
	case l.pre == "" && c.pre != "":
		return true
	case l.pre != "" && c.pre == "":
		return false
	default:
		return l.pre > c.pre
	}
}

type semver struct {
	num [3]int
	pre string
}

func parseSemver(s string) (semver, bool) {
	var v semver
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = s[i+1:]
		s = s[:i]
		if v.pre == "" {
			return v, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return v, false
		}
		v.num[i] = n
	}
	return v, true
}
