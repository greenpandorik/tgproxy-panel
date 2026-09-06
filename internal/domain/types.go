// Package domain holds entity types and validation rules shared by the panel.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

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

// labelRe is the per-label DNS rule: 1..63 characters, alphanumeric at both
// ends, hyphens only in between. Applied label by label so that "a..b" (empty
// label) and "a.-b.c" (label starting with a hyphen) are rejected — the old
// whole-string charset check let both through, and they only surface later as
// a failed ACME issuance on the node.
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

// Engine selects the proxy stack a node runs: the original tproxy-server +
// MTProxy pair, or a single telemt process. It is fixed when the node is
// created, because the two stacks have different on-node layouts.
type Engine string

const (
	EngineTProxy Engine = "tproxy"
	EngineTelemt Engine = "telemt"
)

func (e Engine) Valid() bool { return e == EngineTProxy || e == EngineTelemt }

// MaxTelemtQuotaBytes caps a per-key traffic quota at 100 TB. The cap is not a
// product limit so much as a typo guard: telemt stores the quota as a byte
// count, so an operator who means "100 GB" and pastes an extra three zeros gets
// a rejection here instead of a key that is effectively unmetered.
const MaxTelemtQuotaBytes int64 = 100 * 1024 * 1024 * 1024 * 1024

// MaxTelemtCounter caps max_unique_ips and max_tcp_conns at one million. Both cross the wire
// to the agent as proto3 uint32 and are stored by telemt as 32-bit counters, so a value above
// 4 294 967 295 wraps - 4 294 967 297 becomes 1, turning "effectively unlimited" into "one IP"
// and locking the key out. A million is far past any real per-key ceiling and well inside the
// range, so the wrap can no longer be reached. Zero still means "no limit of this kind".
const MaxTelemtCounter = 1_000_000

// TelemtLimits mirrors telemt's per-user limits. Zero means "unset", which
// telemt reads as "no limit of this kind", so the zero value is a valid,
// unrestricted key.
type TelemtLimits struct {
	DataQuotaBytes   int64 `json:"data_quota_bytes,omitempty"`
	RateLimitUpBps   int64 `json:"rate_limit_up_bps,omitempty"`
	RateLimitDownBps int64 `json:"rate_limit_down_bps,omitempty"`
	MaxUniqueIPs     int   `json:"max_unique_ips,omitempty"`
	MaxTCPConns      int   `json:"max_tcp_conns,omitempty"`
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
	for name, v := range map[string]int{"max_unique_ips": l.MaxUniqueIPs, "max_tcp_conns": l.MaxTCPConns} {
		if v > MaxTelemtCounter {
			return fmt.Errorf("%s must not exceed %d", name, MaxTelemtCounter)
		}
	}
	return nil
}
