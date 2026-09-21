package agent

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

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

// endlessExec stands in for `journalctl -f`: it keeps producing lines until it is closed, and
// records that closing, so a test can tell whether the subscription actually ended.
type endlessExec struct {
	Exec
	closed chan struct{}
	once   sync.Once
}

func (e *endlessExec) Start(context.Context, string, ...string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		for {
			if _, err := pw.Write([]byte("2026-09-03T10:00:00+0000 host telemt[1]: line\n")); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	return &closeRecorder{ReadCloser: pr, on: func() { e.once.Do(func() { close(e.closed) }) }}, nil
}

type closeRecorder struct {
	io.ReadCloser
	on func()
}

func (c *closeRecorder) Close() error { c.on(); return c.ReadCloser.Close() }

// A page that is closed must take its journalctl with it. Without this the node keeps one
// follower per visit, writing into a stream nobody reads.
func TestTailLogsStopsWhenItsSubscriptionIsCancelled(t *testing.T) {
	h, _ := testHandler(t, &fakeExec{}, true)
	ex := &endlessExec{Exec: h.exec, closed: make(chan struct{})}
	h.exec = ex

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.TailLogs(ctx, &agentv1.TailLogsRequest{Services: []string{"telemt"}, Lines: 10, Follow: true},
			func(*agentv1.LogChunk) error { return nil })
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TailLogs kept running after its subscription was cancelled")
	}
	select {
	case <-ex.closed:
	case <-time.After(time.Second):
		t.Fatal("the journalctl reader was never closed")
	}
}

// When the panel's side of the stream is gone, sending fails. Reading on would keep the follower
// alive for a reader that no longer exists.
func TestTailLogsStopsWhenTheStreamIsGone(t *testing.T) {
	h, _ := testHandler(t, &fakeExec{}, true)
	ex := &endlessExec{Exec: h.exec, closed: make(chan struct{})}
	h.exec = ex

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.TailLogs(context.Background(), &agentv1.TailLogsRequest{Services: []string{"telemt"}, Lines: 10, Follow: true},
			func(*agentv1.LogChunk) error { return errors.New("stream closed") })
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TailLogs kept reading after the stream stopped accepting chunks")
	}
}

func TestTailRegistryEndsOneSubscriptionAndCapsTheRest(t *testing.T) {
	reg := &tailRegistry{cancels: map[string]context.CancelFunc{}}

	ctxA, releaseA, ok := reg.start(context.Background(), "a")
	if !ok {
		t.Fatal("the first subscription must be accepted")
	}
	ctxB, _, ok := reg.start(context.Background(), "b")
	if !ok {
		t.Fatal("a second subscription must be accepted")
	}

	// Cancelling one leaves the other running: a closed page must not take down a colleague's.
	reg.cancel("a")
	if ctxA.Err() == nil {
		t.Fatal("the cancelled subscription is still live")
	}
	if ctxB.Err() != nil {
		t.Fatal("cancelling one subscription ended another")
	}
	releaseA() // releasing after an explicit cancel must not panic or resurrect anything

	// "b" still holds one slot, so exactly cap-1 more may be accepted before the node refuses.
	accepted := 1
	for i := range maxTailSubscriptions + 3 {
		if _, _, ok := reg.start(context.Background(), "extra"+strconv.Itoa(i)); !ok {
			break
		}
		accepted++
	}
	if accepted != maxTailSubscriptions {
		t.Fatalf("accepted %d followers, want the cap of %d", accepted, maxTailSubscriptions)
	}
}

func TestTailRegistryCancelAllEndsEverySubscription(t *testing.T) {
	reg := &tailRegistry{cancels: map[string]context.CancelFunc{}}
	ctxA, _, _ := reg.start(context.Background(), "a")
	ctxB, _, _ := reg.start(context.Background(), "b")

	reg.cancelAll() // the session dropped; nothing it started may outlive it

	if ctxA.Err() == nil || ctxB.Err() == nil {
		t.Fatal("a dropped session left a journalctl follower behind")
	}
}
