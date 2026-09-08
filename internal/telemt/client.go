// Package telemt is a client for the telemt Control API (docs: Architecture/API/API.md).
// Every response uses the {ok, data, revision} envelope; failures are surfaced as *APIError.
// The client never logs and never echoes the Authorization header or a user secret.
package telemt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultMetricsURL is telemt's Prometheus listener from the node template ([server]
// metrics_listen). It is a separate port from the control API and is guarded by its own
// whitelist, so the API token is never sent there.
const DefaultMetricsURL = "http://127.0.0.1:9090/metrics"

// maxResponseBytes caps a single API response. /metrics is the largest realistic body.
const maxResponseBytes = 8 << 20

// Client talks to one node's telemt control API.
type Client struct {
	BaseURL    string
	AuthHeader string
	// MetricsURL is the Prometheus endpoint used by Metrics; it is not part of the /v1 API.
	MetricsURL string
	HTTP       *http.Client
}

// New returns a client for baseURL (e.g. http://127.0.0.1:9091) authenticating with the exact
// value telemt expects in the Authorization header.
func New(baseURL, authHeader string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		AuthHeader: authHeader,
		MetricsURL: DefaultMetricsURL,
		HTTP:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) httpc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// do performs one API call and unwraps the envelope into out, returning the config revision.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (string, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return "", err
	}
	if c.AuthHeader != "" {
		req.Header.Set("Authorization", c.AuthHeader)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpc().Do(req)
	if err != nil {
		return "", fmt.Errorf("telemt %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("telemt %s %s: %w", method, path, err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// A non-envelope body (proxy error page, plain text) is only meaningful as a status.
		if resp.StatusCode >= 400 {
			return "", &APIError{Status: resp.StatusCode, Code: "unexpected_response", Message: http.StatusText(resp.StatusCode)}
		}
		return "", fmt.Errorf("telemt %s %s: invalid response body", method, path)
	}
	if !env.OK {
		e := &APIError{Status: resp.StatusCode, Code: "unexpected_response", Message: http.StatusText(resp.StatusCode)}
		if env.Error != nil {
			e.Code, e.Message = env.Error.Code, env.Error.Message
		}
		return "", e
	}
	// A 503 with ok:true is a legitimate answer (health/ready when not ready yet), so status is
	// not consulted beyond the envelope.
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return env.Revision, fmt.Errorf("telemt %s %s: decode data: %w", method, path, err)
		}
	}
	return env.Revision, nil
}

func userPath(username string, suffix ...string) string {
	p := "/v1/users/" + url.PathEscape(username)
	if len(suffix) > 0 {
		p += "/" + suffix[0]
	}
	return p
}

func (c *Client) Health(ctx context.Context) (HealthData, error) {
	var out HealthData
	_, err := c.do(ctx, http.MethodGet, "/v1/health", nil, &out)
	return out, err
}

// Ready reports readiness. telemt answers 503 with a success envelope when it is not ready, so
// a false Ready with a nil error is the normal "not ready yet" answer.
func (c *Client) Ready(ctx context.Context) (ReadyData, error) {
	var out ReadyData
	_, err := c.do(ctx, http.MethodGet, "/v1/health/ready", nil, &out)
	return out, err
}

func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	var out SystemInfo
	_, err := c.do(ctx, http.MethodGet, "/v1/system/info", nil, &out)
	return out, err
}

// ListUsers returns the configured users sorted by username. The returned secrets are empty:
// the API never exposes a secret in a list view.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	var out []User
	_, err := c.do(ctx, http.MethodGet, "/v1/users", nil, &out)
	return out, err
}

// CreateUser creates a user; the returned User carries the effective secret.
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	var out createUserResponse
	if _, err := c.do(ctx, http.MethodPost, "/v1/users", req, &out); err != nil {
		return User{}, err
	}
	u := out.User
	u.Secret = out.Secret
	return u, nil
}

func (c *Client) PatchUser(ctx context.Context, username string, req PatchUserRequest) (User, error) {
	var out User
	_, err := c.do(ctx, http.MethodPatch, userPath(username), req, &out)
	return out, err
}

func (c *Client) DeleteUser(ctx context.Context, username string) error {
	_, err := c.do(ctx, http.MethodDelete, userPath(username), nil, nil)
	return err
}

// RotateSecret sets a new secret (or, with an empty secret, lets telemt generate one) and
// returns the user with the effective secret.
func (c *Client) RotateSecret(ctx context.Context, username, secret string) (User, error) {
	var body any
	if secret != "" {
		body = map[string]string{"secret": secret}
	}
	var out createUserResponse
	if _, err := c.do(ctx, http.MethodPost, userPath(username, "rotate-secret"), body, &out); err != nil {
		return User{}, err
	}
	u := out.User
	u.Secret = out.Secret
	return u, nil
}

func (c *Client) Enable(ctx context.Context, username string) (User, error) {
	var out User
	_, err := c.do(ctx, http.MethodPost, userPath(username, "enable"), nil, &out)
	return out, err
}

func (c *Client) Disable(ctx context.Context, username string) (User, error) {
	var out User
	_, err := c.do(ctx, http.MethodPost, userPath(username, "disable"), nil, &out)
	return out, err
}

// GetConfig returns the editable config sections (never `access`) and the current revision.
func (c *Client) GetConfig(ctx context.Context) (map[string]any, string, error) {
	out := map[string]any{}
	rev, err := c.do(ctx, http.MethodGet, "/v1/config", nil, &out)
	if err != nil {
		return nil, "", err
	}
	return out, rev, nil
}

// PatchConfig applies a sparse patch. Tables deep-merge; arrays and scalars replace wholesale,
// so an array (e.g. web.vhosts) must be sent complete. With reload the patch also asks for a
// draining runtime reload, which activates a new generation without cutting live sessions.
func (c *Client) PatchConfig(ctx context.Context, patch map[string]any, reload bool) (PatchConfigResult, error) {
	path := "/v1/config"
	if reload {
		path += "?reload=drain&timeout_secs=" + strconv.Itoa(ReloadDrainSecs)
	}
	var out PatchConfigResult
	_, err := c.do(ctx, http.MethodPatch, path, patch, &out)
	return out, err
}

// ReloadDrainSecs is how long telemt lets the previous generation's sessions finish. It is
// exported because it is also the floor for the caller's own wait budget: a reload sits in the
// non-terminal `draining` state for up to this long, so a poller that gives up sooner would
// call a perfectly good reload a failure.
const ReloadDrainSecs = 30

// Reload asks telemt to activate a new runtime generation from the on-disk config. An empty
// request defaults to a draining reload so live sessions are not cut.
func (c *Client) Reload(ctx context.Context, req ReloadRequest) (ReloadAccepted, error) {
	if req.Mode == "" {
		req.Mode, req.TimeoutSecs = "drain", ReloadDrainSecs
	}
	var out ReloadAccepted
	_, err := c.do(ctx, http.MethodPost, "/v1/system/reload", req, &out)
	return out, err
}

func (c *Client) ReloadStatus(ctx context.Context, id int64) (ReloadStatusData, error) {
	var out ReloadStatusData
	_, err := c.do(ctx, http.MethodGet, "/v1/system/reload/"+strconv.FormatInt(id, 10), nil, &out)
	return out, err
}

func (c *Client) ConnectionsSummary(ctx context.Context) (ConnectionsSummary, error) {
	var out ConnectionsSummary
	_, err := c.do(ctx, http.MethodGet, "/v1/runtime/connections/summary", nil, &out)
	return out, err
}

// UpstreamsStats reports the node's connectivity to Telegram's datacenters: per-DC latency EMAs
// from telemt's own health checks plus the connect counters.
func (c *Client) UpstreamsStats(ctx context.Context) (UpstreamsStats, error) {
	var out UpstreamsStats
	_, err := c.do(ctx, http.MethodGet, "/v1/stats/upstreams", nil, &out)
	return out, err
}

// Metrics fetches the Prometheus exposition text from the metrics listener. It is a plain text
// endpoint on its own port, so no envelope and no Authorization header are involved.
func (c *Client) Metrics(ctx context.Context) (string, error) {
	target := c.MetricsURL
	if target == "" {
		target = DefaultMetricsURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpc().Do(req)
	if err != nil {
		return "", fmt.Errorf("telemt metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("telemt metrics: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Code: "metrics_unavailable", Message: http.StatusText(resp.StatusCode)}
	}
	return string(raw), nil
}
