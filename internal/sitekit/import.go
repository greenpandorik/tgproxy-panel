package sitekit

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
)

// ImportZIP reads a static website without ever extracting files onto disk.
// Both compressed and expanded sizes are bounded; duplicate and ambiguous paths fail closed.
func ImportZIP(raw []byte, limit int64) (string, map[string][]byte, error) {
	if int64(len(raw)) > limit {
		return "", nil, fmt.Errorf("ZIP exceeds %d bytes", limit)
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", nil, err
	}
	if len(z.File) > 512 {
		return "", nil, fmt.Errorf("ZIP contains more than 512 entries")
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range z.File {
		name := f.Name
		if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") || strings.ContainsRune(name, 0) {
			return "", nil, fmt.Errorf("unsafe ZIP path %q", name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return "", nil, fmt.Errorf("unsafe ZIP path %q", name)
			}
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return "", nil, fmt.Errorf("non-regular ZIP entry %q", name)
		}
		name = path.Clean(name)
		if _, exists := files[name]; exists {
			return "", nil, fmt.Errorf("duplicate ZIP entry %q", name)
		}
		if !StaticAssetPath(name) {
			return "", nil, fmt.Errorf("unsupported static file %q", name)
		}
		r, err := f.Open()
		if err != nil {
			return "", nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, limit-total+1))
		_ = r.Close()
		if err != nil {
			return "", nil, err
		}
		total += int64(len(b))
		if total > limit {
			return "", nil, fmt.Errorf("expanded ZIP exceeds %d bytes", limit)
		}
		files[name] = b
	}
	// Accept a single enclosing directory, as produced by desktop ZIP tools.
	if _, ok := files["index.html"]; !ok {
		prefix := ""
		for name := range files {
			first, _, found := strings.Cut(name, "/")
			if !found {
				return "", nil, fmt.Errorf("index.html is required")
			}
			if prefix == "" {
				prefix = first + "/"
			}
			if !strings.HasPrefix(name, prefix) {
				return "", nil, fmt.Errorf("index.html is required at the website root")
			}
		}
		unwrapped := map[string][]byte{}
		for name, b := range files {
			unwrapped[strings.TrimPrefix(name, prefix)] = b
		}
		files = unwrapped
	}
	index, ok := files["index.html"]
	if !ok || len(bytes.TrimSpace(index)) == 0 {
		return "", nil, fmt.Errorf("non-empty index.html is required")
	}
	delete(files, "index.html")
	return string(index), files, nil
}

func StaticAssetPath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".txt", ".xml", ".svg", ".png", ".jpg", ".jpeg", ".webp", ".avif", ".gif", ".ico", ".woff", ".woff2", ".ttf", ".otf", ".mp4", ".webm", ".mp3", ".ogg", ".pdf":
		return true
	}
	return false
}
