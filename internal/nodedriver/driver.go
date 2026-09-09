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

	// Connectivity to Telegram's datacenters, from telemt's upstream health check. Every field
	// is meaningless unless DcDataAvailable is true: a tproxy node never has the data, and a
	// telemt node loses it for one heartbeat when the stats call fails.
	DCs                      []DcLatency
	UpstreamHealthy          bool
	UpstreamFails            int
	EffectiveLatencyMs       float64
	ConnectSuccessTotal      int64
	ConnectFailTotal         int64
	UpstreamLastCheckAgeSecs int64
	DcDataAvailable          bool
}

// DcLatency is one Telegram datacenter's latency EMA as telemt measures it. Known is false
// while telemt has no measurement for the DC yet; LatencyMs is then zero and must not be read.
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

	// Telemt carries the telemt engine's per-user limits; nil means the panel has no opinion
	// and telemt's own defaults apply. ExpiresAt is the key's expiry (nil = never). Enabled is
	// literal: the panel sets it true for every profile it wants served, so false means the
	// user must be disabled on the node rather than "unset". The tproxy agent ignores all
	// three.
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

	// TLSDomain and ClassicPort are the telemt node's Fake-TLS listener as the panel wants
	// it. The telemt agent rewrites the node's config when either differs from what telemt
	// currently holds, which costs a telemt restart; the tproxy agent ignores both. An empty
	// domain and a zero port mean "no opinion", which is what a tproxy node always sends.
	TLSDomain   string
	ClassicPort uint32
	// PublicIP is the node's public IPv4 as the panel holds it (the address the A record
	// points at). The telemt agent rewrites the WEB vhost's public_addr when it differs,
	// which costs a telemt restart; the tproxy agent ignores it. Empty means "no opinion".
	PublicIP string
	// AdTag is the sponsor-channel tag telemt's middle-proxy mode advertises to Telegram
	// (registered per server with @MTProxybot). The telemt agent applies it to every profile
	// and turns `[general] use_middle_proxy` on when set, off when empty; the tproxy agent
	// ignores it. Unlike TLSDomain/ClassicPort/PublicIP, empty here is authoritative ("no
	// sponsor channel"), not "no opinion" - that is also what an old panel always sends.
	AdTag string
}

type ApplyResult struct {
	OK, RestartedRelay, RestartedMTProxy, RolledBack bool
	Log                                              string
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
