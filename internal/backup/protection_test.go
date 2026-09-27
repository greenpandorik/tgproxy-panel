package backup_test

import (
	"context"
	"io"
	"os"
	"testing"

	"filippo.io/age"
	"tgwebproxy/internal/backup"
)

func TestEncryptedBackupRoundTrip(t *testing.T) {
	identity, e := age.GenerateX25519Identity()
	if e != nil {
		t.Fatal(e)
	}
	r := newRunner(t, &fakeExec{write: "database dump with secrets"})
	r.Protection = &backup.Protection{Recipient: identity.Recipient().String()}
	entry, e := r.Create(context.Background(), backup.KindManual)
	if e != nil || entry.ProtectionError != "" {
		t.Fatalf("%v %s", e, entry.ProtectionError)
	}
	file, e := os.Open(entry.Path + ".age")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = file.Close() }()
	reader, e := age.Decrypt(file, identity)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(reader)
	if e != nil || string(raw) != "database dump with secrets" {
		t.Fatal("round trip failed")
	}
	s := r.ProtectionStatus()
	if !s.Encrypted || s.Uploaded || s.Verified || s.SHA256 == "" {
		t.Fatalf("false success: %+v", s)
	}
}

func TestProtectionFailurePreservesLocalBackup(t *testing.T) {
	r := newRunner(t, &fakeExec{write: "local copy"})
	r.Protection = &backup.Protection{UploadURL: "https://backup.example"}
	entry, e := r.Create(context.Background(), backup.KindScheduled)
	if e != nil || entry.Name == "" || entry.ProtectionError == "" {
		t.Fatalf("local result lost: %+v %v", entry, e)
	}
	if _, e = os.Stat(entry.Path); e != nil {
		t.Fatal(e)
	}
	s := r.ProtectionStatus()
	if s.Error == "" || s.Uploaded {
		t.Fatalf("false upload success: %+v", s)
	}
}
