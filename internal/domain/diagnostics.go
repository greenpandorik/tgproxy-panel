package domain

import "time"

type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
	// CheckNotAvailable is a check the panel could not perform, which is neither a pass nor
	// a failure and is excluded from the passed/total count.
	CheckNotAvailable CheckStatus = "not_available"
)

type DiagnosticsStatus string

const (
	DiagnosticsHealthy  DiagnosticsStatus = "healthy"
	DiagnosticsDegraded DiagnosticsStatus = "degraded"
	DiagnosticsOffline  DiagnosticsStatus = "offline"
	DiagnosticsUnknown  DiagnosticsStatus = "unknown"
)

type DiagnosticsTrigger string

const (
	TriggerManual      DiagnosticsTrigger = "manual"
	TriggerScheduled   DiagnosticsTrigger = "scheduled"
	TriggerPostInstall DiagnosticsTrigger = "post_install"
	TriggerPostUpdate  DiagnosticsTrigger = "post_update"
	TriggerAlert       DiagnosticsTrigger = "alert"
)

// Diagnostic check group keys.
const (
	GroupDNS          = "dns"
	GroupTransport    = "tcp_tls"
	GroupReverseProxy = "reverse_proxy"
	GroupWebTransport = "web_transport"
	GroupTelemt       = "telemt"
	GroupTelegram     = "telegram"
)

// DiagnosticCheck is one line of a diagnostics report. Value is nil when there is no figure
// to show, which is not the same as an empty one.
type DiagnosticCheck struct {
	Key    string      `json:"key"`
	Status CheckStatus `json:"status"`
	Value  *string     `json:"value"`
	Detail *string     `json:"detail"`
}

type DiagnosticGroup struct {
	Key    string            `json:"key"`
	Checks []DiagnosticCheck `json:"checks"`
}

// DiagnosticsRun is one stored diagnostics pass. Passed and Total count only the checks that
// ran: a not_available check is in neither, so "11 / 11 passed" never hides a check the panel
// silently skipped.
type DiagnosticsRun struct {
	ID         int64              `json:"id"`
	NodeID     string             `json:"node_id"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at"`
	Status     DiagnosticsStatus  `json:"overall_status"`
	Trigger    DiagnosticsTrigger `json:"trigger"`
	Groups     []DiagnosticGroup  `json:"groups"`
	Passed     int                `json:"passed"`
	Total      int                `json:"total"`
	NotRun     int                `json:"not_run"`
}

// Tally counts the checks by outcome and derives the overall status. A failed check degrades
// the run; checks the panel could not perform never do, because not knowing is not evidence
// of a fault.
func (r *DiagnosticsRun) Tally() {
	r.Passed, r.Total, r.NotRun = 0, 0, 0
	failed, warned := 0, 0
	for _, g := range r.Groups {
		for _, c := range g.Checks {
			switch c.Status {
			case CheckNotAvailable:
				r.NotRun++
				continue
			case CheckOK:
				r.Passed++
			case CheckWarn:
				warned++
			case CheckFail:
				failed++
			}
			r.Total++
		}
	}
	switch {
	case r.Total == 0:
		r.Status = DiagnosticsUnknown
	case failed > 0 || warned > 0:
		r.Status = DiagnosticsDegraded
	default:
		r.Status = DiagnosticsHealthy
	}
}
