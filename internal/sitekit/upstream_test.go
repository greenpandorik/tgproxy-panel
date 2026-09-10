package sitekit

import "testing"

func TestValidateHTTPUpstream(t *testing.T) {
	for _, valid := range []string{"http://127.0.0.1:3000", "http://10.0.0.4", "http://[::1]:8080"} {
		if _, err := ValidateHTTPUpstream(valid); err != nil {
			t.Errorf("valid origin %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"https://127.0.0.1:3000", "http://example.com:3000", "http://8.8.8.8",
		"http://127.0.0.1:3000/private", "http://user:pass@127.0.0.1:3000",
	} {
		if _, err := ValidateHTTPUpstream(invalid); err == nil {
			t.Errorf("unsafe origin %q accepted", invalid)
		}
	}
}
