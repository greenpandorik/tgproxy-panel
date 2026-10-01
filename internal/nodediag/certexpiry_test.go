package nodediag

import (
	"testing"
	"time"
)

func TestCertNotAfterReadsBackEveryExpiryCheck(t *testing.T) {
	for _, days := range []int{-3, 5, 61} {
		want := time.Now().Add(time.Duration(days)*24*time.Hour + time.Hour).UTC().Truncate(time.Second)
		got, found := CertNotAfter(certExpiry(want))
		if !found || !got.Equal(want) {
			t.Fatalf("%d days: got %v (%v), want %v", days, got, found, want)
		}
	}
}

func TestCertNotAfterIgnoresOtherChecks(t *testing.T) {
	if _, found := CertNotAfter(na("certificate_expiry", "port 443 could not be reached")); found {
		t.Fatal("a check that did not run has no date")
	}
	if _, found := CertNotAfter(ok("certificate", "x", "the certificate covers 2026-01-01T00:00:00Z")); found {
		t.Fatal("only certificate_expiry carries the date")
	}
}
