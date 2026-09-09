package telemt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Limits GET /v1/runtime/web/sessions and the drain timeout accept.
const (
	MaxWebSessionsLimit = 200
	MinDrainTimeoutSecs = 1
	MaxDrainTimeoutSecs = 3600
)

const webBasePath = "/v1/runtime/web"

// WebStatus returns the WEB runtime status and caches the runtime instance the write calls need.
func (c *Client) WebStatus(ctx context.Context) (WebStatus, error) {
	var out WebStatus
	if _, err := c.do(ctx, http.MethodGet, webBasePath+"/status", nil, &out); err != nil {
		return WebStatus{}, err
	}
	if out.RuntimeInstance != "" {
		c.mu.Lock()
		c.runtimeInstance = out.RuntimeInstance
		c.mu.Unlock()
	}
	return out, nil
}

func (q WebSessionsQuery) values() (url.Values, error) {
	v := url.Values{}
	if q.Carrier != "" {
		if !ValidCarrier(q.Carrier) {
			return nil, fmt.Errorf("telemt: unknown carrier %q", q.Carrier)
		}
		v.Set("carrier", q.Carrier)
	}
	if q.State != "" {
		v.Set("state", q.State)
	}
	if q.Limit != 0 {
		if q.Limit < 1 || q.Limit > MaxWebSessionsLimit {
			return nil, fmt.Errorf("telemt: sessions limit %d out of range 1..%d", q.Limit, MaxWebSessionsLimit)
		}
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	return v, nil
}

// WebSessions returns one page of WEB sessions; an empty NextCursor means the last page.
func (c *Client) WebSessions(ctx context.Context, q WebSessionsQuery) (WebSessionsPage, error) {
	v, err := q.values()
	if err != nil {
		return WebSessionsPage{}, err
	}
	path := webBasePath + "/sessions"
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	var out WebSessionsPage
	_, err = c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// WebPause stops admitting new WEB work while keeping live sessions.
func (c *Client) WebPause(ctx context.Context) error {
	_, err := withRuntimeInstance(ctx, c, func(instance string) (struct{}, error) {
		_, err := c.do(ctx, http.MethodPost, webBasePath+"/lifecycle/pause", webInstanceRequest{RuntimeInstance: instance}, nil)
		return struct{}{}, err
	})
	return err
}

// WebResume reopens admission after a pause or a drain.
func (c *Client) WebResume(ctx context.Context) error {
	_, err := withRuntimeInstance(ctx, c, func(instance string) (struct{}, error) {
		_, err := c.do(ctx, http.MethodPost, webBasePath+"/lifecycle/resume", webInstanceRequest{RuntimeInstance: instance}, nil)
		return struct{}{}, err
	})
	return err
}

// WebDrain starts a drain and returns as soon as telemt accepts it; progress is read from WebStatus.
func (c *Client) WebDrain(ctx context.Context, timeoutSecs int) (WebDrainAccepted, error) {
	if timeoutSecs < MinDrainTimeoutSecs || timeoutSecs > MaxDrainTimeoutSecs {
		return WebDrainAccepted{}, fmt.Errorf("telemt: drain timeout %d out of range %d..%d", timeoutSecs, MinDrainTimeoutSecs, MaxDrainTimeoutSecs)
	}
	return withRuntimeInstance(ctx, c, func(instance string) (WebDrainAccepted, error) {
		var out WebDrainAccepted
		body := webDrainRequest{RuntimeInstance: instance, TimeoutSecs: timeoutSecs}
		_, err := c.do(ctx, http.MethodPost, webBasePath+"/lifecycle/drain", body, &out)
		return out, err
	})
}

// ResetCarrierLearning clears the learned carrier table.
func (c *Client) ResetCarrierLearning(ctx context.Context) (CarrierLearningReset, error) {
	return withRuntimeInstance(ctx, c, func(instance string) (CarrierLearningReset, error) {
		var out CarrierLearningReset
		body := webInstanceRequest{RuntimeInstance: instance}
		_, err := c.do(ctx, http.MethodPost, webBasePath+"/carrier-learning/reset", body, &out)
		return out, err
	})
}

func (c *Client) webRuntimeInstance(ctx context.Context, refresh bool) (string, error) {
	c.mu.Lock()
	cached := c.runtimeInstance
	if refresh {
		c.runtimeInstance = ""
	}
	c.mu.Unlock()
	if !refresh && cached != "" {
		return cached, nil
	}
	st, err := c.WebStatus(ctx)
	if err != nil {
		return "", err
	}
	if st.RuntimeInstance == "" {
		return "", &APIError{Status: http.StatusConflict, Code: "web_runtime_unavailable", Message: "web status carries no runtime_instance"}
	}
	return st.RuntimeInstance, nil
}

// withRuntimeInstance runs call with the current runtime instance and retries once, with a
// re-read instance, when telemt reports that it restarted underneath us.
func withRuntimeInstance[T any](ctx context.Context, c *Client, call func(instance string) (T, error)) (T, error) {
	var zero T
	instance, err := c.webRuntimeInstance(ctx, false)
	if err != nil {
		return zero, err
	}
	out, err := call(instance)
	if !IsCode(err, ErrCodeRuntimeMismatch) {
		return out, err
	}
	instance, err = c.webRuntimeInstance(ctx, true)
	if err != nil {
		return zero, err
	}
	return call(instance)
}

// WebMetrics fetches the Prometheus text and returns the parsed telemt_web_* families.
func (c *Client) WebMetrics(ctx context.Context) (WebMetrics, error) {
	text, err := c.Metrics(ctx)
	if err != nil {
		return WebMetrics{}, err
	}
	return ParseWebMetrics(text), nil
}
