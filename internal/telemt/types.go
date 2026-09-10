package telemt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError is a telemt error envelope ({"ok":false,"error":{code,message}}) or a non-envelope HTTP failure.
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
	UserAdTag          string `json:"user_ad_tag,omitempty"`
}

// CreateUserRequest is POST /v1/users.
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
	UserAdTag         *string `json:"user_ad_tag,omitempty"`
}

type PatchUserRequest struct {
	Secret            *string `json:"secret,omitempty"`
	MaxTCPConns       *uint64 `json:"max_tcp_conns,omitempty"`
	ExpirationRFC3339 *string `json:"expiration_rfc3339,omitempty"`
	DataQuotaBytes    *uint64 `json:"data_quota_bytes,omitempty"`
	RateLimitUpBps    *uint64 `json:"rate_limit_up_bps,omitempty"`
	RateLimitDownBps  *uint64 `json:"rate_limit_down_bps,omitempty"`
	MaxUniqueIPs      *uint64 `json:"max_unique_ips,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
	UserAdTag         *string `json:"user_ad_tag,omitempty"`

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
	TargetOS      string  `json:"target_os"`
	BuildProfile  string  `json:"build_profile"`
	GitCommit     string  `json:"git_commit"`
	BuildTimeUTC  string  `json:"build_time_utc"`
	ConfigPath    string  `json:"config_path"`
	ConfigHash    string  `json:"config_hash"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

// BuildID is the tightest identity of the running binary telemt will give us: the commit if
// the build carried one, otherwise the platform it was built for.
func (s SystemInfo) BuildID() string {
	if s.GitCommit != "" {
		return s.GitCommit
	}
	if s.TargetOS != "" && s.TargetArch != "" {
		return s.TargetOS + "/" + s.TargetArch
	}
	return s.TargetArch
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

// ReloadRequest is POST /v1/system/reload.
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

// ConnectionsSummary is GET /v1/runtime/connections/summary.
type ConnectionsSummary struct {
	Enabled bool                       `json:"enabled"`
	Reason  string                     `json:"reason,omitempty"`
	Data    *ConnectionsSummaryPayload `json:"data"`
}

// UpstreamDc is one Telegram datacenter in an upstream's health view.
type UpstreamDc struct {
	DC           int      `json:"dc"`
	LatencyEmaMs *float64 `json:"latency_ema_ms"`
	IPPreference string   `json:"ip_preference"`
}

// Upstream is one route to Telegram as telemt's upstream health check sees it.
type Upstream struct {
	RouteKind          string       `json:"route_kind"`
	Healthy            bool         `json:"healthy"`
	Fails              int          `json:"fails"`
	LastCheckAgeSecs   int64        `json:"last_check_age_secs"`
	EffectiveLatencyMs float64      `json:"effective_latency_ms"`
	DC                 []UpstreamDc `json:"dc"`
}

// UpstreamsStats is GET /v1/stats/upstreams, trimmed to what the panel shows.
type UpstreamsStats struct {
	Enabled bool `json:"enabled"`
	Zero    struct {
		ConnectSuccessTotal int64 `json:"connect_success_total"`
		ConnectFailTotal    int64 `json:"connect_fail_total"`
	} `json:"zero"`
	Upstreams []Upstream `json:"upstreams"`
}

// Error codes telemt returns for the WEB runtime endpoints.
const (
	ErrCodeRuntimeMismatch     = "web_runtime_mismatch"
	ErrCodeLifecycleInProgress = "web_lifecycle_in_progress"
)

// IsCode reports whether err is an APIError carrying code.
func IsCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// IsNotFound reports whether err is an APIError with HTTP 404, i.e. the endpoint is absent.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// Carriers telemt accepts for a WEB session.
const (
	CarrierHTTPS          = "https"
	CarrierHTTPSLanes     = "https-lanes"
	CarrierWebsocket      = "websocket"
	CarrierWebsocketLanes = "websocket-lanes"
)

// Carriers lists every carrier value telemt accepts.
var Carriers = []string{CarrierHTTPS, CarrierHTTPSLanes, CarrierWebsocket, CarrierWebsocketLanes}

// ValidCarrier reports whether name is a carrier telemt knows.
func ValidCarrier(name string) bool {
	for _, c := range Carriers {
		if c == name {
			return true
		}
	}
	return false
}

// WEB operator lifecycle states.
const (
	WebLifecycleRunning      = "running"
	WebLifecyclePaused       = "paused"
	WebLifecycleDraining     = "draining"
	WebLifecycleForceClosing = "force_closing"
	WebLifecycleDrained      = "drained"
)

// FlexID is an identifier telemt reports either as a JSON number or as a string.
type FlexID string

func (f *FlexID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = FlexID(v)
		return nil
	}
	*f = FlexID(s)
	return nil
}

func (f FlexID) String() string { return string(f) }

// WebStatus is GET /v1/runtime/web/status.
type WebStatus struct {
	RuntimeInstance   string        `json:"runtime_instance"`
	OperatorLifecycle *WebLifecycle `json:"operator_lifecycle"`
	Runtime           WebRuntime    `json:"runtime"`
	Capacity          *WebCapacity  `json:"capacity"`
	// CarrierNegotiation stays raw: its selection and failure matrices are telemt's own shape.
	CarrierNegotiation json.RawMessage `json:"carrier_negotiation"`
}

type WebCapacityResource struct {
	Resource  string `json:"resource"`
	Unit      string `json:"unit"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
	Limit     uint64 `json:"limit"`
	Closed    bool   `json:"closed"`
}

type WebCapacityCounter struct {
	Outcome string `json:"outcome"`
	Total   uint64 `json:"total"`
}

type WebCapacity struct {
	HTTPConnectionCapacityAction   string                `json:"http_connection_capacity_action"`
	MaxHTTPOverloadConnections     uint64                `json:"max_http_overload_connections"`
	HTTPOverloadTimeoutMs          uint64                `json:"http_overload_timeout_ms"`
	Resources                      []WebCapacityResource `json:"resources"`
	SaturatedResources             []string              `json:"saturated_resources"`
	Partial                        []string              `json:"partial"`
	HTTPConnectionOverloadOutcomes []WebCapacityCounter  `json:"http_connection_overload_outcomes"`
}

// HasCarrierNegotiation reports whether the payload carried a carrier negotiation section.
func (s WebStatus) HasCarrierNegotiation() bool {
	return len(s.CarrierNegotiation) > 0 && string(s.CarrierNegotiation) != "null"
}

type WebRuntime struct {
	RuntimeInstance string       `json:"runtime_instance"`
	Learning        *WebLearning `json:"learning"`
}

type WebLearning struct {
	Enabled          bool   `json:"enabled"`
	PolicyGeneration uint64 `json:"policy_generation"`
	Epoch            uint64 `json:"epoch"`
	Entries          int    `json:"entries"`
	Capacity         int    `json:"capacity"`
	LifetimeSecs     int64  `json:"lifetime_secs"`
	HealthSecs       int64  `json:"health_secs"`
	AgeMs            int64  `json:"age_ms"`
}

type WebLifecycle struct {
	State                     string           `json:"state"`
	Epoch                     uint64           `json:"epoch"`
	AgeMs                     int64            `json:"age_ms"`
	AdmissionOpen             bool             `json:"admission_open"`
	EffectiveNewWorkAdmission bool             `json:"effective_new_work_admission"`
	Drain                     *WebDrainProcess `json:"drain"`
}

// WebDrainProcess is operator_lifecycle.drain, the progress of a running or finished drain.
type WebDrainProcess struct {
	OperationID          FlexID `json:"operation_id"`
	State                string `json:"state"`
	Outcome              string `json:"outcome,omitempty"`
	TimeoutSecs          int    `json:"timeout_secs"`
	StartedEpochMillis   int64  `json:"started_epoch_millis"`
	DeadlineEpochMillis  int64  `json:"deadline_epoch_millis"`
	CompletedEpochMillis *int64 `json:"completed_epoch_millis"`
	RemainingSessions    uint64 `json:"remaining_sessions"`
	RemainingStreams     uint64 `json:"remaining_streams"`
	RemainingWebsockets  uint64 `json:"remaining_websockets"`
	ForceCloseSignalled  bool   `json:"force_close_signalled"`
}

// Done reports whether the drain operation reached a terminal state.
func (d WebDrainProcess) Done() bool {
	return d.State == "completed" || d.State == "cancelled"
}

// WebDrainAccepted is the 202 body of POST /v1/runtime/web/lifecycle/drain.
type WebDrainAccepted struct {
	OperationID         FlexID `json:"operation_id"`
	State               string `json:"state"`
	TimeoutSecs         int    `json:"timeout_secs"`
	StartedEpochMillis  int64  `json:"started_epoch_millis"`
	DeadlineEpochMillis int64  `json:"deadline_epoch_millis"`
}

// CarrierLearningReset is POST /v1/runtime/web/carrier-learning/reset.
type CarrierLearningReset struct {
	EntriesCleared int    `json:"entries_cleared"`
	Epoch          uint64 `json:"epoch"`
}

// WebSession is one row of GET /v1/runtime/web/sessions; Raw keeps the untyped remainder.
type WebSession struct {
	Carrier string
	State   string
	Raw     json.RawMessage
}

func (s *WebSession) UnmarshalJSON(b []byte) error {
	var head struct {
		Carrier string `json:"carrier"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	s.Carrier, s.State = head.Carrier, head.State
	s.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// WebSessionsPage is one page of GET /v1/runtime/web/sessions.
type WebSessionsPage struct {
	Sessions        []WebSession `json:"sessions"`
	NextCursor      string       `json:"next_cursor"`
	Scanned         int          `json:"scanned"`
	ScanTruncated   bool         `json:"scan_truncated"`
	PartialSessions int          `json:"partial_sessions"`
	Partial         []WebSession `json:"partial"`
}

// WebSessionsQuery filters GET /v1/runtime/web/sessions.
type WebSessionsQuery struct {
	Carrier string
	State   string
	Limit   int
	Cursor  string
}

type webInstanceRequest struct {
	RuntimeInstance string `json:"runtime_instance"`
}

type webDrainRequest struct {
	RuntimeInstance string `json:"runtime_instance"`
	TimeoutSecs     int    `json:"timeout_secs"`
}
