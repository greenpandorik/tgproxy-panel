package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresClientSuppliedForwardedEntries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{"no header falls back to the peer", "10.0.0.9:5555", nil, "10.0.0.9"},
		{"forged head, our proxy's entry last", "10.0.0.9:5555", []string{"1.2.3.4, 10.0.0.9"}, "10.0.0.9"},
		{"single entry written by the proxy", "10.0.0.9:5555", []string{"203.0.113.7"}, "203.0.113.7"},
		{"spaces and a long chain", "10.0.0.9:5555", []string{"1.2.3.4 , 5.6.7.8 ,  10.0.0.9 "}, "10.0.0.9"},
		// Repeated header lines are one list too; Header.Get would only see the first.
		{"split across header lines", "10.0.0.9:5555", []string{"1.2.3.4", "10.0.0.9"}, "10.0.0.9"},
		{"junk last entry falls back to the peer", "10.0.0.9:5555", []string{"1.2.3.4, not-an-ip"}, "10.0.0.9"},
		{"empty header falls back to the peer", "10.0.0.9:5555", []string{""}, "10.0.0.9"},
		{"ipv6 with a port", "10.0.0.9:5555", []string{"[2001:db8::1]:443"}, "2001:db8::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remoteAddr
			for _, v := range tc.forwarded {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
