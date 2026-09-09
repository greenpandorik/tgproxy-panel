package nodediag

import (
	"net"
	"sort"
	"strconv"
	"strings"

	"tgwebproxy/internal/domain"
)

type builder struct {
	key    string
	checks []domain.DiagnosticCheck
}

func newGroup(key string) *builder { return &builder{key: key} }

func (b *builder) add(c domain.DiagnosticCheck) { b.checks = append(b.checks, c) }

func (b *builder) group() domain.DiagnosticGroup {
	if b.checks == nil {
		b.checks = []domain.DiagnosticCheck{}
	}
	return domain.DiagnosticGroup{Key: b.key, Checks: b.checks}
}

func check(key string, status domain.CheckStatus, value, detail string) domain.DiagnosticCheck {
	c := domain.DiagnosticCheck{Key: key, Status: status}
	if value != "" {
		c.Value = &value
	}
	if detail != "" {
		c.Detail = &detail
	}
	return c
}

func ok(key, value, detail string) domain.DiagnosticCheck {
	return check(key, domain.CheckOK, value, detail)
}

func warn(key, value, detail string) domain.DiagnosticCheck {
	return check(key, domain.CheckWarn, value, detail)
}

func fail(key, value, detail string) domain.DiagnosticCheck {
	return check(key, domain.CheckFail, value, detail)
}

// na is a check the panel could not perform; detail always says why.
func na(key, detail string) domain.DiagnosticCheck {
	return check(key, domain.CheckNotAvailable, "", detail)
}

func joinIPs(addrs []net.IPAddr) string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	return strings.Join(out, ", ")
}

func joinStrings(items []string) string { return strings.Join(items, ", ") }

func sortedCarriers(totals map[string]float64) []string {
	out := make([]string, 0, len(totals))
	for name, v := range totals {
		if v > 0 {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func formatCount(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func formatMs(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + " ms" }
