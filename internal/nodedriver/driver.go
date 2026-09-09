// Package nodedriver abstracts how the panel talks to a node.
package nodedriver

import (
	"context"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/gateway"
)

var ErrOffline = gateway.ErrOffline

type HealthReport struct {
	RelayActive, MTProxyActive, CaddyActive, Healthz, Readyz bool
	TProxyVersion, AgentVersion                              string
	UptimeSeconds                                            int64
	CPUPercent, MemUsedPercent, DiskUsedPercent              float64
	ProfileCount                                             int

	DCs                      []DcLatency
	UpstreamHealthy          bool
	UpstreamFails            int
	EffectiveLatencyMs       float64
	ConnectSuccessTotal      int64
	ConnectFailTotal         int64
	UpstreamLastCheckAgeSecs int64
	DcDataAvailable          bool
}

// DcLatency is one Telegram datacenter's latency EMA as telemt measures it.
type DcLatency struct {
	DC           int
	LatencyMs    float64
	Known        bool
	IPPreference string
}

type Profile struct {
	Name, Secret, Backend string
	CarrierMode           string
	Limits                *domain.ProfileLimits

	Telemt    *domain.TelemtLimits
	ExpiresAt *time.Time
	Enabled   bool
}

type SiteBundle struct{ Files map[string][]byte }

type ApplyRequest struct {
	ApplyProfiles  bool
	Profiles       []Profile
	MTProxySecrets []string
	Site           *SiteBundle

	TLSDomain   string
	ClassicPort uint32
	PublicIP    string
	// AdTag is the sponsor-channel tag; empty means no sponsor channel, not "no opinion".
	AdTag string
	// WebPolicy is telemt's carrier policy; nil means the panel has no opinion and the
	// node's WEB config is left alone.
	WebPolicy *domain.WebPolicy
}

type ApplyResult struct {
	OK, RestartedRelay, RestartedMTProxy, RolledBack bool
	Log                                              string
	// DeferredFields are config keys the node persisted without activating, and
	// RestartRequired says it wants a process restart before they take effect.
	DeferredFields  []string
	RestartRequired bool
}

type LogLine struct {
	Service, Line string
	Time          time.Time
}

type Driver interface {
	Online(nodeID uuid.UUID) bool
	Health(ctx context.Context, nodeID uuid.UUID) (HealthReport, error)
	GetProfiles(ctx context.Context, nodeID uuid.UUID) ([]Profile, error)
	Apply(ctx context.Context, nodeID uuid.UUID, req ApplyRequest) (ApplyResult, error)
	GetSite(ctx context.Context, nodeID uuid.UUID) (SiteBundle, error)
	Metrics(ctx context.Context, nodeID uuid.UUID) (string, error)
	Stats(ctx context.Context, nodeID uuid.UUID) (map[string]string, error)
	TailLogs(ctx context.Context, nodeID uuid.UUID, services []string, lines int, follow bool) (<-chan LogLine, error)
	RestartRelay(ctx context.Context, nodeID uuid.UUID) error
}
