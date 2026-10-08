package reliability

import (
	"errors"
	"regexp"
	"time"
)

const AuthenticatedMTProto = "authenticated_mtproto"

type ProbeCheck struct {
	Method    string `json:"method,omitempty"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
}
type ProbeReport struct {
	NodeID   string     `json:"node_id"`
	Location string     `json:"location"`
	At       time.Time  `json:"at"`
	TLS      ProbeCheck `json:"tls"`
	HTTP     ProbeCheck `json:"http"`
	FakeTLS  ProbeCheck `json:"faketls"`
	WEB      ProbeCheck `json:"web"`
}

var probeName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (p ProbeReport) Validate(now time.Time) error {
	if !probeName.MatchString(p.Location) {
		return errors.New("invalid probe location")
	}
	if now.Sub(p.At) > 2*time.Minute || p.At.Sub(now) > 30*time.Second {
		return errors.New("probe timestamp is outside the allowed clock window")
	}
	for _, c := range []ProbeCheck{p.TLS, p.HTTP, p.FakeTLS, p.WEB} {
		if c.Method != "" && c.Method != AuthenticatedMTProto {
			return errors.New("invalid probe method")
		}
		if c.Method != "" && c.Status == "not_run" {
			return errors.New("unexecuted probe cannot specify a method")
		}
		if c.Status != "ok" && c.Status != "failed" && c.Status != "not_run" {
			return errors.New("invalid probe status")
		}
		if c.LatencyMS < 0 || c.LatencyMS > 120000 {
			return errors.New("invalid probe latency")
		}
	}
	return nil
}
