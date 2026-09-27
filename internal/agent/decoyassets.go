package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// localAssets reads browser resource references, never arbitrary links or remote origins.
func localAssets(index []byte) ([]string, error) {
	z := html.NewTokenizer(bytes.NewReader(index))
	seen := map[string]bool{}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, z.Err()
			}
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		token := z.Token()
		if token.Data != "script" && token.Data != "link" && token.Data != "img" && token.Data != "source" {
			continue
		}
		if token.Data == "link" {
			resource := false
			for _, a := range token.Attr {
				if a.Key == "rel" {
					for _, rel := range strings.Fields(strings.ToLower(a.Val)) {
						if rel == "stylesheet" || rel == "icon" || rel == "preload" || rel == "modulepreload" {
							resource = true
						}
					}
				}
			}
			if !resource {
				continue
			}
		}
		for _, a := range token.Attr {
			if a.Key != "src" && (a.Key != "href" || token.Data != "link") {
				continue
			}
			u, e := url.Parse(a.Val)
			if e != nil || u.IsAbs() || u.Host != "" || u.Path == "" {
				continue
			}
			p, e := safeSitePath(strings.TrimPrefix(u.Path, "/"))
			if e != nil {
				return nil, e
			}
			seen[p] = true
		}
	}
	if len(seen) > 128 {
		return nil, errors.New("website references more than 128 local assets; asset verification budget exceeded")
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func (h *Handler) verifyWebsiteAssets(ctx context.Context, client *http.Client, endpoint, host string, index []byte) error {
	paths, e := localAssets(index)
	if e != nil {
		return e
	}
	base, e := url.Parse(endpoint)
	if e != nil {
		return e
	}
	for _, p := range paths {
		local := filepath.Join(h.siteDir(), p)
		info, e := os.Lstat(local)
		if e != nil {
			return fmt.Errorf("website asset %s is absent: %w", p, e)
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return fmt.Errorf("website asset %s is not a regular file within the verification limit", p)
		}
		expected, e := os.ReadFile(local)
		if e != nil {
			return e
		}
		target := *base
		target.Path = "/" + p
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if e != nil {
			return e
		}
		req.Host = host
		resp, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("website asset %s did not answer: %w", p, e)
		}
		actual, re := io.ReadAll(io.LimitReader(resp.Body, int64(len(expected))+1))
		_ = resp.Body.Close()
		if re != nil {
			return re
		}
		if resp.StatusCode != 200 || sha256.Sum256(actual) != sha256.Sum256(expected) {
			return fmt.Errorf("website asset %s: unexpected status or content", p)
		}
		contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".css" && contentType != "text/css" {
			return fmt.Errorf("website stylesheet %s has invalid MIME type", p)
		}
		if (ext == ".js" || ext == ".mjs") && contentType != "text/javascript" && contentType != "application/javascript" {
			return fmt.Errorf("website script %s has invalid MIME type", p)
		}
	}
	return nil
}
