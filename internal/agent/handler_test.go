package agent

import (
	"context"
	"errors"
	"io"
	"testing"

	agentv1 "tgwebproxy/proto/agent/v1"
)

func TestHealthReport(t *testing.T) {
	ex := &fakeExec{active: map[string]bool{"tproxy-server": true, "mtproxy": false, "caddy": true}}
	h, _ := testHandler(t, ex, true)
	rep := h.Health(context.Background())
	if !rep.RelayActive || rep.MtproxyActive || !rep.CaddyActive || !rep.Healthz || !rep.Readyz || rep.ProfileCount != 1 || rep.TproxyVersion != "abc" {
		t.Fatalf("bad report %+v", rep)
	}
	// tproxy has no view of Telegram's datacenters: the DC fields stay zero and are marked so.
	if rep.DcDataAvailable || len(rep.Dcs) != 0 || rep.UpstreamHealthy || rep.EffectiveLatencyMs != 0 || rep.ConnectSuccessTotal != 0 {
		t.Fatalf("tproxy report must carry no DC data: %+v", rep)
	}
}

func TestHandleGetProfilesAndMetricsAndStats(t *testing.T) {
	h, _ := testHandler(t, &fakeExec{}, true)
	ctx := context.Background()
	resp := h.Handle(ctx, &agentv1.Request{Body: &agentv1.Request_GetProfiles{GetProfiles: &agentv1.GetProfilesRequest{}}})
	if resp.Error != "" || len(resp.GetProfiles().Profiles) != 1 || resp.GetProfiles().Profiles[0].Name != "default" {
		t.Fatalf("profiles: %+v", resp)
	}
	resp = h.Handle(ctx, &agentv1.Request{Body: &agentv1.Request_Metrics{Metrics: &agentv1.MetricsRequest{}}})
	if resp.GetMetrics().GetText() != "tproxy_sessions_live 3\n" {
		t.Fatalf("metrics: %+v", resp)
	}
	resp = h.Handle(ctx, &agentv1.Request{Body: &agentv1.Request_Stats{Stats: &agentv1.StatsRequest{}}})
	if resp.GetStats().GetValues()["active_connections"] != "3" {
		t.Fatalf("stats: %+v", resp)
	}
	resp = h.Handle(ctx, &agentv1.Request{Body: &agentv1.Request_GetSite{GetSite: &agentv1.GetSiteRequest{}}})
	if len(resp.GetSite().Files) != 1 || resp.GetSite().Files[0].Path != "index.html" {
		t.Fatalf("site: %+v", resp)
	}
}

func TestTailLogs(t *testing.T) {
	h, _ := testHandler(t, &fakeExec{}, true)
	var got []*agentv1.LogChunk
	h.TailLogs(context.Background(), &agentv1.TailLogsRequest{Services: []string{"tproxy-server"}, Lines: 10}, func(c *agentv1.LogChunk) error {
		got = append(got, c)
		return nil
	})
	if len(got) < 2 || !got[len(got)-1].Done || got[0].Lines[0].Service != "tproxy-server" {
		t.Fatalf("chunks: %+v", got)
	}
}

// failingReader yields one usable line and then fails, the way journalctl does when it is
// killed mid-stream.
type failingReader struct{ done bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("journalctl died")
	}
	r.done = true
	return copy(p, "host telemt[1]: started\n"), nil
}

func (r *failingReader) Close() error { return nil }

type failingStreamExec struct{ Exec }

func (f *failingStreamExec) Start(context.Context, string, ...string) (io.ReadCloser, error) {
	return &failingReader{}, nil
}

// A stream that died must not reach the operator as a tidy end of the log.
func TestTailLogsReportsAStreamThatDied(t *testing.T) {
	h, _ := testHandler(t, &fakeExec{}, true)
	h.exec = &failingStreamExec{Exec: h.exec}
	var got []*agentv1.LogChunk
	h.TailLogs(context.Background(), &agentv1.TailLogsRequest{Services: []string{"telemt"}, Lines: 10}, func(c *agentv1.LogChunk) error {
		got = append(got, c)
		return nil
	})
	last := got[len(got)-1]
	if !last.Done {
		t.Fatalf("the stream must still end with Done: %+v", got)
	}
	if last.Error == "" {
		t.Fatal("a died stream must say so instead of looking like the end of the log")
	}
}
