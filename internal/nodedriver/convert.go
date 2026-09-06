package nodedriver

import (
	"time"

	"tgwebproxy/internal/domain"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func ProfileToProto(p Profile) *agentv1.Profile {
	out := &agentv1.Profile{Name: p.Name, Secret: p.Secret, Backend: p.Backend, CarrierMode: p.CarrierMode, Enabled: p.Enabled}
	if t := p.Telemt; t != nil {
		out.DataQuotaBytes, out.RateLimitUpBps, out.RateLimitDownBps = uint64(t.DataQuotaBytes), uint64(t.RateLimitUpBps), uint64(t.RateLimitDownBps)
		out.MaxUniqueIps, out.MaxTcpConns = uint32(t.MaxUniqueIPs), uint32(t.MaxTCPConns)
	}
	if p.ExpiresAt != nil {
		out.ExpiresAtUnix = p.ExpiresAt.Unix()
	}
	if p.Limits != nil {
		l := p.Limits
		out.Limits = &agentv1.ProfileLimits{
			MaxSessions: int32(l.MaxSessions), MaxStreams: int32(l.MaxStreams),
			MaxBackendDialsInFlight: int32(l.MaxBackendDialsInFlight),
			NewSessionsPerMinute:    int32(l.NewSessionsPerMinute), NewSessionsBurst: int32(l.NewSessionsBurst),
			NewStreamsPerMinute: int32(l.NewStreamsPerMinute), NewStreamsBurst: int32(l.NewStreamsBurst),
			MaxStreamsPerSession: int32(l.MaxStreamsPerSession), MaxPendingPerSession: int32(l.MaxPendingPerSession),
		}
	}
	return out
}

func ProfileFromProto(p *agentv1.Profile) Profile {
	out := Profile{Name: p.GetName(), Secret: p.GetSecret(), Backend: p.GetBackend(), CarrierMode: p.GetCarrierMode(), Enabled: p.GetEnabled()}
	// An all-zero telemt block stays nil: proto3 cannot tell "no limits" from "limits, all
	// unset", and the two mean the same thing to telemt, so the smaller representation wins.
	tl := domain.TelemtLimits{
		DataQuotaBytes: int64(p.GetDataQuotaBytes()), RateLimitUpBps: int64(p.GetRateLimitUpBps()),
		RateLimitDownBps: int64(p.GetRateLimitDownBps()), MaxUniqueIPs: int(p.GetMaxUniqueIps()),
		MaxTCPConns: int(p.GetMaxTcpConns()),
	}
	if tl != (domain.TelemtLimits{}) {
		out.Telemt = &tl
	}
	if v := p.GetExpiresAtUnix(); v != 0 {
		t := time.Unix(v, 0).UTC()
		out.ExpiresAt = &t
	}
	if l := p.GetLimits(); l != nil {
		out.Limits = &domain.ProfileLimits{
			MaxSessions: int(l.MaxSessions), MaxStreams: int(l.MaxStreams),
			MaxBackendDialsInFlight: int(l.MaxBackendDialsInFlight),
			NewSessionsPerMinute:    int(l.NewSessionsPerMinute), NewSessionsBurst: int(l.NewSessionsBurst),
			NewStreamsPerMinute: int(l.NewStreamsPerMinute), NewStreamsBurst: int(l.NewStreamsBurst),
			MaxStreamsPerSession: int(l.MaxStreamsPerSession), MaxPendingPerSession: int(l.MaxPendingPerSession),
		}
	}
	return out
}

func SiteToProto(s SiteBundle) *agentv1.SiteBundle {
	out := &agentv1.SiteBundle{}
	for p, c := range s.Files {
		out.Files = append(out.Files, &agentv1.SiteFile{Path: p, Content: c})
	}
	return out
}

func SiteFromProto(s *agentv1.SiteBundle) SiteBundle {
	out := SiteBundle{Files: map[string][]byte{}}
	for _, f := range s.GetFiles() {
		out.Files[f.Path] = f.Content
	}
	return out
}

func HealthFromProto(h *agentv1.HealthReport) HealthReport {
	return HealthReport{
		RelayActive: h.GetRelayActive(), MTProxyActive: h.GetMtproxyActive(), CaddyActive: h.GetCaddyActive(),
		Healthz: h.GetHealthz(), Readyz: h.GetReadyz(), TProxyVersion: h.GetTproxyVersion(), AgentVersion: h.GetAgentVersion(),
		UptimeSeconds: h.GetUptimeSeconds(), CPUPercent: h.GetCpuPercent(), MemUsedPercent: h.GetMemUsedPercent(),
		DiskUsedPercent: h.GetDiskUsedPercent(), ProfileCount: int(h.GetProfileCount()),
	}
}

func HealthToProto(h HealthReport) *agentv1.HealthReport {
	return &agentv1.HealthReport{
		RelayActive: h.RelayActive, MtproxyActive: h.MTProxyActive, CaddyActive: h.CaddyActive,
		Healthz: h.Healthz, Readyz: h.Readyz, TproxyVersion: h.TProxyVersion, AgentVersion: h.AgentVersion,
		UptimeSeconds: h.UptimeSeconds, CpuPercent: h.CPUPercent, MemUsedPercent: h.MemUsedPercent,
		DiskUsedPercent: h.DiskUsedPercent, ProfileCount: int32(h.ProfileCount),
	}
}

func logLineFromProto(l *agentv1.LogLine) LogLine {
	return LogLine{Service: l.GetService(), Line: l.GetLine(), Time: time.UnixMilli(l.GetUnixMs())}
}
