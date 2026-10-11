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

func TestHTTPUpstreamHostnameRequiresExplicitStartupResolution(t *testing.T) {
	if _, err := UpstreamBundle("http://origin.internal:8080"); err == nil {
		t.Fatal("implicit DNS accepted")
	}
	bundle, err := UpstreamBundle("http://origin.internal:8080", "startup")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := UpstreamFromBundle(bundle, "startup")
	if err != nil || !ok || got != "http://origin.internal:8080" {
		t.Fatalf("hostname bundle lost: %s %v", got, err)
	}
	if _, _, err := UpstreamFromBundle(bundle); err == nil {
		t.Fatal("bundle bypassed DNS opt-in")
	}
	for _, host := range []string{"http://origin_internal:8080", "http://127.0.0.1.example.com/path", "http://a:0"} {
		if _, err := UpstreamBundle(host, "startup"); err == nil {
			t.Fatalf("invalid origin accepted: %s", host)
		}
	}
}
