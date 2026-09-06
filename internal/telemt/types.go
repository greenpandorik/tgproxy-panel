package telemt

import (
	"encoding/json"
	"fmt"
)

// APIError is a telemt error envelope ({"ok":false,"error":{code,message}}) or a non-envelope
// HTTP failure. It never carries request headers, so it is safe to log.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telemt api %d %s: %s", e.Status, e.Code, e.Message)
}

// envelope is the shape every /v1 response uses.
type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Revision string          `json:"revision"`
	Error    *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// User is telemt's UserInfo plus the secret, which the API returns only from create and
// rotate-secret responses (list and get views never expose it).
type User struct {
	Username           string `json:"username"`
	Secret             string `json:"secret,omitempty"`
	Enabled            bool   `json:"enabled"`
	InRuntime          bool   `json:"in_runtime"`
	ExpirationRFC3339  string `json:"expiration_rfc3339,omitempty"`
	DataQuotaBytes     uint64 `json:"data_quota_bytes,omitempty"`
	RateLimitUpBps     uint64 `json:"rate_limit_up_bps,omitempty"`
	RateLimitDownBps   uint64 `json:"rate_limit_down_bps,omitempty"`
	MaxUniqueIPs       uint64 `json:"max_unique_ips,omitempty"`
	MaxTCPConns        uint64 `json:"max_tcp_conns,omitempty"`
	CurrentConnections uint64 `json:"current_connections,omitempty"`
	ActiveUniqueIPs    uint64 `json:"active_unique_ips,omitempty"`
	TotalOctets        uint64 `json:"total_octets,omitempty"`
}

// CreateUserRequest is POST /v1/users. Optional fields are pointers because telemt reads a
// present-but-null value as "remove the override"; an omitted field means "use the default".
type CreateUserRequest struct {
	Username          string  `json:"username"`
	Secret            string  `json:"secret,omitempty"`
	MaxTCPConns       *uint64 `json:"max_tcp_conns,omitempty"`
	ExpirationRFC3339 *string `json:"expiration_rfc3339,omitempty"`
	DataQuotaBytes    *uint64 `json:"data_quota_bytes,omitempty"`
	RateLimitUpBps    *uint64 `json:"rate_limit_up_bps,omitempty"`
	RateLimitDownBps  *uint64 `json:"rate_limit_down_bps,omitempty"`
	MaxUniqueIPs      *uint64 `json:"max_unique_ips,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
}

// PatchUserRequest is PATCH /v1/users/{name} with JSON Merge Patch semantics: an omitted field
// is unchanged, an explicit null removes the per-user entry. Set the field pointer to change a
// value; list the JSON name in Clear to remove it.
type PatchUserRequest struct {
	Secret            *string `json:"secret,omitempty"`
	MaxTCPConns       *uint64 `json:"max_tcp_conns,omitempty"`
	ExpirationRFC3339 *string `json:"expiration_rfc3339,omitempty"`
	DataQuotaBytes    *uint64 `json:"data_quota_bytes,omitempty"`
	RateLimitUpBps    *uint64 `json:"rate_limit_up_bps,omitempty"`
	RateLimitDownBps  *uint64 `json:"rate_limit_down_bps,omitempty"`
	MaxUniqueIPs      *uint64 `json:"max_unique_ips,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`

	// Clear lists JSON field names to send as an explicit null (remove the override).
	Clear []string `json:"-"`
}

func (p PatchUserRequest) MarshalJSON() ([]byte, error) {
	type alias PatchUserRequest
	raw, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	if len(p.Clear) == 0 {
		return raw, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for _, k := range p.Clear {
		m[k] = json.RawMessage("null")
	}
	return json.Marshal(m)
}

// createUserResponse is the {user, secret} pair returned by create and rotate-secret.
type createUserResponse struct {
	User   User   `json:"user"`
	Secret string `json:"secret"`
}

type HealthData struct {
	Status   string `json:"status"`
	ReadOnly bool   `json:"read_only"`
}

type ReadyData struct {
	Ready            bool   `json:"ready"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
	AdmissionOpen    bool   `json:"admission_open"`
	HealthyUpstreams int    `json:"healthy_upstreams"`
	TotalUpstreams   int    `json:"total_upstreams"`
}

type SystemInfo struct {
	Version       string  `json:"version"`
	TargetArch    string  `json:"target_arch"`
	ConfigPath    string  `json:"config_path"`
	ConfigHash    string  `json:"config_hash"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

// PatchConfigResult is PatchConfigResponse.
type PatchConfigResult struct {
	Revision               string          `json:"revision"`
	RestartRequired        bool            `json:"restart_required"`
	RuntimeReloadRequired  bool            `json:"runtime_reload_required"`
	ProcessRestartRequired bool            `json:"process_restart_required"`
	DeferredProcessFields  []string        `json:"deferred_process_fields"`
	Changed                []string        `json:"changed"`
	Reload                 *ReloadAccepted `json:"reload"`
}

// ReloadRequest is POST /v1/system/reload. Mode is "instant" (cancels the previous
// generation's sessions) or "drain" (lets them finish within TimeoutSecs).
type ReloadRequest struct {
	Mode          string `json:"mode,omitempty"`
	TimeoutSecs   int    `json:"timeout_secs,omitempty"`
	FailurePolicy string `json:"failure_policy,omitempty"`
}

type ReloadAccepted struct {
	ReloadID         int64  `json:"reload_id"`
	TargetGeneration int64  `json:"target_generation"`
	ConfigRevision   string `json:"config_revision"`
	State            string `json:"state"`
	Mode             string `json:"mode"`
	FailurePolicy    string `json:"failure_policy"`
}

type ReloadStatusData struct {
	ReloadID              int64    `json:"reload_id"`
	State                 string   `json:"state"`
	Error                 string   `json:"error,omitempty"`
	Warnings              []string `json:"warnings,omitempty"`
	DeferredProcessFields []string `json:"deferred_process_fields,omitempty"`
}

// Terminal reports whether the reload operation has finished (successfully or not).
func (r ReloadStatusData) Terminal() bool {
	switch r.State {
	case "succeeded", "failed", "rolled_back":
		return true
	}
	return false
}

type ConnectionUser struct {
	Username           string `json:"username"`
	CurrentConnections uint64 `json:"current_connections"`
	TotalOctets        uint64 `json:"total_octets"`
}

type ConnectionsSummaryPayload struct {
	Totals struct {
		CurrentConnections       uint64 `json:"current_connections"`
		CurrentConnectionsMe     uint64 `json:"current_connections_me"`
		CurrentConnectionsDirect uint64 `json:"current_connections_direct"`
		ActiveUsers              int    `json:"active_users"`
	} `json:"totals"`
	Top struct {
		Limit         int              `json:"limit"`
		ByConnections []ConnectionUser `json:"by_connections"`
		ByThroughput  []ConnectionUser `json:"by_throughput"`
	} `json:"top"`
}

// ConnectionsSummary is GET /v1/runtime/connections/summary. Data is nil when the runtime edge
// feature is disabled or the snapshot source is unavailable; Reason says which.
type ConnectionsSummary struct {
	Enabled bool                       `json:"enabled"`
	Reason  string                     `json:"reason,omitempty"`
	Data    *ConnectionsSummaryPayload `json:"data"`
}
