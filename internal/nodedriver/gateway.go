package nodedriver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/gateway"
	agentv1 "tgwebproxy/proto/agent/v1"
)

// Gateway implements Driver over live agent sessions.
type Gateway struct {
	reg     *gateway.Registry
	timeout time.Duration
}

func NewGateway(reg *gateway.Registry, timeout time.Duration) *Gateway {
	return &Gateway{reg: reg, timeout: timeout}
}

func (g *Gateway) call(ctx context.Context, id uuid.UUID, req *agentv1.Request, timeout time.Duration) (*agentv1.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return g.reg.Call(ctx, id, req)
}

func (g *Gateway) Online(id uuid.UUID) bool { return g.reg.Online(id) }

func (g *Gateway) Health(ctx context.Context, id uuid.UUID) (HealthReport, error) {
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Health{Health: &agentv1.HealthRequest{}}}, g.timeout)
	if err != nil {
		return HealthReport{}, err
	}
	return HealthFromProto(resp.GetHealth()), nil
}

func (g *Gateway) GetProfiles(ctx context.Context, id uuid.UUID) ([]Profile, error) {
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_GetProfiles{GetProfiles: &agentv1.GetProfilesRequest{}}}, g.timeout)
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, p := range resp.GetProfiles().GetProfiles() {
		out = append(out, ProfileFromProto(p))
	}
	return out, nil
}

func (g *Gateway) Apply(ctx context.Context, id uuid.UUID, req ApplyRequest) (ApplyResult, error) {
	pr := &agentv1.ApplyRequest{
		ApplyProfiles: req.ApplyProfiles, MtproxySecrets: req.MTProxySecrets,
		TlsDomain: req.TLSDomain, ClassicPort: req.ClassicPort, PublicIp: req.PublicIP,
	}
	for _, p := range req.Profiles {
		pr.Profiles = append(pr.Profiles, ProfileToProto(p))
	}
	if req.Site != nil {
		pr.Site = SiteToProto(*req.Site)
	}
	// apply restarts services and waits for health: allow longer than a plain call.
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Apply{Apply: pr}}, 90*time.Second)
	return applyResultFrom(resp, err)
}

// applyResultFrom maps an agent reply onto an ApplyResult. It never returns a
// nil error without an ApplyResponse: an agent that answers an Apply with some
// other body would otherwise look like a successful apply and let the worker
// mark profiles synced for state the node never received.
func applyResultFrom(resp *agentv1.Response, err error) (ApplyResult, error) {
	if a := resp.GetApply(); a != nil {
		res := ApplyResult{OK: a.Ok, RestartedRelay: a.RestartedRelay, RestartedMTProxy: a.RestartedMtproxy, RolledBack: a.RolledBack, Log: a.Log}
		switch {
		case err != nil:
			return res, err
		case !a.Ok:
			return res, errors.New("apply failed on node")
		default:
			return res, nil
		}
	}
	if err == nil {
		err = errors.New("empty apply response")
	}
	return ApplyResult{}, err
}

func (g *Gateway) GetSite(ctx context.Context, id uuid.UUID) (SiteBundle, error) {
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_GetSite{GetSite: &agentv1.GetSiteRequest{}}}, g.timeout)
	if err != nil {
		return SiteBundle{}, err
	}
	return SiteFromProto(resp.GetSite()), nil
}

func (g *Gateway) Metrics(ctx context.Context, id uuid.UUID) (string, error) {
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Metrics{Metrics: &agentv1.MetricsRequest{}}}, g.timeout)
	if err != nil {
		return "", err
	}
	return resp.GetMetrics().GetText(), nil
}

func (g *Gateway) Stats(ctx context.Context, id uuid.UUID) (map[string]string, error) {
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Stats{Stats: &agentv1.StatsRequest{}}}, g.timeout)
	if err != nil {
		return nil, err
	}
	return resp.GetStats().GetValues(), nil
}

func (g *Gateway) TailLogs(ctx context.Context, id uuid.UUID, services []string, lines int, follow bool) (<-chan LogLine, error) {
	ch, err := g.reg.Stream(ctx, id, &agentv1.Request{Body: &agentv1.Request_TailLogs{TailLogs: &agentv1.TailLogsRequest{Services: services, Lines: int32(lines), Follow: follow}}})
	if err != nil {
		return nil, err
	}
	out := make(chan LogLine, 64)
	go func() {
		defer close(out)
		for chunk := range ch {
			for _, l := range chunk.Lines {
				select {
				case out <- logLineFromProto(l):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (g *Gateway) RestartRelay(ctx context.Context, id uuid.UUID) error {
	_, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_RestartRelay{RestartRelay: &agentv1.RestartRelayRequest{}}}, 60*time.Second)
	return err
}
