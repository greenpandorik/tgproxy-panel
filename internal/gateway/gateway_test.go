package gateway_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"tgwebproxy/internal/gateway"
	agentv1 "tgwebproxy/proto/agent/v1"
)

type fakeAuth struct{ id uuid.UUID }

func (f fakeAuth) NodeByToken(_ context.Context, tok string) (uuid.UUID, error) {
	if tok == "good" {
		return f.id, nil
	}
	return uuid.Nil, errors.New("bad token")
}

type fakeHooks struct {
	mu          sync.Mutex
	hello, beat int
	disconnects int
}

func (h *fakeHooks) OnHello(context.Context, uuid.UUID, *agentv1.Hello) {
	h.mu.Lock()
	h.hello++
	h.mu.Unlock()
}

func (h *fakeHooks) OnHeartbeat(context.Context, uuid.UUID, *agentv1.HealthReport) {
	h.mu.Lock()
	h.beat++
	h.mu.Unlock()
}

func (h *fakeHooks) OnDisconnect(context.Context, uuid.UUID) {
	h.mu.Lock()
	h.disconnects++
	h.mu.Unlock()
}

type env struct {
	reg    *gateway.Registry
	hooks  *fakeHooks
	nodeID uuid.UUID
	dial   func(ctx context.Context) (agentv1.AgentGatewayClient, func())
}

func setup(t *testing.T) *env {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	reg := gateway.NewRegistry()
	hooks := &fakeHooks{}
	id := uuid.New()
	srv := grpc.NewServer()
	agentv1.RegisterAgentGatewayServer(srv, gateway.NewServer(reg, fakeAuth{id}, hooks, slog.New(slog.DiscardHandler)))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	dial := func(ctx context.Context) (agentv1.AgentGatewayClient, func()) {
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		return agentv1.NewAgentGatewayClient(conn), func() { _ = conn.Close() }
	}
	return &env{reg: reg, hooks: hooks, nodeID: id, dial: dial}
}

// fakeAgent connects, sends Hello, and answers Health requests until ctx ends.
func fakeAgent(t *testing.T, e *env, token string) (agentv1.AgentGateway_SessionClient, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	client, closeFn := e.dial(ctx)
	stream, err := client.Session(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&agentv1.Envelope{Body: &agentv1.Envelope_Hello{Hello: &agentv1.Hello{AgentVersion: "t", Hostname: "n.test"}}})
	go func() {
		defer closeFn()
		for {
			env, err := stream.Recv()
			if err != nil {
				return
			}
			req := env.GetRequest()
			if req == nil {
				continue
			}
			switch req.Body.(type) {
			case *agentv1.Request_Health:
				_ = stream.Send(&agentv1.Envelope{RequestId: env.RequestId, Body: &agentv1.Envelope_Response{Response: &agentv1.Response{
					Body: &agentv1.Response_Health{Health: &agentv1.HealthReport{RelayActive: true}},
				}}})
			case *agentv1.Request_TailLogs:
				_ = stream.Send(&agentv1.Envelope{RequestId: env.RequestId, Body: &agentv1.Envelope_LogChunk{LogChunk: &agentv1.LogChunk{
					Lines: []*agentv1.LogLine{{Service: "tproxy-server", Line: "one"}},
				}}})
				_ = stream.Send(&agentv1.Envelope{RequestId: env.RequestId, Body: &agentv1.Envelope_LogChunk{LogChunk: &agentv1.LogChunk{Done: true}}})
			}
		}
	}()
	return stream, cancel
}

func waitOnline(t *testing.T, e *env) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if e.reg.Online(e.nodeID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("node never came online")
}

func TestCallRoundTrip(t *testing.T) {
	e := setup(t)
	_, cancel := fakeAgent(t, e, "good")
	defer cancel()
	waitOnline(t, e)
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	resp, err := e.reg.Call(ctx, e.nodeID, &agentv1.Request{Body: &agentv1.Request_Health{Health: &agentv1.HealthRequest{}}})
	if err != nil || !resp.GetHealth().GetRelayActive() {
		t.Fatalf("resp %v err %v", resp, err)
	}
	e.hooks.mu.Lock()
	defer e.hooks.mu.Unlock()
	if e.hooks.hello != 1 {
		t.Fatalf("hello hooks = %d", e.hooks.hello)
	}
}

func TestOfflineNode(t *testing.T) {
	e := setup(t)
	_, err := e.reg.Call(context.Background(), uuid.New(), &agentv1.Request{Body: &agentv1.Request_Health{Health: &agentv1.HealthRequest{}}})
	if !errors.Is(err, gateway.ErrOffline) {
		t.Fatalf("expected ErrOffline, got %v", err)
	}
}

func TestBadTokenRejected(t *testing.T) {
	e := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer bad")
	client, closeFn := e.dial(ctx)
	defer closeFn()
	stream, err := client.Session(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&agentv1.Envelope{Body: &agentv1.Envelope_Hello{Hello: &agentv1.Hello{AgentVersion: "t", Hostname: "n.test"}}})
	if _, err := stream.Recv(); err == nil {
		t.Fatal("expected stream error for bad token")
	}
	if e.reg.Online(e.nodeID) {
		t.Fatal("node must not be online")
	}
}

func TestStreamLogs(t *testing.T) {
	e := setup(t)
	_, cancel := fakeAgent(t, e, "good")
	defer cancel()
	waitOnline(t, e)
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	ch, err := e.reg.Stream(ctx, e.nodeID, &agentv1.Request{Body: &agentv1.Request_TailLogs{TailLogs: &agentv1.TailLogsRequest{Lines: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	var lines int
	for chunk := range ch {
		lines += len(chunk.Lines)
	}
	if lines != 1 {
		t.Fatalf("lines = %d", lines)
	}
}

func TestDisconnectMarksOffline(t *testing.T) {
	e := setup(t)
	_, cancel := fakeAgent(t, e, "good")
	waitOnline(t, e)
	cancel()
	for i := 0; i < 50 && e.reg.Online(e.nodeID); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if e.reg.Online(e.nodeID) {
		t.Fatal("still online after disconnect")
	}
	e.hooks.mu.Lock()
	defer e.hooks.mu.Unlock()
	if e.hooks.disconnects != 1 {
		t.Fatalf("disconnect hooks = %d", e.hooks.disconnects)
	}
}

func floodAgent(t *testing.T, e *env, chunks int) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer good")
	client, closeFn := e.dial(ctx)
	stream, err := client.Session(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&agentv1.Envelope{Body: &agentv1.Envelope_Hello{Hello: &agentv1.Hello{AgentVersion: "t", Hostname: "n.test"}}})
	go func() {
		defer closeFn()
		for {
			in, err := stream.Recv()
			if err != nil {
				return
			}
			if in.GetRequest().GetTailLogs() == nil {
				continue
			}
			for i := 0; i < chunks; i++ {
				if err := stream.Send(&agentv1.Envelope{RequestId: in.RequestId, Body: &agentv1.Envelope_LogChunk{LogChunk: &agentv1.LogChunk{
					Lines: []*agentv1.LogLine{{Service: "tproxy-server", Line: "flood"}},
				}}}); err != nil {
					return
				}
			}
			_ = stream.Send(&agentv1.Envelope{Body: &agentv1.Envelope_Heartbeat{Heartbeat: &agentv1.Heartbeat{
				Health: &agentv1.HealthReport{RelayActive: true},
			}}})
		}
	}()
	return cancel
}

func TestLogFloodDoesNotDelayHeartbeat(t *testing.T) {
	e := setup(t)
	cancel := floodAgent(t, e, 200)
	defer cancel()
	waitOnline(t, e)

	ctx, c := context.WithCancel(context.Background())
	defer c()
	// The returned channel is deliberately never read: this is the slow-SSE-consumer case.
	if _, err := e.reg.Stream(ctx, e.nodeID, &agentv1.Request{Body: &agentv1.Request_TailLogs{TailLogs: &agentv1.TailLogsRequest{Lines: 1}}}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.hooks.mu.Lock()
		beats := e.hooks.beat
		e.hooks.mu.Unlock()
		if beats > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("heartbeat never delivered: the log flood is blocking the session's Recv pump")
}
