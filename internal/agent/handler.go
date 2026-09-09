package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"tgwebproxy/internal/telemt"
	agentv1 "tgwebproxy/proto/agent/v1"
)

type Handler struct {
	cfg   Config
	exec  Exec
	httpc *http.Client
	log   *slog.Logger
	// tm is the telemt control API client; nil unless the node runs the telemt engine.
	tm *telemt.Client
}

func NewHandler(cfg Config, ex Exec, log *slog.Logger) *Handler {
	h := &Handler{cfg: cfg, exec: ex, httpc: &http.Client{Timeout: 5 * time.Second}, log: log}
	if cfg.Engine == EngineTelemt {
		h.tm = telemt.New(cfg.TelemtAPI, cfg.TelemtAPIToken)
		if cfg.TelemtMetricsURL != "" {
			h.tm.MetricsURL = cfg.TelemtMetricsURL
		}
	}
	return h
}

func (h *Handler) siteDir() string {
	if h.cfg.Engine == EngineTelemt {
		return h.cfg.TelemtSiteDir
	}
	return h.cfg.SiteDir
}

func errResp(err error) *agentv1.Response { return &agentv1.Response{Error: err.Error()} }

func errorf(format string, a ...any) error { return fmt.Errorf(format, a...) }

func (h *Handler) Handle(ctx context.Context, req *agentv1.Request) *agentv1.Response {
	switch b := req.Body.(type) {
	case *agentv1.Request_Health:
		return &agentv1.Response{Body: &agentv1.Response_Health{Health: h.Health(ctx)}}
	case *agentv1.Request_GetProfiles:
		read := h.readProfiles
		if h.cfg.Engine == EngineTelemt {
			read = func() ([]*agentv1.Profile, error) { return h.telemtProfiles(ctx) }
		}
		ps, err := read()
		if err != nil {
			return errResp(err)
		}
		return &agentv1.Response{Body: &agentv1.Response_Profiles{Profiles: &agentv1.ProfilesFile{Profiles: ps}}}
	case *agentv1.Request_Apply:
		res := h.Apply(ctx, b.Apply)
		resp := &agentv1.Response{Body: &agentv1.Response_Apply{Apply: res}}
		if !res.Ok {
			resp.Error = "apply failed"
		}
		return resp
	case *agentv1.Request_GetSite:
		bundle := &agentv1.SiteBundle{}
		for p, c := range h.readSite() {
			bundle.Files = append(bundle.Files, &agentv1.SiteFile{Path: p, Content: c})
		}
		return &agentv1.Response{Body: &agentv1.Response_Site{Site: bundle}}
	case *agentv1.Request_Metrics:
		text, err := h.metricsText(ctx)
		if err != nil {
			return errResp(err)
		}
		return &agentv1.Response{Body: &agentv1.Response_Metrics{Metrics: &agentv1.MetricsText{Text: text}}}
	case *agentv1.Request_Stats:
		if h.cfg.Engine == EngineTelemt {
			values, err := h.telemtStats(ctx)
			if err != nil {
				return errResp(err)
			}
			return &agentv1.Response{Body: &agentv1.Response_Stats{Stats: &agentv1.StatsMap{Values: values}}}
		}
		text, err := h.get(ctx, h.cfg.MTProxyStatsURL)
		if err != nil {
			return errResp(err)
		}
		return &agentv1.Response{Body: &agentv1.Response_Stats{Stats: &agentv1.StatsMap{Values: parseStats(text)}}}
	case *agentv1.Request_RestartRelay:
		return h.restartRelay(ctx)
	}
	return errResp(errorf("unsupported request"))
}

// metricsText fetches the engine's Prometheus exposition text.
func (h *Handler) metricsText(ctx context.Context) (string, error) {
	if h.cfg.Engine == EngineTelemt {
		if h.tm == nil {
			return "", errorf("telemt engine is not configured")
		}
		return h.tm.Metrics(ctx)
	}
	return h.get(ctx, h.cfg.RelayAdminURL+"/metrics")
}

// restartRelay restarts the engine's proxy unit.
func (h *Handler) restartRelay(ctx context.Context) *agentv1.Response {
	unit, wait := "tproxy-server", h.waitHealthy
	if h.cfg.Engine == EngineTelemt {
		unit, wait = "telemt", h.waitTelemtReady
	}
	if out, err := h.exec.Run(ctx, "systemctl", "restart", unit); err != nil {
		return errResp(errorf("restart: %s", strings.TrimSpace(string(out))))
	}
	if err := wait(ctx); err != nil {
		return errResp(err)
	}
	return &agentv1.Response{Body: &agentv1.Response_Empty{Empty: &agentv1.Empty{}}}
}

func (h *Handler) get(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := h.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

func (h *Handler) readProfiles() ([]*agentv1.Profile, error) {
	raw, err := os.ReadFile(h.cfg.ProfilesPath)
	if err != nil {
		return nil, err
	}
	var f struct {
		Profiles []struct {
			Name        string                 `json:"name"`
			Secret      string                 `json:"secret"`
			Backend     string                 `json:"backend"`
			CarrierMode string                 `json:"carrier_mode"`
			Limits      *agentv1.ProfileLimits `json:"limits"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	out := make([]*agentv1.Profile, 0, len(f.Profiles))
	for _, p := range f.Profiles {
		out = append(out, &agentv1.Profile{Name: p.Name, Secret: p.Secret, Backend: p.Backend, CarrierMode: p.CarrierMode, Limits: p.Limits})
	}
	return out, nil
}

// parseStats turns MTProxy's "key<TAB>value" lines into a map.
func parseStats(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, "\t")
		if !ok {
			k, v, ok = strings.Cut(line, " ")
		}
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
