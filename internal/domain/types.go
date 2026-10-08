// Package domain holds entity types and validation rules shared by the panel.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

type KeyType string

const (
	KeyShared   KeyType = "SHARED"
	KeyPersonal KeyType = "PERSONAL"
)

type KeyStatus string

const (
	KeyPending KeyStatus = "pending"
	KeyActive  KeyStatus = "active"
	KeyRevoked KeyStatus = "revoked"
)

type NodeStatus string

const (
	NodePending  NodeStatus = "pending"
	NodeOnline   NodeStatus = "online"
	NodeOffline  NodeStatus = "offline"
	NodeDegraded NodeStatus = "degraded"
)

type CarrierMode string

var AllCarrierModes = []CarrierMode{"https", "https-lanes", "websocket", "websocket-lanes"}

func (c CarrierMode) Valid() bool {
	for _, m := range AllCarrierModes {
		if m == c {
			return true
		}
	}
	return false
}

// ProfileLimits mirrors the relay's per-profile limits. Zero means "inherit global".
type ProfileLimits struct {
	MaxSessions             int `json:"max_sessions,omitempty"`
	MaxStreams              int `json:"max_streams,omitempty"`
	MaxBackendDialsInFlight int `json:"max_backend_dials_in_flight,omitempty"`
	NewSessionsPerMinute    int `json:"new_sessions_per_minute,omitempty"`
	NewSessionsBurst        int `json:"new_sessions_burst,omitempty"`
	NewStreamsPerMinute     int `json:"new_streams_per_minute,omitempty"`
	NewStreamsBurst         int `json:"new_streams_burst,omitempty"`
	MaxStreamsPerSession    int `json:"max_streams_per_session,omitempty"`
	MaxPendingPerSession    int `json:"max_pending_per_session,omitempty"`
}

func (l ProfileLimits) Validate() error {
	for name, v := range map[string]int{
		"max_sessions": l.MaxSessions, "max_streams": l.MaxStreams,
		"max_backend_dials_in_flight": l.MaxBackendDialsInFlight,
		"new_sessions_per_minute":     l.NewSessionsPerMinute, "new_sessions_burst": l.NewSessionsBurst,
		"new_streams_per_minute": l.NewStreamsPerMinute, "new_streams_burst": l.NewStreamsBurst,
		"max_streams_per_session": l.MaxStreamsPerSession, "max_pending_per_session": l.MaxPendingPerSession,
	} {
		if v < 0 {
			return fmt.Errorf("%s must be >= 0", name)
		}
	}
	if l.MaxStreams > 0 && l.MaxStreamsPerSession > l.MaxStreams {
		return errors.New("max_streams_per_session must not exceed max_streams")
	}
	if l.MaxStreams > 0 && l.MaxBackendDialsInFlight > l.MaxStreams {
		return errors.New("max_backend_dials_in_flight must not exceed max_streams")
	}
	return nil
}

var secretRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidateSecretHex(s string) error {
	if !secretRe.MatchString(s) {
		return errors.New("secret must be 32 lowercase hex characters")
	}
	return nil
}

// labelRe is the per-label DNS rule: 1..63 characters, alphanumeric at both ends, hyphens only in between.
var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

const errHostname = "hostname must be a lowercase DNS name with a dot, labels of 1-63 chars starting and ending alphanumeric, no scheme or path"

func ValidateHostname(h string) error {
	if len(h) == 0 || len(h) > 253 || !strings.Contains(h, ".") {
		return errors.New(errHostname)
	}
	for _, label := range strings.Split(h, ".") {
		if !labelRe.MatchString(label) {
			return errors.New(errHostname)
		}
	}
	return nil
}

// ProfileName derives the relay profile name for a key: "k" + first 12 hex of the uuid.
func ProfileName(keyID uuid.UUID) string {
	return "k" + strings.ReplaceAll(keyID.String(), "-", "")[:12]
}

type Engine string

const (
	EngineTProxy Engine = "tproxy"
	EngineTelemt Engine = "telemt"
)

func (e Engine) Valid() bool { return e == EngineTProxy || e == EngineTelemt }

const MaxTelemtQuotaBytes int64 = 100 * 1024 * 1024 * 1024 * 1024

// MaxTelemtCounter caps max_unique_ips and max_tcp_conns at one million.
const MaxTelemtCounter = 1_000_000

// MaxTelemtRateBps is the highest per-user rate telemt accepts, in bits per second.
const MaxTelemtRateBps int64 = 100_000_000_000

// TelemtLimits mirrors telemt's per-user limits.
type TelemtLimits struct {
	DataQuotaBytes   int64 `json:"data_quota_bytes,omitempty"`
	RateLimitUpBps   int64 `json:"rate_limit_up_bps,omitempty"`
	RateLimitDownBps int64 `json:"rate_limit_down_bps,omitempty"`
	MaxUniqueIPs     int   `json:"max_unique_ips,omitempty"`
	MaxTCPConns      int   `json:"max_tcp_conns,omitempty"`
	// DataQuotaPeriod resets the consumed quota at the start of every calendar period (UTC).
	// Empty means the quota is a lifetime total.
	DataQuotaPeriod QuotaPeriod `json:"data_quota_period,omitempty"`
}

type QuotaPeriod string

const (
	QuotaPeriodNone  QuotaPeriod = ""
	QuotaPeriodWeek  QuotaPeriod = "week"
	QuotaPeriodMonth QuotaPeriod = "month"
)

func (p QuotaPeriod) Valid() bool {
	return p == QuotaPeriodNone || p == QuotaPeriodWeek || p == QuotaPeriodMonth
}

// Start returns the beginning of the period containing t, in UTC: Monday 00:00 for a week, the
// 1st at 00:00 for a month. The zero time means the period never resets.
func (p QuotaPeriod) Start(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch p {
	case QuotaPeriodWeek:
		return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	case QuotaPeriodMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Time{}
}

// Next returns the start of the period after the one containing t.
func (p QuotaPeriod) Next(t time.Time) time.Time {
	start := p.Start(t)
	switch p {
	case QuotaPeriodWeek:
		return start.AddDate(0, 0, 7)
	case QuotaPeriodMonth:
		return start.AddDate(0, 1, 0)
	}
	return time.Time{}
}

func (l TelemtLimits) Validate() error {
	for name, v := range map[string]int64{
		"data_quota_bytes": l.DataQuotaBytes, "rate_limit_up_bps": l.RateLimitUpBps,
		"rate_limit_down_bps": l.RateLimitDownBps, "max_unique_ips": int64(l.MaxUniqueIPs),
		"max_tcp_conns": int64(l.MaxTCPConns),
	} {
		if v < 0 {
			return fmt.Errorf("%s must be >= 0", name)
		}
	}
	if l.DataQuotaBytes > MaxTelemtQuotaBytes {
		return fmt.Errorf("data_quota_bytes must not exceed %d (100 TB)", MaxTelemtQuotaBytes)
	}
	for name, v := range map[string]int64{"rate_limit_up_bps": l.RateLimitUpBps, "rate_limit_down_bps": l.RateLimitDownBps} {
		if v > MaxTelemtRateBps {
			return fmt.Errorf("%s must not exceed %d (100 Gbit/s)", name, MaxTelemtRateBps)
		}
	}
	for name, v := range map[string]int{"max_unique_ips": l.MaxUniqueIPs, "max_tcp_conns": l.MaxTCPConns} {
		if v > MaxTelemtCounter {
			return fmt.Errorf("%s must not exceed %d", name, MaxTelemtCounter)
		}
	}
	if !l.DataQuotaPeriod.Valid() {
		return errors.New("data_quota_period must be empty, week or month")
	}
	if l.DataQuotaPeriod != QuotaPeriodNone && l.DataQuotaBytes == 0 {
		return errors.New("data_quota_period needs data_quota_bytes")
	}
	return nil
}
