package sitekit

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func zipFixture(t *testing.T, name string, mode os.FileMode, body string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetMode(mode)
	f, err := w.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte(body))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestImportZIPRejectsUnsafeArchives(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode os.FileMode
		body string
	}{
		{"../index.html", 0o644, "hello"}, {"/index.html", 0o644, "hello"}, {"a\\index.html", 0o644, "hello"}, {"index.html", os.ModeSymlink | 0o777, "/etc/passwd"}, {"server.php", 0o644, "<?php"}, {"index.html", 0o644, strings.Repeat("x", 4096)},
	} {
		t.Run(tc.name+tc.mode.String(), func(t *testing.T) {
			if _, _, err := ImportZIP(zipFixture(t, tc.name, tc.mode, tc.body), 1024); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestImportZIPUnwrapsOneDirectory(t *testing.T) {
	index, assets, err := ImportZIP(zipFixture(t, "website/index.html", 0o644, "<h1>Hello</h1>"), 4096)
	if err != nil || index != "<h1>Hello</h1>" || len(assets) != 0 {
		t.Fatalf("%q %v %v", index, assets, err)
	}
}

func TestCustomizeEscapesTextAndDisablesVariants(t *testing.T) {
	source := `<html><head><title>Acme</title></head><body><h1 data-variants="old|other">old</h1><p>old description</p><footer><p>copyright</p></footer></body></html>`
	out, err := Customize(source, map[string]string{"headline": "old", "footer_text": "copyright"}, map[string]string{"headline": "<script>alert(1)</script>", "footer_text": "My footer"})
	if err != nil || strings.Contains(out, "<script>") || strings.Contains(out, "data-variants") || strings.Count(out, "My footer") != 1 {
		t.Fatalf("unsafe/incorrect customization: %s %v", out, err)
	}
}
