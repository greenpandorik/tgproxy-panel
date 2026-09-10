package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// verifyDecoy checks both Telemt's private listener and the public TLS front, without
// credentials. A successful control API check alone does not prove the website is served.
func (h *Handler) verifyDecoy(ctx context.Context, index []byte) error {
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return err
	}
	hosts, err := telemtVhosts(cfg)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return fmt.Errorf("decoy has no configured virtual host")
	}
	vhost, _ := hosts[0].(map[string]any)
	host, _ := vhost["host"].(string)
	if host == "" {
		return fmt.Errorf("decoy virtual host is missing")
	}
	client := h.decoyHTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	for _, endpoint := range []string{"http://127.0.0.1:18080/", "https://" + host + "/"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Host = host
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("decoy HTTP/TLS check %s: %w", endpoint, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(len(index))+1))
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode != http.StatusOK || !bytes.Equal(raw, index) {
			return fmt.Errorf("decoy check %s: status %d or served index differs from deployed website", endpoint, resp.StatusCode)
		}
	}
	return nil
}

func (h *Handler) decoyClient() *http.Client {
	if h.decoyHTTP == nil {
		return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	client := *h.decoyHTTP
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 10 * time.Second
	}
	return &client
}

func (h *Handler) probeHTTPWebsite(ctx context.Context, endpoint, host string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if host != "" {
		req.Host = host
	}
	resp, err := h.decoyClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return fmt.Errorf("empty response")
	}
	return nil
}

func (h *Handler) verifyUpstreamDecoy(ctx context.Context, origin string) error {
	if err := h.probeHTTPWebsite(ctx, origin, ""); err != nil {
		return fmt.Errorf("origin check %s: %w", origin, err)
	}
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return err
	}
	hosts, err := telemtVhosts(cfg)
	if err != nil || len(hosts) == 0 {
		return fmt.Errorf("decoy has no configured virtual host")
	}
	vhost, _ := hosts[0].(map[string]any)
	host, _ := vhost["host"].(string)
	if host == "" {
		return fmt.Errorf("decoy virtual host is missing")
	}
	if err := h.probeHTTPWebsite(ctx, "https://"+host+"/", host); err != nil {
		return fmt.Errorf("public upstream decoy check: %w", err)
	}
	return nil
}

func (h *Handler) verifyConfiguredDecoy(ctx context.Context) error {
	cfg, _, err := h.tm.GetConfig(ctx)
	if err != nil {
		return err
	}
	hosts, err := telemtVhosts(cfg)
	if err != nil || len(hosts) == 0 {
		return fmt.Errorf("decoy has no configured virtual host")
	}
	vhost, _ := hosts[0].(map[string]any)
	decoy, _ := vhost["decoy"].(map[string]any)
	mode, _ := decoy["mode"].(string)
	switch mode {
	case "http_upstream":
		origin, _ := decoy["upstream"].(string)
		if origin == "" {
			return fmt.Errorf("HTTP upstream decoy has no origin")
		}
		return h.verifyUpstreamDecoy(ctx, origin)
	case "static_directory", "":
		index, err := os.ReadFile(filepath.Join(h.siteDir(), "index.html"))
		if err != nil {
			return err
		}
		return h.verifyDecoy(ctx, index)
	default:
		return fmt.Errorf("unsupported decoy mode %q", mode)
	}
}
