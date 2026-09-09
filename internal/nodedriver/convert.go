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
	out := HealthReport{
		RelayActive: h.GetRelayActive(), MTProxyActive: h.GetMtproxyActive(), CaddyActive: h.GetCaddyActive(),
		Healthz: h.GetHealthz(), Readyz: h.GetReadyz(), TProxyVersion: h.GetTproxyVersion(), AgentVersion: h.GetAgentVersion(),
		UptimeSeconds: h.GetUptimeSeconds(), CPUPercent: h.GetCpuPercent(), MemUsedPercent: h.GetMemUsedPercent(),
		DiskUsedPercent: h.GetDiskUsedPercent(), ProfileCount: int(h.GetProfileCount()),
		UpstreamHealthy: h.GetUpstreamHealthy(), UpstreamFails: int(h.GetUpstreamFails()),
		EffectiveLatencyMs: h.GetEffectiveLatencyMs(), ConnectSuccessTotal: h.GetConnectSuccessTotal(),
		ConnectFailTotal: h.GetConnectFailTotal(), UpstreamLastCheckAgeSecs: h.GetUpstreamLastCheckAgeSecs(),
		DcDataAvailable: h.GetDcDataAvailable(),
		Web:             WebTelemetryFromProto(h.GetWeb()),
		Capabilities:    CapabilitiesFromProto(h.GetCapabilities()),
		TelemtBuild:     h.GetCapabilities().GetBuild(),
	}
	for _, d := range h.GetDcs() {
		out.DCs = append(out.DCs, DcLatency{DC: int(d.GetDc()), LatencyMs: d.GetLatencyMs(), Known: d.GetKnown(), IPPreference: d.GetIpPreference()})
	}
	return out
}

func HealthToProto(h HealthReport) *agentv1.HealthReport {
	out := &agentv1.HealthReport{
		RelayActive: h.RelayActive, MtproxyActive: h.MTProxyActive, CaddyActive: h.CaddyActive,
		Healthz: h.Healthz, Readyz: h.Readyz, TproxyVersion: h.TProxyVersion, AgentVersion: h.AgentVersion,
		UptimeSeconds: h.UptimeSeconds, CpuPercent: h.CPUPercent, MemUsedPercent: h.MemUsedPercent,
		DiskUsedPercent: h.DiskUsedPercent, ProfileCount: int32(h.ProfileCount),
		UpstreamHealthy: h.UpstreamHealthy, UpstreamFails: int32(h.UpstreamFails),
		EffectiveLatencyMs: h.EffectiveLatencyMs, ConnectSuccessTotal: h.ConnectSuccessTotal,
		ConnectFailTotal: h.ConnectFailTotal, UpstreamLastCheckAgeSecs: h.UpstreamLastCheckAgeSecs,
		DcDataAvailable: h.DcDataAvailable,
		Web:             WebTelemetryToProto(h.Web),
		Capabilities:    CapabilitiesToProto(h.Capabilities, h.TelemtBuild),
	}
	for _, d := range h.DCs {
		out.Dcs = append(out.Dcs, &agentv1.DcLatency{Dc: int32(d.DC), LatencyMs: d.LatencyMs, Known: d.Known, IpPreference: d.IPPreference})
	}
	return out
}

func logLineFromProto(l *agentv1.LogLine) LogLine {
	return LogLine{Service: l.GetService(), Line: l.GetLine(), Time: time.UnixMilli(l.GetUnixMs())}
}

// WebPolicyToProto carries the panel's carrier policy to the agent. A nil policy stays nil:
// an agent that receives no policy leaves the node's WEB config alone.
func WebPolicyToProto(p *domain.WebPolicy) *agentv1.WebPolicy {
	if p == nil {
		return nil
	}
	out := &agentv1.WebPolicy{
		Carrier:         string(p.Carrier),
		CarrierLearning: p.CarrierLearning,
		Aggressiveness:  string(p.Aggressiveness),
		Timeouts: &agentv1.WebTimeouts{
			CarrierHealthSecs:   int32(p.Timeouts.CarrierHealthSecs),
			CarrierLearningSecs: int32(p.Timeouts.CarrierLearningSecs),
			BridgeRequestSecs:   int32(p.Timeouts.BridgeRequestSecs),
			BridgeRetrySecs:     int32(p.Timeouts.BridgeRetrySecs),
			ProbeCoalesceMs:     int32(p.Timeouts.ProbeCoalesceMs),
		},
	}
	for _, c := range p.Carriers {
		out.Carriers = append(out.Carriers, string(c))
	}
	for _, d := range p.Timeouts.NegotiationDeadlinesSecs {
		out.Timeouts.NegotiationDeadlinesSecs = append(out.Timeouts.NegotiationDeadlinesSecs, int32(d))
	}
	return out
}

// WebPolicyFromProto is the agent-side inverse of WebPolicyToProto.
func WebPolicyFromProto(p *agentv1.WebPolicy) *domain.WebPolicy {
	if p == nil {
		return nil
	}
	t := p.GetTimeouts()
	out := &domain.WebPolicy{
		Preset:          domain.PresetCustom,
		Carrier:         domain.Carrier(p.GetCarrier()),
		CarrierLearning: p.GetCarrierLearning(),
		Aggressiveness:  domain.Aggressiveness(p.GetAggressiveness()),
		Timeouts: domain.WebTimeouts{
			CarrierHealthSecs:   int(t.GetCarrierHealthSecs()),
			CarrierLearningSecs: int(t.GetCarrierLearningSecs()),
			BridgeRequestSecs:   int(t.GetBridgeRequestSecs()),
			BridgeRetrySecs:     int(t.GetBridgeRetrySecs()),
			ProbeCoalesceMs:     int(t.GetProbeCoalesceMs()),
		},
	}
	for _, c := range p.GetCarriers() {
		out.Carriers = append(out.Carriers, domain.Carrier(c))
	}
	for _, d := range t.GetNegotiationDeadlinesSecs() {
		out.Timeouts.NegotiationDeadlinesSecs = append(out.Timeouts.NegotiationDeadlinesSecs, int(d))
	}
	return out
}
