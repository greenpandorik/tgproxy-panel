package subscription_test

import (
	"strings"
	"testing"

	"tgwebproxy/internal/subscription"
)

func testPage() subscription.Page {
	return subscription.Page{
		PanelName:    "Acme Panel",
		PrimaryColor: "#3b82f6",
		AccentColor:  "#22d3ee",
		Theme:        "dark",
		SupportLink:  "https://support.example.com",
		FooterText:   "Acme Ops",
		Locations: []subscription.Location{
			{
				Name:     "Frankfurt",
				Hostname: "fra1.example.com",
				Links: []subscription.LocationLink{
					{
						Kind:      "web",
						Label:     "WEB",
						TMe:       "https://t.me/webproxy?server=fra1.example.com&secret=abc",
						Tg:        "tg://webproxy?server=fra1.example.com&secret=abc",
						QRDataURI: "data:image/png;base64,ZmFrZQ==",
					},
					{
						Kind:      "tls",
						Label:     "Fake-TLS",
						TMe:       "https://t.me/proxy?server=fra1.example.com&port=8443&secret=eeabc666",
						Tg:        "tg://proxy?server=fra1.example.com&port=8443&secret=eeabc666",
						QRDataURI: "data:image/png;base64,ZmFrZTM=",
					},
				},
			},
			{
				Name:     "Amsterdam",
				Hostname: "ams1.example.com",
				Links: []subscription.LocationLink{
					{
						Kind:      "web",
						Label:     "WEB",
						TMe:       "https://t.me/webproxy?server=ams1.example.com&secret=def",
						Tg:        "tg://webproxy?server=ams1.example.com&secret=def",
						QRDataURI: "data:image/png;base64,ZmFrZTI=",
					},
				},
			},
		},
		ClientSupport: map[string]string{"desktop": "stable", "android": "experimental", "ios": "planned"},
	}
}

func TestRenderIncludesBothLocations(t *testing.T) {
	var buf strings.Builder
	if err := subscription.Render(&buf, testPage()); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"fra1.example.com",
		"ams1.example.com",
		"https://t.me/webproxy?server=fra1.example.com&amp;secret=abc",
		"tg://webproxy?server=fra1.example.com&amp;secret=abc",
		"https://t.me/webproxy?server=ams1.example.com&amp;secret=def",
		"tg://webproxy?server=ams1.example.com&amp;secret=def",
		"data:image/png;base64,ZmFrZQ==",
		"data:image/png;base64,ZmFrZTI=",
		"data:image/png;base64,ZmFrZTM=",
		"https://t.me/proxy?server=fra1.example.com&amp;port=8443&amp;secret=eeabc666",
		"Fake-TLS",
		"Acme Panel",
		"Frankfurt",
		"Amsterdam",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderHasNoExternalScripts(t *testing.T) {
	var buf strings.Builder
	if err := subscription.Render(&buf, testPage()); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script src") {
		t.Error("output must not load external scripts")
	}
	if strings.Contains(out, "http://") && !strings.Contains(strings.ToLower(out), "https://t.me") {
		t.Error("output should not reference insecure external resources")
	}
}

func TestRenderNeverShowsLabelFields(t *testing.T) {
	// Page has no field to carry a key label/owner label/note at all: the type
	// system itself enforces that the public page cannot leak them.
	p := testPage()
	var buf strings.Builder
	if err := subscription.Render(&buf, p); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, forbidden := range []string{"owner_label", "Owner", "label=\""} {
		if strings.Contains(out, forbidden) {
			t.Errorf("output leaks %q", forbidden)
		}
	}
}

func TestRenderEmptyLocationsStillRenders(t *testing.T) {
	p := testPage()
	p.Locations = nil
	var buf strings.Builder
	if err := subscription.Render(&buf, p); err != nil {
		t.Fatalf("render with no locations: %v", err)
	}
}

// TestRenderErrorHasNoLocationOrSecretData covers Task 33 item 3(d): the
// branded 404/410 page for /s/{token} carries only PanelName/Theme/Message -
// there is no field to leak a key label, hostname or link even by accident.
func TestRenderErrorHasNoLocationOrSecretData(t *testing.T) {
	var buf strings.Builder
	p := subscription.ErrorPage{PanelName: "Acme Panel", Theme: "dark", Message: "Ссылка не найдена."}
	if err := subscription.RenderError(&buf, p); err != nil {
		t.Fatalf("render error page: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<html", "Acme Panel", "Ссылка не найдена."} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(out, "<script") {
		t.Error("error page must not load or run any script")
	}
}
