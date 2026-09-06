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
