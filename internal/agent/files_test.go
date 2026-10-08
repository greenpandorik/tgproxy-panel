package agent

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteAtomicKeepOwnerKeepsTheGroup(t *testing.T) {
	other := -1
	groups, _ := os.Getgroups()
	for _, g := range groups {
		if g != os.Getgid() {
			other = g
			break
		}
	}
	if other == -1 {
		t.Skip("needs a supplementary group to chown into")
	}
	path := filepath.Join(t.TempDir(), "telemt.toml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, -1, other); err != nil {
		t.Skipf("chown into group %d: %v", other, err)
	}
	if err := writeAtomicKeepOwner(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gid := int(info.Sys().(*syscall.Stat_t).Gid); gid != other {
		t.Fatalf("gid after the rewrite = %d, want %d", gid, other)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Fatalf("content = %q", b)
	}
}
