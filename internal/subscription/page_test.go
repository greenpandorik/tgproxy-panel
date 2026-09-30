package subscription_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tgwebproxy/internal/alerttext"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/subscription"
)

var (
	ams = uuid.MustParse("a4b9ecb3-1f8a-4bc4-b82f-76244089c0ae")
	hel = uuid.MustParse("96753fb2-ca95-4445-a8c0-ab880fc93ce3")
)

const secret = "0123456789abcdef0123456789abcdef"

func locations() []keys.NodeLinks {
	return []keys.NodeLinks{
		keys.LinksFor(keys.LinkTarget{
			NodeID: ams, NodeName: "Amsterdam", Hostname: "ams1.example.net", Engine: db.NodeEngineTelemt,
			TLSDomain: "ams1.example.net", TLSDomains: []string{"backup.example.org"}, ClassicPort: 8443,
		}, secret),
		keys.LinksFor(keys.LinkTarget{NodeID: hel, NodeName: "Helsinki", Hostname: "hel1.example.net", Engine: db.NodeEngineTproxy}, secret),
	}
}

func build(t *testing.T, s subscription.Settings, l alerttext.Lang, p subscription.Platform, expires *time.Time) (subscription.Page, string) {
	t.Helper()
	page, err := subscription.Build(subscription.Input{
		Settings: s, Lang: l, Platform: p, Locations: locations(), ExpiresAt: expires, QRSize: 128,
		Branding: subscription.Branding{PanelName: "Demo", PrimaryColor: "#3fc0d6", AccentColor: "#20c997", PrimaryInk: "#0b0e11", Theme: "dark"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := subscription.Render(&buf, page); err != nil {
		t.Fatal(err)
	}
	return page, buf.String()
}

func platform(page subscription.Page, id subscription.Platform) subscription.PlatformView {
	for _, p := range page.Platforms {
		if p.ID == id {
			return p
		}
	}
	return subscription.PlatformView{}
}

func TestEachDeviceGetsTheLinkItCanOpen(t *testing.T) {
	page, _ := build(t, subscription.DefaultSettings(), alerttext.RU, subscription.IOS, nil)

	ios := platform(page, subscription.IOS)
	if len(ios.Actions) != 1 || ios.Actions[0].Name != "Amsterdam" || ios.Actions[0].Primary.Kind != keys.LinkTLS || ios.Actions[0].Alt != nil {
		t.Fatalf("iPhone should get only the Fake-TLS server: %+v", ios.Actions)
	}
	android := platform(page, subscription.Android)
	if len(android.Actions) != 2 || android.Actions[0].Primary.Kind != keys.LinkTLS || android.Actions[0].Alt.Kind != keys.LinkWeb {
		t.Fatalf("Android: Fake-TLS first with WEB as the fallback: %+v", android.Actions)
	}
	if android.Actions[1].Primary.Kind != keys.LinkWeb || android.Actions[1].Alt != nil {
		t.Fatalf("a tproxy server offers WEB only: %+v", android.Actions[1])
	}
	desktop := platform(page, subscription.Desktop)
	if desktop.Actions[0].Primary.Kind != keys.LinkWeb || desktop.Actions[0].Alt.Kind != keys.LinkTLS {
		t.Fatalf("desktop: WEB first with Fake-TLS as the fallback: %+v", desktop.Actions[0])
	}
	if !ios.Selected || android.Selected {
		t.Fatal("the detected device should be the selected tab")
	}
}

func TestSettingsHideLinksServersAndBlocks(t *testing.T) {
	s := subscription.DefaultSettings()
	s.ShowWeb, s.ShowBackupDomains, s.ShowQR, s.ShowGuide, s.ShowStatus = false, false, false, false, false
	s.HiddenNodes = []string{hel.String()}
	page, out := build(t, s, alerttext.EN, subscription.Android, nil)
	if len(page.Servers) != 1 || len(page.Servers[0].Links) != 1 || page.Servers[0].Links[0].Kind != keys.LinkTLS {
		t.Fatalf("only the main Fake-TLS link of Amsterdam should stay: %+v", page.Servers)
	}
	for _, gone := range []string{"Helsinki", "webproxy", "backup.example.org", "data:image/png", "Install Telegram", "Access is active"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be hidden", gone)
		}
	}
}

func TestBackupDomainsGetTheirOwnLabel(t *testing.T) {
	_, out := build(t, subscription.DefaultSettings(), alerttext.EN, subscription.Android, nil)
	if !strings.Contains(out, "Standard link, backup domain backup.example.org") {
		t.Fatal("the backup domain link is not labelled with its domain")
	}
}

func TestLanguageStatusAndTitles(t *testing.T) {
	expires := time.Date(2026, 12, 31, 12, 0, 0, 0, time.Local)
	s := subscription.DefaultSettings()
	s.Title, s.Intro = "Мой прокси", "Привет!"
	_, ru := build(t, s, alerttext.RU, subscription.Android, &expires)
	for _, want := range []string{
		`lang="ru"`, "Мой прокси", "Привет!", "Доступ активен", "до 31 декабря 2026", "Установите Telegram", "Подключить", "Открыть Google Play",
		"«Соединение…»", "«Настройки прокси»", "Если не подключается", `href="?lang=en"`, "Серверов несколько",
	} {
		if !strings.Contains(ru, want) {
			t.Errorf("missing %q", want)
		}
	}
	_, en := build(t, subscription.DefaultSettings(), alerttext.EN, subscription.Desktop, nil)
	for _, want := range []string{`lang="en"`, "<title>Demo</title>", "with no end date", "Download Telegram Desktop", "Connection type", "Didn&#39;t connect within a minute? Try another way", "opens Telegram Desktop"} {
		if !strings.Contains(en, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestDetectPlatformAndLanguage(t *testing.T) {
	cases := map[string]subscription.Platform{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)": subscription.IOS,
		"Mozilla/5.0 (Linux; Android 14; Pixel 8)":               subscription.Android,
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64)":              subscription.Desktop,
	}
	for ua, want := range cases {
		if got := subscription.DetectPlatform(ua); got != want {
			t.Errorf("%s: %s", ua, got)
		}
	}
	if subscription.DetectLang("auto", "en-US,en;q=0.9") != alerttext.EN || subscription.DetectLang("auto", "uk-UA") != alerttext.RU {
		t.Fatal("browser language")
	}
	if subscription.DetectLang("en", "ru-RU") != alerttext.EN {
		t.Fatal("a pinned language must win over the browser")
	}
}

func TestRenderHasNoExternalScriptsOrKeyNames(t *testing.T) {
	_, out := build(t, subscription.DefaultSettings(), alerttext.RU, subscription.Android, nil)
	if strings.Contains(out, "<script src") {
		t.Fatal("external script")
	}
	if strings.Contains(out, "http://") {
		t.Fatal("plain http link on the page")
	}
}

func TestSettingsValidation(t *testing.T) {
	s := subscription.DefaultSettings()
	s.ShowWeb, s.ShowFakeTLS, s.Language = false, false, "de"
	f := s.Validate()
	if f["subscription_page.show_fake_tls"] == "" || f["subscription_page.language"] == "" {
		t.Fatalf("validation: %v", f)
	}
}

func TestErrorPageIsTranslated(t *testing.T) {
	var buf bytes.Buffer
	if err := subscription.RenderError(&buf, subscription.ErrorPage{Lang: "en", PanelName: "Demo", Theme: "dark", Message: "This link was not found."}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `lang="en"`) || !strings.Contains(out, "This link was not found.") || strings.Contains(out, "<script") {
		t.Fatalf("error page: %s", out)
	}
}

func TestIPhoneTabHidesWebLinksAndVisitorLanguageWins(t *testing.T) {
	_, out := build(t, subscription.DefaultSettings(), alerttext.RU, subscription.IOS, nil)
	for _, want := range []string{`<body data-platform="ios">`, `data-kind="web"`, `data-web-only`, `body[data-platform="ios"] .lnk[data-kind="web"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if subscription.PageLang("en", "ru", "ru-RU") != alerttext.EN || subscription.PageLang("", "ru", "en-US") != alerttext.RU {
		t.Fatal("the visitor's ?lang= must win, and the owner's fixed language otherwise")
	}
}

func TestNoQRMeansNoTalkOfScanning(t *testing.T) {
	s := subscription.DefaultSettings()
	s.ShowQR = false
	_, out := build(t, s, alerttext.RU, subscription.Android, nil)
	if strings.Contains(out, "QR-код") || !strings.Contains(out, "Все ссылки") {
		t.Fatal("without QR codes the block should not mention them")
	}
}
