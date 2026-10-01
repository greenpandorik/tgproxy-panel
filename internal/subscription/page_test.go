package subscription_test

import (
	"bytes"
	"html"
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

func build(t *testing.T, s subscription.Settings, l alerttext.Lang, expires *time.Time) (subscription.Page, string) {
	t.Helper()
	return buildWith(t, s, l, expires, subscription.Branding{PanelName: "Demo", PrimaryColor: "#3fc0d6", AccentColor: "#20c997", PrimaryInk: "#0b0e11", Theme: "dark"})
}

func buildWith(t *testing.T, s subscription.Settings, l alerttext.Lang, expires *time.Time, b subscription.Branding) (subscription.Page, string) {
	t.Helper()
	page, err := subscription.Build(subscription.Input{
		Settings: s, Lang: l, Locations: locations(), ExpiresAt: expires, QRSize: 128, Branding: b,
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

func TestServerCardsOfferFakeTLSFirstThenWeb(t *testing.T) {
	page, out := build(t, subscription.DefaultSettings(), alerttext.RU, nil)
	if len(page.Servers) != 2 {
		t.Fatalf("servers: %+v", page.Servers)
	}
	ams := page.Servers[0]
	if len(ams.Buttons) != 2 || ams.Buttons[0].Kind != keys.LinkTLS || !ams.Buttons[0].Primary || ams.Buttons[1].Kind != keys.LinkWeb || ams.Buttons[1].Primary {
		t.Fatalf("Amsterdam: Fake-TLS first and highlighted, WEB second: %+v", ams.Buttons)
	}
	if len(ams.Links) != 3 || ams.Links[0].Kind != keys.LinkTLS || ams.Links[1].Kind != keys.LinkTLS || ams.Links[2].Kind != keys.LinkWeb {
		t.Fatalf("Amsterdam links: main Fake-TLS, its backup, then WEB: %+v", ams.Links)
	}
	if !strings.Contains(ams.Buttons[0].Href, "secret=ee") || strings.Contains(ams.Buttons[0].Href, "6261636b7570") {
		t.Fatalf("the Fake-TLS button opens the main domain: %s", ams.Buttons[0].Href)
	}
	hel := page.Servers[1]
	if len(hel.Buttons) != 1 || hel.Buttons[0].Kind != keys.LinkWeb || !hel.Buttons[0].Primary {
		t.Fatalf("a tproxy server offers WEB only, as the main button: %+v", hel.Buttons)
	}
	for _, want := range []string{
		"Подключить через Fake-TLS", "Для всех приложений Telegram, включая iPhone",
		"Подключить через WEB", "Для Telegram на Android и компьютере. Похоже на обычный сайт",
		"Ссылки и QR-коды", "Попробуйте другой сервер", `<h2>Amsterdam</h2>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, gone := range []string{"data-platform", `role="tab"`, "Установите Telegram", "Google Play", "Как выключить", "class=\"host\""} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be gone from the page", gone)
		}
	}
}

func TestOwnButtonTextsReplaceTheStandardOnes(t *testing.T) {
	s := subscription.DefaultSettings()
	s.TLSButtonRU, s.WebNoteRU = "Подключиться", "Работает в браузере Telegram"
	s.TLSNoteEN = "Works everywhere"
	_, ru := build(t, s, alerttext.RU, nil)
	for _, want := range []string{">Подключиться<", "Работает в браузере Telegram", "Для всех приложений Telegram, включая iPhone", "Подключить через WEB"} {
		if !strings.Contains(ru, want) {
			t.Errorf("RU missing %q", want)
		}
	}
	if strings.Contains(ru, "Works everywhere") || strings.Contains(ru, "Похоже на обычный сайт") {
		t.Error("the Russian page must use the Russian texts and drop the replaced one")
	}
	_, en := build(t, s, alerttext.EN, nil)
	for _, want := range []string{"Connect via Fake-TLS", "Works everywhere", "Looks like an ordinary website"} {
		if !strings.Contains(en, want) {
			t.Errorf("EN missing %q", want)
		}
	}
}

func TestSettingsHideLinksServersAndBlocks(t *testing.T) {
	s := subscription.DefaultSettings()
	s.ShowWeb, s.ShowBackupDomains, s.ShowQR, s.ShowStatus = false, false, false, false
	s.HiddenNodes = []string{hel.String()}
	page, out := build(t, s, alerttext.EN, nil)
	if len(page.Servers) != 1 || len(page.Servers[0].Links) != 1 || page.Servers[0].Links[0].Kind != keys.LinkTLS || len(page.Servers[0].Buttons) != 1 {
		t.Fatalf("only the main Fake-TLS link of Amsterdam should stay: %+v", page.Servers)
	}
	for _, gone := range []string{"Helsinki", "webproxy", "backup.example.org", "data:image/png", "Access never expires", "QR"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be hidden", gone)
		}
	}
	if !strings.Contains(out, "Wait a minute and try again") {
		t.Error("with one server the page should not suggest another one")
	}
}

func TestBackupDomainsGetTheirOwnLabel(t *testing.T) {
	_, out := build(t, subscription.DefaultSettings(), alerttext.EN, nil)
	if !strings.Contains(out, "Fake-TLS, backup address backup.example.org") {
		t.Fatal("the backup domain link is not labelled with its domain")
	}
}

func TestStatusSaysWhenAccessNeverExpires(t *testing.T) {
	_, ru := build(t, subscription.DefaultSettings(), alerttext.RU, nil)
	if !strings.Contains(ru, "<b>Доступ бессрочный</b>") || strings.Contains(ru, `class="until"`) {
		t.Error("access without an end date should read as never expiring")
	}
	expires := time.Date(2026, 12, 31, 12, 0, 0, 0, time.Local)
	_, ru = build(t, subscription.DefaultSettings(), alerttext.RU, &expires)
	if !strings.Contains(ru, "<b>Доступ активен</b>") || !strings.Contains(ru, "до 31 декабря 2026") {
		t.Error("access with an end date should name it")
	}
	_, en := build(t, subscription.DefaultSettings(), alerttext.EN, nil)
	if !strings.Contains(en, "Access never expires") {
		t.Error("English status")
	}
}

func TestLanguageAndTitles(t *testing.T) {
	s := subscription.DefaultSettings()
	s.TitleRU, s.IntroRU = "Мой прокси", "Привет!"
	s.TitleEN = "Our proxy"
	_, ru := build(t, s, alerttext.RU, nil)
	for _, want := range []string{`lang="ru"`, "<title>Мой прокси</title>", "Привет!", `href="?lang=en"`} {
		if !strings.Contains(ru, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(ru, "Our proxy") {
		t.Error("the English title leaked onto the Russian page")
	}
	_, en := build(t, s, alerttext.EN, nil)
	if !strings.Contains(en, "<title>Our proxy</title>") || strings.Contains(en, "Привет!") || !strings.Contains(en, "Press Connect next to any server") {
		t.Error("the English page should use the English title and the standard English intro")
	}
	if subscription.DetectLang("auto", "en-US,en;q=0.9") != alerttext.EN || subscription.DetectLang("auto", "uk-UA") != alerttext.RU {
		t.Fatal("browser language")
	}
	if subscription.DetectLang("en", "ru-RU") != alerttext.EN {
		t.Fatal("a pinned language must win over the browser")
	}
	if subscription.PageLang("en", "ru", "ru-RU") != alerttext.EN || subscription.PageLang("", "ru", "en-US") != alerttext.RU {
		t.Fatal("the visitor's ?lang= must win, and the owner's fixed language otherwise")
	}
}

func TestSupportButtonFollowsTheSettings(t *testing.T) {
	branded := subscription.Branding{PanelName: "Demo", Theme: "dark", SupportLink: "https://t.me/branding_support"}
	_, out := buildWith(t, subscription.DefaultSettings(), alerttext.RU, nil, branded)
	if !strings.Contains(out, `href="https://t.me/branding_support"`) || !strings.Contains(out, "Написать в поддержку") || !strings.Contains(out, "напишите в поддержку") {
		t.Fatal("with no own link the page falls back to the branding link")
	}

	s := subscription.DefaultSettings()
	s.SupportURL, s.SupportLabelRU = "tg://resolve?domain=helpdesk", "Спросить администратора"
	_, out = buildWith(t, s, alerttext.RU, nil, branded)
	if !strings.Contains(out, `href="tg://resolve?domain=helpdesk"`) || !strings.Contains(out, "Спросить администратора") || strings.Contains(out, "branding_support") {
		t.Fatal("the page's own support link and label win")
	}

	s.HideSupport = true
	_, out = buildWith(t, s, alerttext.RU, nil, branded)
	if strings.Contains(out, "helpdesk") || strings.Contains(out, "branding_support") || !strings.Contains(out, "тому, кто прислал вам ссылку") {
		t.Fatal("a hidden support button leaves only the plain advice")
	}

	evil := subscription.Branding{PanelName: "Demo", Theme: "dark", SupportLink: "javascript:alert(1)"}
	_, out = buildWith(t, subscription.DefaultSettings(), alerttext.RU, nil, evil)
	if strings.Contains(out, "javascript:") || strings.Contains(out, "Написать в поддержку") {
		t.Fatal("an unsafe link must never become a button")
	}
}

func TestSafeSupportURL(t *testing.T) {
	ok := []string{"https://t.me/help", "http://example.com/support", "tg://resolve?domain=help", "mailto:help@example.com"}
	bad := []string{"javascript:alert(1)", "data:text/html,x", "https://", "https://a b.com", "//example.com", "ftp://example.com", "https://x.com/\"x\"", "vbscript:x"}
	for _, u := range ok {
		if subscription.SafeSupportURL(u) == "" {
			t.Errorf("%s should be allowed", u)
		}
	}
	for _, u := range bad {
		if subscription.SafeSupportURL(u) != "" {
			t.Errorf("%s should be refused", u)
		}
	}
}

func TestRenderHasNoExternalScriptsOrKeyNames(t *testing.T) {
	_, out := build(t, subscription.DefaultSettings(), alerttext.RU, nil)
	if strings.Contains(out, "<script src") {
		t.Fatal("external script")
	}
	if strings.Contains(out, "http://") {
		t.Fatal("plain http link on the page")
	}
	if !strings.Contains(out, `<meta name="referrer" content="no-referrer">`) {
		t.Fatal("the page must not hand its address to sites it links to")
	}
}

func TestSettingsValidation(t *testing.T) {
	s := subscription.DefaultSettings()
	s.ShowWeb, s.ShowFakeTLS, s.Language = false, false, "de"
	s.TitleEN = strings.Repeat("я", 81)
	s.TLSButtonRU = strings.Repeat("я", 41)
	s.WebNoteEN = strings.Repeat("я", 141)
	s.SupportURL = "javascript:alert(1)"
	f := s.Validate()
	for _, key := range []string{"show_fake_tls", "language", "title_en", "tls_button_ru", "web_note_en", "support_url"} {
		if f["subscription_page."+key] == "" {
			t.Errorf("%s should be refused: %v", key, f)
		}
	}
	if f := subscription.DefaultSettings().Validate(); len(f) != 0 {
		t.Fatalf("defaults must be valid: %v", f)
	}
}

func TestErrorPageIsTranslated(t *testing.T) {
	var buf bytes.Buffer
	if err := subscription.RenderError(&buf, subscription.ErrorPage{Lang: "en", Theme: "dark", Message: "This link was not found."}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `lang="en"`) || !strings.Contains(out, "This link was not found.") || strings.Contains(out, "<script") {
		t.Fatalf("error page: %s", out)
	}
	if !strings.Contains(out, "<h1>Connect Telegram</h1>") || strings.Contains(out, "Panel") {
		t.Fatal("the error page must not name the panel behind it")
	}
}

func TestNoQRMeansNoTalkOfScanning(t *testing.T) {
	s := subscription.DefaultSettings()
	s.ShowQR = false
	_, out := build(t, s, alerttext.RU, nil)
	if strings.Contains(out, "QR") || !strings.Contains(out, "<summary>Ссылки</summary>") {
		t.Fatal("without QR codes the links block should not mention them")
	}
}

func TestPreviewDoesNotTouchTheVisitorsMemory(t *testing.T) {
	page, _ := build(t, subscription.DefaultSettings(), alerttext.RU, nil)
	page.Preview = true
	var buf bytes.Buffer
	if err := subscription.Render(&buf, page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "var remember = false") {
		t.Fatal("the preview must not read or save the language")
	}
}

func TestPageCarriesAFavicon(t *testing.T) {
	_, out := build(t, subscription.DefaultSettings(), alerttext.RU, nil)
	out = html.UnescapeString(out)
	if !strings.Contains(out, `<link rel="icon" href="data:image/svg+xml;base64,`) {
		t.Fatal("the page needs a favicon of its own")
	}

	png := subscription.FaviconDataURI("image/png", []byte("\x89PNG\r\n\x1a\nfake"))
	if !strings.HasPrefix(png, "data:image/png;base64,") {
		t.Fatalf("png favicon: %q", png)
	}
	branded := subscription.Branding{PanelName: "Demo", Theme: "dark", PrimaryColor: "#c4ed79", FaviconDataURI: png}
	_, out = buildWith(t, subscription.DefaultSettings(), alerttext.RU, nil, branded)
	out = html.UnescapeString(out)
	if !strings.Contains(out, `<link rel="icon" href="`+png+`">`) {
		t.Fatal("the branding favicon should be used")
	}

	evil := subscription.Branding{PanelName: "Demo", Theme: "dark", FaviconDataURI: "javascript:alert(1)"}
	_, out = buildWith(t, subscription.DefaultSettings(), alerttext.RU, nil, evil)
	out = html.UnescapeString(out)
	if strings.Contains(out, "javascript:") || !strings.Contains(out, `<link rel="icon" href="data:image/svg+xml;base64,`) {
		t.Fatal("anything but an image falls back to the standard favicon")
	}

	if subscription.FaviconDataURI("text/html", []byte("<script>")) != "" || subscription.FaviconDataURI("image/png", make([]byte, subscription.MaxFaviconBytes+1)) != "" {
		t.Fatal("only small images may be embedded")
	}

	var buf bytes.Buffer
	if err := subscription.RenderError(&buf, subscription.ErrorPage{Lang: "ru", Theme: "dark", Message: "x", Favicon: png}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.UnescapeString(buf.String()), `<link rel="icon" href="`+png+`">`) {
		t.Fatal("the error page should carry the favicon too")
	}
}
