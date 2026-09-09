package telemt

import "testing"

func TestBuildIDPrefersTheCommit(t *testing.T) {
	if got := (SystemInfo{GitCommit: "abc123", TargetOS: "linux", TargetArch: "x86_64"}).BuildID(); got != "abc123" {
		t.Fatalf("BuildID = %q, want the commit when telemt reports one", got)
	}
	if got := (SystemInfo{TargetOS: "linux", TargetArch: "x86_64"}).BuildID(); got != "linux/x86_64" {
		t.Fatalf("BuildID = %q, want the platform when there is no commit", got)
	}
	if got := (SystemInfo{}).BuildID(); got != "" {
		t.Fatalf("BuildID = %q, want empty when telemt said nothing", got)
	}
}
