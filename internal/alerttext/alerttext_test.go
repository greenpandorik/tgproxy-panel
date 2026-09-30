package alerttext

import (
	"strings"
	"testing"
)

var node = Node{ID: "7c4a03d6-2ba1-4b0b-8179-86494e15179f", Name: "Test2", Hostname: "test2.example.com"}

func contains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %q in:\n%s", w, got)
		}
	}
}

func TestPartialDCCoverageReadsAsAMediaDatacentre(t *testing.T) {
	c := Default()
	in := Incident{Kind: "diagnostic_telegram_dc_writers_-5", Message: "Scheduled check: telegram / dc_writers_-5: partial DC coverage", Value: "2 / 3"}
	ru := c.Incident(RU, node, in, "https://panel.example.com")
	contains(t, ru.HTML, "⚠️ <b>Test2</b>: Медиа-датацентр Telegram 5", "постоянные соединения", "Соединений сейчас: 2 из 3",
		"<i>Плановая проверка сервера</i>", `href="https://panel.example.com/nodes/7c4a03d6-2ba1-4b0b-8179-86494e15179f?section=diagnostics"`)
	if strings.Contains(ru.HTML, "partial DC coverage") {
		t.Fatalf("raw detail leaked: %s", ru.HTML)
	}
	en := c.Incident(EN, node, in, "https://panel.example.com")
	contains(t, en.HTML, "Test2</b>: Telegram media DC 5", "standing connections", "Connections now: 2 of 3", "Scheduled server check", "Open the server")
}

func TestAgentLinkAndItsRecovery(t *testing.T) {
	c := Default()
	open := c.Incident(RU, node, Incident{Kind: "diagnostic_telemt_agent_link", Message: "Scheduled check: telemt / agent_link: the node's agent did not answer", Value: "error"}, "")
	contains(t, open.HTML, "Связь с агентом", "не ответил панели вовремя")
	if strings.Contains(open.HTML, "Сейчас:") || strings.Contains(open.HTML, "<a ") {
		t.Fatalf("state word or link without a panel URL: %s", open.HTML)
	}
	closed := c.Incident(RU, node, Incident{Kind: "diagnostic_telemt_agent_link", Recovered: true}, "")
	contains(t, closed.HTML, "✅ <b>Test2</b>: снова в порядке", "Связь с агентом")
}

func TestReliabilityAndProbeKindsLoseTheirPrefix(t *testing.T) {
	c := Default()
	contains(t, c.Incident(RU, node, Incident{Kind: "reliability_disk_pressure"}, "").HTML, "Заканчивается место на диске", "90%", "Сообщает агент")
	contains(t, c.Incident(RU, node, Incident{Kind: "reliability_dc_2"}, "").HTML, "Датацентр Telegram 2", "нет ни одного рабочего соединения")
	contains(t, c.Incident(EN, node, Incident{Kind: "reliability_probe_ams_stale"}, "").HTML, "ams", "Check from another network")
	contains(t, c.Incident(EN, node, Incident{Kind: "reliability_probe_ams_faketls"}, "").HTML, "ams", "may be blocked there")
}

func TestUnknownKindFallsBackToTheMessage(t *testing.T) {
	got := Default().Incident(EN, node, Incident{Kind: "something_new", Message: "a <new> thing"}, "")
	contains(t, got.HTML, "a &lt;new&gt; thing")
}

func TestNamesAreEscapedAndPlainTextHasNoTags(t *testing.T) {
	c := Default()
	n := Node{ID: "x", Name: "<b>evil</b>", Hostname: "h&h.test"}
	m := c.Offline(RU, n, "https://panel.example.com")
	contains(t, m.HTML, "&lt;b&gt;evil&lt;/b&gt;", "h&amp;h.test")
	if strings.Contains(m.Plain, "<a ") || !strings.Contains(m.Plain, "<b>evil</b> не на связи") || !strings.Contains(m.Plain, "Открыть сервер: https://panel.example.com/nodes/x") {
		t.Fatalf("plain text: %q", m.Plain)
	}
}

func TestApplyFailedShowsOnlyTheFirstLine(t *testing.T) {
	m := Default().ApplyFailed(EN, node, "boom: first line\nstack trace", "")
	contains(t, m.HTML, "changes could not be applied", "<code>boom: first line</code>")
	if strings.Contains(m.HTML, "stack trace") {
		t.Fatalf("second line leaked: %s", m.HTML)
	}
}

func TestBothLanguagesHaveEveryNotifyKey(t *testing.T) {
	c := Default()
	for key := range c.strings[RU] {
		if strings.HasPrefix(key, "notify.") && !c.has(EN, key) {
			t.Errorf("%s is missing in English", key)
		}
	}
	if len(c.strings[RU]) == 0 {
		t.Fatal("the embedded translations did not load")
	}
}
