package alerttext

import (
	"html"
	"regexp"
	"strings"
)

// Incident is one opened or closed alert as the workers see it.
type Incident struct {
	Kind      string
	Message   string
	Value     string
	Growth    string
	Recovered bool
}

var probeChecks = []string{"tls", "http", "faketls", "web"}

// valueShown lists the checks whose current value helps a reader; for the rest it is a state word.
var valueShown = map[string]bool{
	"dc_writers": true, "upstream_latency": true, "datacenters": true, "connect_attempts": true,
	"certificate_expiry": true, "tls_front_errors": true, "carrier_failures": true, "web_rejections": true,
	"https_response": true,
}

var dcKind = regexp.MustCompile(`^(dc|dc_writers)_(-?\d+)$`)

type about struct {
	title   string
	explain string
	source  string
	section string
	check   string
}

// describe works out what an alert kind is about, in the reader's language.
func (c *Catalog) describe(l Lang, kind, message string) about {
	k := strings.TrimPrefix(kind, "reliability_")
	if strings.HasPrefix(k, "probe_") {
		rest := strings.TrimPrefix(k, "probe_")
		if loc, ok := strings.CutSuffix(rest, "_stale"); ok {
			return about{title: c.text(l, "dashboard.alert_probe_stale", map[string]string{"location": loc}), explain: "probe_stale", source: "notify.source_probe"}
		}
		for _, check := range probeChecks {
			if loc, ok := strings.CutSuffix(rest, "_"+check); ok {
				return about{title: c.text(l, "dashboard.alert_probe_failed", map[string]string{"location": loc, "check": c.text(l, "probe."+check, nil)}), explain: "probe_failed", source: "notify.source_probe"}
			}
		}
	}
	if rest, ok := strings.CutPrefix(k, "diagnostic_"); ok {
		a := c.describeCheck(l, rest)
		a.source, a.section = "notify.source_scheduled", "diagnostics"
		if a.title == "" {
			a.title = message
		}
		return a
	}
	if m := dcKind.FindStringSubmatch(k); m != nil {
		return about{title: c.dcTitle(l, m[2]), explain: "dc_down", source: "notify.source_agent"}
	}
	if k == "upstream_failure" {
		return about{title: c.text(l, "dashboard.alert_upstream_failure", nil), explain: "route", source: "notify.source_agent"}
	}
	if c.has(l, "dashboard.alert_"+k) {
		return about{title: c.text(l, "dashboard.alert_"+k, nil), explain: k, source: "notify.source_agent"}
	}
	return about{title: message}
}

func (c *Catalog) describeCheck(l Lang, rest string) about {
	if addr, ok := strings.CutPrefix(rest, "public_addresses_"); ok {
		return about{title: c.text(l, "notify.title_address", map[string]string{"address": addr}), explain: "public_address"}
	}
	for i := strings.IndexByte(rest, '_'); i != -1; i = next(rest, i) {
		check := rest[i+1:]
		if m := dcKind.FindStringSubmatch(check); m != nil && m[1] == "dc_writers" {
			return about{title: c.dcTitle(l, m[2]), explain: "dc_writers", check: "dc_writers"}
		}
		if route, ok := strings.CutPrefix(check, "route_"); ok {
			return about{title: c.text(l, "notify.title_route", map[string]string{"route": route}), explain: "route", check: "route"}
		}
		if c.has(l, "web.check_"+check) {
			return about{title: c.text(l, "web.check_"+check, nil), explain: check, check: check}
		}
	}
	return about{}
}

func next(s string, i int) int {
	j := strings.IndexByte(s[i+1:], '_')
	if j == -1 {
		return -1
	}
	return i + 1 + j
}

func (c *Catalog) dcTitle(l Lang, id string) string {
	if n, ok := strings.CutPrefix(id, "-"); ok {
		return c.text(l, "notify.title_dc_media", map[string]string{"dc": n})
	}
	return c.text(l, "notify.title_dc", map[string]string{"dc": id})
}

// Incident renders an opened or a resolved alert.
func (c *Catalog) Incident(l Lang, n Node, in Incident, panelURL string) Message {
	a := c.describe(l, in.Kind, in.Message)
	if in.Recovered {
		return message([]string{
			"✅ " + c.htmlText(l, "notify.recovered_title", map[string]string{"name": "\x00"}),
			html.EscapeString(a.title),
		}).withName(n.Name)
	}
	lines := []string{"⚠️ " + c.htmlText(l, "notify.incident_title", map[string]string{"name": "\x00", "what": a.title})}
	if a.explain != "" && c.has(l, "notify.explain."+a.explain) {
		lines = append(lines, c.htmlText(l, "notify.explain."+a.explain, nil))
	}
	if in.Value != "" && valueShown[a.check] {
		value := c.htmlText(l, "notify.value", map[string]string{"value": in.Value})
		if alive, required, ok := strings.Cut(in.Value, " / "); ok && a.check == "dc_writers" {
			value = c.htmlText(l, "notify.value_dc", map[string]string{"alive": alive, "required": required})
		}
		if in.Growth != "" {
			value += " (" + c.htmlText(l, "notify.growth", map[string]string{"growth": in.Growth}) + ")"
		}
		lines = append(lines, value)
	}
	if a.source != "" {
		lines = append(lines, "<i>"+c.htmlText(l, a.source, nil)+"</i>")
	}
	lines = append(lines, c.link(l, panelURL, n.ID, a.section))
	return message(lines).withName(n.Name)
}
