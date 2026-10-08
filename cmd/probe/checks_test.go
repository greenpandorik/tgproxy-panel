package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthenticatedChecksUnconfigured(t *testing.T) {
	r, e := authenticatedChecks(t.Context(), "127.0.0.1", "", "")
	if e != nil || r.FakeTLS.Status != "not_run" || r.WEB.Status != "not_run" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestCustomCheckerCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "check.sh")
	if e := os.WriteFile(path, []byte("#!/bin/sh\nread host\n[ \"$host\" = \"proxy.example.com\" ] || exit 1\nprintf '%s' '{\"faketls\":{\"status\":\"ok\",\"latency_ms\":123},\"web\":{\"status\":\"not_run\",\"latency_ms\":0}}'\n"), 0o700); e != nil {
		t.Fatal(e)
	}
	r, e := authenticatedChecks(context.Background(), "proxy.example.com", "", path)
	if e != nil || r.FakeTLS.Status != "ok" || r.FakeTLS.LatencyMS != 123 || r.WEB.Status != "not_run" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestCheckerConfigAndCustomRunnerConflict(t *testing.T) {
	if _, e := authenticatedChecks(t.Context(), "127.0.0.1", "config.json", "/bin/true"); e == nil {
		t.Fatal("ambiguous checker accepted")
	}
}
