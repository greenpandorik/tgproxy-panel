# Phase 1 / Part B — Nodes, gateway, agent, install flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect nodes to the panel: protobuf contract, gRPC gateway that agents dial into, `NodeDriver` abstraction with mock and real implementations, the node agent binary (health, profiles apply with `-check`/backup/rollback, site deploy, metrics, stats, logs), and the node CRUD + one-command install flow.

**Architecture:** Agent opens one bidirectional gRPC stream to the panel (`AgentGateway.Session`) authenticated by a node token; the panel correlates requests by `request_id`. `internal/nodedriver.Driver` hides transport. The agent owns all file/systemd operations on the node; the panel owns all state.

**Tech Stack:** grpc-go, protobuf, golang.org/x/net/http2/h2c (gRPC on the same port as HTTP), golang.org/x/sys/unix, chi.

**Spec:** `docs/superpowers/specs/2026-09-03-webproxy-panel-design.md` (§3.2, §3.3, §3.4, §2 facts table)

## Global Constraints

Same as Part A. Additionally:
- Relay admin endpoints: `http://127.0.0.1:8081/{healthz,readyz,metrics}`. MTProxy stats: `http://127.0.0.1:8888/stats`.
- Paths on a node: `/etc/tproxy-server/config.json`, `/etc/tproxy-server/profiles.json` (mode 0400, owner `root:tproxy`), `/etc/mtproxy/mtproxy.env` (0640 `root:mtproxy`), `/srv/tproxy-site`, relay binary `/usr/local/bin/tproxy-server`.
- No hot reload: apply = `systemctl restart mtproxy` (when secrets changed) + `systemctl restart tproxy-server`.
- Every node always has at least one profile: the node's `default` profile (not tied to a key).
- Agent must build with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`.

---

## File structure (Part B)

```
proto/agent/v1/agent.proto
proto/agent/v1/agent.pb.go, agent_grpc.pb.go   # generated
internal/gateway/registry.go       # live connections, Call/Stream correlation
internal/gateway/server.go         # gRPC Session handler, auth, hooks
internal/gateway/gateway_test.go
internal/nodedriver/driver.go      # Driver interface + types + ErrOffline
internal/nodedriver/convert.go     # proto <-> driver types
internal/nodedriver/mock.go        # in-memory driver for tests / NODE_DRIVER=mock
internal/nodedriver/gateway.go     # Driver over gateway.Registry
internal/nodedriver/*_test.go
internal/agent/config.go
internal/agent/exec.go             # Exec/Streamer interfaces + OS impl
internal/agent/health.go
internal/agent/apply.go            # profiles/env/site apply with check+backup+rollback
internal/agent/files.go            # atomic write helpers
internal/agent/logs.go             # journalctl streaming
internal/agent/handler.go          # Request dispatcher
internal/agent/initnode.go         # init-node subcommand logic
internal/agent/run.go              # connect loop
internal/agent/*_test.go
cmd/agent/main.go
internal/nodeinstall/script.sh.tmpl
internal/nodeinstall/fallback-site/index.html, styles.css
internal/nodeinstall/render.go     # Render(params), FallbackSite()
internal/nodeinstall/render_test.go
internal/nodesvc/presence.go       # gateway.Auth + gateway.Hooks backed by store
internal/store/queries/nodes.sql, profiles.sql
internal/api/nodes.go, internal/api/install.go, internal/api/nodes_test.go, internal/api/install_test.go
cmd/panel/main.go                  # h2c mux for gRPC + HTTP, driver selection
```

---

### Task 6: Protobuf contract and gRPC gateway

**Files:**
- Create: `proto/agent/v1/agent.proto`, `internal/gateway/registry.go`, `internal/gateway/server.go`
- Test: `internal/gateway/gateway_test.go`

**Interfaces:**
- Produces:
  - Generated package `agentv1` (`tgwebproxy/proto/agent/v1`).
  - `gateway.NewRegistry() *Registry`; `(*Registry).Online(uuid.UUID) bool`; `(*Registry).OnlineIDs() []uuid.UUID`; `(*Registry).Call(ctx, nodeID, *agentv1.Request) (*agentv1.Response, error)`; `(*Registry).Stream(ctx, nodeID, *agentv1.Request) (<-chan *agentv1.LogChunk, error)`; `gateway.ErrOffline`.
  - `gateway.Auth` interface `{ NodeByToken(ctx, token string) (uuid.UUID, error) }`; `gateway.Hooks` interface `{ OnHello(ctx, uuid.UUID, *agentv1.Hello); OnHeartbeat(ctx, uuid.UUID, *agentv1.HealthReport); OnDisconnect(ctx, uuid.UUID) }`.
  - `gateway.NewServer(reg *Registry, auth Auth, hooks Hooks, log *slog.Logger) *Server` implementing `agentv1.AgentGatewayServer`.

- [ ] **Step 1: Write the proto**

`proto/agent/v1/agent.proto`:
```proto
syntax = "proto3";
package agent.v1;
option go_package = "tgwebproxy/proto/agent/v1;agentv1";

service AgentGateway {
  rpc Session(stream Envelope) returns (stream Envelope);
}

message Envelope {
  string request_id = 1;
  oneof body {
    Hello hello = 2;
    Heartbeat heartbeat = 3;
    Request request = 4;
    Response response = 5;
    LogChunk log_chunk = 6;
  }
}

message Hello {
  string agent_version = 1;
  string tproxy_version = 2;
  string hostname = 3;
}

message Heartbeat { HealthReport health = 1; }

message HealthReport {
  bool relay_active = 1;
  bool mtproxy_active = 2;
  bool caddy_active = 3;
  bool healthz = 4;
  bool readyz = 5;
  string tproxy_version = 6;
  string agent_version = 7;
  int64 uptime_seconds = 8;
  double cpu_percent = 9;
  double mem_used_percent = 10;
  double disk_used_percent = 11;
  int32 profile_count = 12;
}

message ProfileLimits {
  int32 max_sessions = 1;
  int32 max_streams = 2;
  int32 max_backend_dials_in_flight = 3;
  int32 new_sessions_per_minute = 4;
  int32 new_sessions_burst = 5;
  int32 new_streams_per_minute = 6;
  int32 new_streams_burst = 7;
  int32 max_streams_per_session = 8;
  int32 max_pending_per_session = 9;
}

message Profile {
  string name = 1;
  string secret = 2;
  string backend = 3;
  string carrier_mode = 4;
  ProfileLimits limits = 5;
}

message ProfilesFile { repeated Profile profiles = 1; }

message SiteFile { string path = 1; bytes content = 2; }
message SiteBundle { repeated SiteFile files = 1; }

message Request {
  oneof body {
    HealthRequest health = 1;
    GetProfilesRequest get_profiles = 2;
    ApplyRequest apply = 3;
    GetSiteRequest get_site = 4;
    MetricsRequest metrics = 5;
    StatsRequest stats = 6;
    TailLogsRequest tail_logs = 7;
    RestartRelayRequest restart_relay = 8;
  }
}
message HealthRequest {}
message GetProfilesRequest {}
message ApplyRequest {
  bool apply_profiles = 1;
  repeated Profile profiles = 2;
  repeated string mtproxy_secrets = 3;
  SiteBundle site = 4;   // nil = leave site untouched
}
message GetSiteRequest {}
message MetricsRequest {}
message StatsRequest {}
message TailLogsRequest { repeated string services = 1; int32 lines = 2; bool follow = 3; }
message RestartRelayRequest {}

message Response {
  string error = 1;
  oneof body {
    HealthReport health = 2;
    ProfilesFile profiles = 3;
    ApplyResult apply = 4;
    SiteBundle site = 5;
    MetricsText metrics = 6;
    StatsMap stats = 7;
    Empty empty = 8;
  }
}
message ApplyResult {
  bool ok = 1;
  bool restarted_relay = 2;
  bool restarted_mtproxy = 3;
  bool rolled_back = 4;
  string log = 5;
}
message MetricsText { string text = 1; }
message StatsMap { map<string, string> values = 1; }
message Empty {}

message LogLine { string service = 1; string line = 2; int64 unix_ms = 3; }
message LogChunk { repeated LogLine lines = 1; bool done = 2; string error = 3; }
```

Run: `make proto` (requires `protoc-gen-go` and `protoc-gen-go-grpc` on PATH). Expected: `proto/agent/v1/agent.pb.go` and `agent_grpc.pb.go` exist. Then `go get google.golang.org/grpc google.golang.org/protobuf && go mod tidy`.

- [ ] **Step 2: Write failing gateway test**

`internal/gateway/gateway_test.go`:
```go
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

func (h *fakeHooks) OnHello(context.Context, uuid.UUID, *agentv1.Hello)             { h.mu.Lock(); h.hello++; h.mu.Unlock() }
func (h *fakeHooks) OnHeartbeat(context.Context, uuid.UUID, *agentv1.HealthReport)  { h.mu.Lock(); h.beat++; h.mu.Unlock() }
func (h *fakeHooks) OnDisconnect(context.Context, uuid.UUID)                        { h.mu.Lock(); h.disconnects++; h.mu.Unlock() }

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
					Body: &agentv1.Response_Health{Health: &agentv1.HealthReport{RelayActive: true}}}}})
			case *agentv1.Request_TailLogs:
				_ = stream.Send(&agentv1.Envelope{RequestId: env.RequestId, Body: &agentv1.Envelope_LogChunk{LogChunk: &agentv1.LogChunk{
					Lines: []*agentv1.LogLine{{Service: "tproxy-server", Line: "one"}}}}})
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
	stream, cancel := fakeAgent(t, e, "bad")
	defer cancel()
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/gateway/`
Expected: compile errors.

- [ ] **Step 4: Implement registry**

`internal/gateway/registry.go`:
```go
// Package gateway accepts agent sessions and routes panel requests to them.
package gateway

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	agentv1 "tgwebproxy/proto/agent/v1"
)

var ErrOffline = errors.New("node offline")

type conn struct {
	nodeID  uuid.UUID
	send    chan *agentv1.Envelope
	mu      sync.Mutex
	pending map[string]chan *agentv1.Envelope
	closed  chan struct{}
}

func newConn(id uuid.UUID) *conn {
	return &conn{nodeID: id, send: make(chan *agentv1.Envelope, 64), pending: map[string]chan *agentv1.Envelope{}, closed: make(chan struct{})}
}

func (c *conn) register(id string) chan *agentv1.Envelope {
	ch := make(chan *agentv1.Envelope, 32)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	return ch
}

func (c *conn) unregister(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *conn) deliver(env *agentv1.Envelope) {
	c.mu.Lock()
	ch := c.pending[env.RequestId]
	c.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- env:
	default:
	}
}

func (c *conn) close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
}

type Registry struct {
	mu    sync.RWMutex
	conns map[uuid.UUID]*conn
}

func NewRegistry() *Registry { return &Registry{conns: map[uuid.UUID]*conn{}} }

func (r *Registry) add(c *conn) (replaced *conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	replaced = r.conns[c.nodeID]
	r.conns[c.nodeID] = c
	return replaced
}

func (r *Registry) remove(c *conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[c.nodeID] == c {
		delete(r.conns, c.nodeID)
		return true
	}
	return false
}

func (r *Registry) get(id uuid.UUID) *conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conns[id]
}

func (r *Registry) Online(id uuid.UUID) bool { return r.get(id) != nil }

func (r *Registry) OnlineIDs() []uuid.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(r.conns))
	for id := range r.conns {
		ids = append(ids, id)
	}
	return ids
}

func (r *Registry) send(ctx context.Context, c *conn, env *agentv1.Envelope) error {
	select {
	case c.send <- env:
		return nil
	case <-c.closed:
		return ErrOffline
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Call sends one request and waits for its Response.
func (r *Registry) Call(ctx context.Context, id uuid.UUID, req *agentv1.Request) (*agentv1.Response, error) {
	c := r.get(id)
	if c == nil {
		return nil, ErrOffline
	}
	rid := uuid.NewString()
	ch := c.register(rid)
	defer c.unregister(rid)
	if err := r.send(ctx, c, &agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_Request{Request: req}}); err != nil {
		return nil, err
	}
	select {
	case env := <-ch:
		resp := env.GetResponse()
		if resp == nil {
			return nil, errors.New("agent sent non-response")
		}
		if resp.Error != "" {
			return resp, errors.New(resp.Error)
		}
		return resp, nil
	case <-c.closed:
		return nil, ErrOffline
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stream sends a request and yields LogChunks until one with Done or ctx ends.
func (r *Registry) Stream(ctx context.Context, id uuid.UUID, req *agentv1.Request) (<-chan *agentv1.LogChunk, error) {
	c := r.get(id)
	if c == nil {
		return nil, ErrOffline
	}
	rid := uuid.NewString()
	ch := c.register(rid)
	if err := r.send(ctx, c, &agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_Request{Request: req}}); err != nil {
		c.unregister(rid)
		return nil, err
	}
	out := make(chan *agentv1.LogChunk, 32)
	go func() {
		defer close(out)
		defer c.unregister(rid)
		for {
			select {
			case env := <-ch:
				chunk := env.GetLogChunk()
				if chunk == nil {
					continue
				}
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
				if chunk.Done {
					return
				}
			case <-c.closed:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
```

- [ ] **Step 5: Implement server**

`internal/gateway/server.go`:
```go
package gateway

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type Auth interface {
	NodeByToken(ctx context.Context, token string) (uuid.UUID, error)
}

type Hooks interface {
	OnHello(ctx context.Context, nodeID uuid.UUID, hello *agentv1.Hello)
	OnHeartbeat(ctx context.Context, nodeID uuid.UUID, health *agentv1.HealthReport)
	OnDisconnect(ctx context.Context, nodeID uuid.UUID)
}

type Server struct {
	agentv1.UnimplementedAgentGatewayServer
	reg   *Registry
	auth  Auth
	hooks Hooks
	log   *slog.Logger
}

func NewServer(reg *Registry, auth Auth, hooks Hooks, log *slog.Logger) *Server {
	return &Server{reg: reg, auth: auth, hooks: hooks, log: log}
}

func bearer(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, v := range md.Get("authorization") {
		if strings.HasPrefix(v, "Bearer ") {
			return strings.TrimPrefix(v, "Bearer ")
		}
	}
	return ""
}

func (s *Server) Session(stream agentv1.AgentGateway_SessionServer) error {
	ctx := stream.Context()
	nodeID, err := s.auth.NodeByToken(ctx, bearer(ctx))
	if err != nil {
		return status.Error(codes.Unauthenticated, "invalid node token")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "first message must be Hello")
	}
	c := newConn(nodeID)
	if old := s.reg.add(c); old != nil {
		old.close()
	}
	s.hooks.OnHello(ctx, nodeID, hello)
	s.log.Info("agent connected", "node", nodeID, "agent_version", hello.AgentVersion)

	defer func() {
		c.close()
		if s.reg.remove(c) {
			s.hooks.OnDisconnect(context.WithoutCancel(ctx), nodeID)
			s.log.Info("agent disconnected", "node", nodeID)
		}
	}()

	sendErr := make(chan error, 1)
	go func() {
		for {
			select {
			case env := <-c.send:
				if err := stream.Send(env); err != nil {
					sendErr <- err
					return
				}
			case <-c.closed:
				sendErr <- nil
				return
			case <-ctx.Done():
				sendErr <- ctx.Err()
				return
			}
		}
	}()

	recvErr := make(chan error, 1)
	go func() {
		for {
			env, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			switch b := env.Body.(type) {
			case *agentv1.Envelope_Heartbeat:
				s.hooks.OnHeartbeat(ctx, nodeID, b.Heartbeat.GetHealth())
			case *agentv1.Envelope_Response, *agentv1.Envelope_LogChunk:
				c.deliver(env)
			}
		}
	}()

	select {
	case err := <-sendErr:
		return err
	case err := <-recvErr:
		if ctx.Err() != nil {
			return nil
		}
		return err
	case <-c.closed:
		return status.Error(codes.Aborted, "replaced by a newer session")
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go mod tidy && go test ./internal/gateway/ -race -v`
Expected: 5 PASS.

---

### Task 7: NodeDriver interface, conversions, mock and gateway drivers

**Files:**
- Create: `internal/nodedriver/driver.go`, `internal/nodedriver/convert.go`, `internal/nodedriver/mock.go`, `internal/nodedriver/gateway.go`
- Test: `internal/nodedriver/convert_test.go`, `internal/nodedriver/mock_test.go`

**Interfaces:**
- Produces (package `nodedriver`):
```go
type HealthReport struct {
    RelayActive, MTProxyActive, CaddyActive, Healthz, Readyz bool
    TProxyVersion, AgentVersion string
    UptimeSeconds int64
    CPUPercent, MemUsedPercent, DiskUsedPercent float64
    ProfileCount int
}
type Profile struct { Name, Secret, Backend string; CarrierMode string; Limits *domain.ProfileLimits }
type SiteBundle struct { Files map[string][]byte }
type ApplyRequest struct { ApplyProfiles bool; Profiles []Profile; MTProxySecrets []string; Site *SiteBundle }
type ApplyResult struct { OK, RestartedRelay, RestartedMTProxy, RolledBack bool; Log string }
type LogLine struct { Service, Line string; Time time.Time }
var ErrOffline = gateway.ErrOffline
type Driver interface {
    Online(nodeID uuid.UUID) bool
    Health(ctx, nodeID) (HealthReport, error)
    GetProfiles(ctx, nodeID) ([]Profile, error)
    Apply(ctx, nodeID, ApplyRequest) (ApplyResult, error)
    GetSite(ctx, nodeID) (SiteBundle, error)
    Metrics(ctx, nodeID) (string, error)
    Stats(ctx, nodeID) (map[string]string, error)
    TailLogs(ctx, nodeID, services []string, lines int, follow bool) (<-chan LogLine, error)
    RestartRelay(ctx, nodeID) error
}
func ProfileToProto(Profile) *agentv1.Profile; func ProfileFromProto(*agentv1.Profile) Profile
func SiteToProto(SiteBundle) *agentv1.SiteBundle; func SiteFromProto(*agentv1.SiteBundle) SiteBundle
func HealthFromProto(*agentv1.HealthReport) HealthReport; func HealthToProto(HealthReport) *agentv1.HealthReport
func NewMock() *Mock  // Mock implements Driver; methods: SetOnline(id, bool), SetHealth(id, HealthReport), Applied(id) []ApplyRequest, FailNextApply(id, msg string), SetMetrics(id, text), SetStats(id, map)
func NewGateway(reg *gateway.Registry, timeout time.Duration) *Gateway  // implements Driver
```

- [ ] **Step 1: Write failing tests**

`internal/nodedriver/convert_test.go`:
```go
package nodedriver

import (
	"testing"

	"tgwebproxy/internal/domain"
)

func TestProfileRoundTrip(t *testing.T) {
	in := Profile{Name: "k1", Secret: "00", Backend: "127.0.0.1:2398", CarrierMode: "https-lanes",
		Limits: &domain.ProfileLimits{MaxSessions: 5, MaxStreamsPerSession: 2}}
	out := ProfileFromProto(ProfileToProto(in))
	if out.Name != in.Name || out.Secret != in.Secret || out.CarrierMode != in.CarrierMode || out.Limits == nil || out.Limits.MaxSessions != 5 {
		t.Fatalf("round trip lost data: %+v", out)
	}
	noLimits := ProfileFromProto(ProfileToProto(Profile{Name: "a"}))
	if noLimits.Limits != nil {
		t.Fatal("nil limits must stay nil")
	}
}

func TestSiteRoundTrip(t *testing.T) {
	in := SiteBundle{Files: map[string][]byte{"index.html": []byte("<p>x</p>"), "s.css": []byte("p{}")}}
	out := SiteFromProto(SiteToProto(in))
	if len(out.Files) != 2 || string(out.Files["index.html"]) != "<p>x</p>" {
		t.Fatalf("bad %+v", out)
	}
}
```

`internal/nodedriver/mock_test.go`:
```go
package nodedriver

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMockApplyRecordsAndFails(t *testing.T) {
	m := NewMock()
	id := uuid.New()
	ctx := context.Background()
	if _, err := m.Health(ctx, id); !errors.Is(err, ErrOffline) {
		t.Fatalf("unknown node should be offline, got %v", err)
	}
	m.SetOnline(id, true)
	res, err := m.Apply(ctx, id, ApplyRequest{ApplyProfiles: true, Profiles: []Profile{{Name: "default", Secret: "00"}}})
	if err != nil || !res.OK {
		t.Fatalf("apply %+v %v", res, err)
	}
	profiles, _ := m.GetProfiles(ctx, id)
	if len(profiles) != 1 || profiles[0].Name != "default" {
		t.Fatalf("profiles not stored: %+v", profiles)
	}
	m.FailNextApply(id, "check failed")
	res, err = m.Apply(ctx, id, ApplyRequest{ApplyProfiles: true})
	if err == nil || res.OK {
		t.Fatal("expected failure")
	}
	if len(m.Applied(id)) != 2 {
		t.Fatalf("applied = %d", len(m.Applied(id)))
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/nodedriver/`
Expected: compile errors.

- [ ] **Step 3: Implement driver.go and convert.go**

`internal/nodedriver/driver.go`:
```go
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
}

type SiteBundle struct{ Files map[string][]byte }

type ApplyRequest struct {
	ApplyProfiles  bool
	Profiles       []Profile
	MTProxySecrets []string
	Site           *SiteBundle
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
```

`internal/nodedriver/convert.go`:
```go
package nodedriver

import (
	"time"

	"tgwebproxy/internal/domain"
	agentv1 "tgwebproxy/proto/agent/v1"
)

func ProfileToProto(p Profile) *agentv1.Profile {
	out := &agentv1.Profile{Name: p.Name, Secret: p.Secret, Backend: p.Backend, CarrierMode: p.CarrierMode}
	if p.Limits != nil {
		l := p.Limits
		out.Limits = &agentv1.ProfileLimits{
			MaxSessions: int32(l.MaxSessions), MaxStreams: int32(l.MaxStreams),
			MaxBackendDialsInFlight: int32(l.MaxBackendDialsInFlight),
			NewSessionsPerMinute: int32(l.NewSessionsPerMinute), NewSessionsBurst: int32(l.NewSessionsBurst),
			NewStreamsPerMinute: int32(l.NewStreamsPerMinute), NewStreamsBurst: int32(l.NewStreamsBurst),
			MaxStreamsPerSession: int32(l.MaxStreamsPerSession), MaxPendingPerSession: int32(l.MaxPendingPerSession),
		}
	}
	return out
}

func ProfileFromProto(p *agentv1.Profile) Profile {
	out := Profile{Name: p.GetName(), Secret: p.GetSecret(), Backend: p.GetBackend(), CarrierMode: p.GetCarrierMode()}
	if l := p.GetLimits(); l != nil {
		out.Limits = &domain.ProfileLimits{
			MaxSessions: int(l.MaxSessions), MaxStreams: int(l.MaxStreams),
			MaxBackendDialsInFlight: int(l.MaxBackendDialsInFlight),
			NewSessionsPerMinute: int(l.NewSessionsPerMinute), NewSessionsBurst: int(l.NewSessionsBurst),
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
```

- [ ] **Step 4: Implement mock.go**

`internal/nodedriver/mock.go`:
```go
package nodedriver

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

type mockNode struct {
	online   bool
	health   HealthReport
	profiles []Profile
	site     SiteBundle
	metrics  string
	stats    map[string]string
	applied  []ApplyRequest
	failMsg  string
	restarts int
}

// Mock is an in-memory Driver for tests and NODE_DRIVER=mock.
type Mock struct {
	mu    sync.Mutex
	nodes map[uuid.UUID]*mockNode
}

func NewMock() *Mock { return &Mock{nodes: map[uuid.UUID]*mockNode{}} }

func (m *Mock) node(id uuid.UUID) *mockNode {
	n := m.nodes[id]
	if n == nil {
		n = &mockNode{health: HealthReport{RelayActive: true, MTProxyActive: true, CaddyActive: true, Healthz: true, Readyz: true, TProxyVersion: "mock"}, stats: map[string]string{}}
		m.nodes[id] = n
	}
	return n
}

func (m *Mock) SetOnline(id uuid.UUID, online bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).online = online
}

func (m *Mock) SetHealth(id uuid.UUID, h HealthReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).health = h
}

func (m *Mock) SetMetrics(id uuid.UUID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).metrics = text
}

func (m *Mock) SetStats(id uuid.UUID, s map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).stats = s
}

func (m *Mock) FailNextApply(id uuid.UUID, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.node(id).failMsg = msg
}

func (m *Mock) Applied(id uuid.UUID) []ApplyRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ApplyRequest(nil), m.node(id).applied...)
}

func (m *Mock) Restarts(id uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.node(id).restarts
}

func (m *Mock) get(id uuid.UUID) (*mockNode, error) {
	n := m.nodes[id]
	if n == nil || !n.online {
		return nil, ErrOffline
	}
	return n, nil
}

func (m *Mock) Online(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[id]
	return n != nil && n.online
}

func (m *Mock) Health(_ context.Context, id uuid.UUID) (HealthReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return HealthReport{}, err
	}
	h := n.health
	h.ProfileCount = len(n.profiles)
	return h, nil
}

func (m *Mock) GetProfiles(_ context.Context, id uuid.UUID) ([]Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return append([]Profile(nil), n.profiles...), nil
}

func (m *Mock) Apply(_ context.Context, id uuid.UUID, req ApplyRequest) (ApplyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return ApplyResult{}, err
	}
	n.applied = append(n.applied, req)
	if n.failMsg != "" {
		msg := n.failMsg
		n.failMsg = ""
		return ApplyResult{OK: false, RolledBack: true, Log: msg}, errors.New(msg)
	}
	res := ApplyResult{OK: true, Log: "mock apply ok"}
	if req.ApplyProfiles {
		n.profiles = append([]Profile(nil), req.Profiles...)
		res.RestartedRelay, res.RestartedMTProxy = true, true
	}
	if req.Site != nil {
		n.site = *req.Site
		res.RestartedRelay = true
	}
	return res, nil
}

func (m *Mock) GetSite(_ context.Context, id uuid.UUID) (SiteBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return SiteBundle{}, err
	}
	return n.site, nil
}

func (m *Mock) Metrics(_ context.Context, id uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return "", err
	}
	if n.metrics == "" {
		return "tproxy_sessions_live 0\ntproxy_streams_live 0\ntproxy_bytes_up_total 0\ntproxy_bytes_down_total 0\ntproxy_sessions_created_total 0\ntproxy_limit_hits_total 0\n", nil
	}
	return n.metrics, nil
}

func (m *Mock) Stats(_ context.Context, id uuid.UUID) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return n.stats, nil
}

func (m *Mock) TailLogs(ctx context.Context, id uuid.UUID, services []string, lines int, follow bool) (<-chan LogLine, error) {
	if !m.Online(id) {
		return nil, ErrOffline
	}
	out := make(chan LogLine, 8)
	go func() {
		defer close(out)
		for _, svc := range services {
			select {
			case out <- LogLine{Service: svc, Line: "mock log line", Time: time.Now()}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (m *Mock) RestartRelay(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := m.get(id)
	if err != nil {
		return err
	}
	n.restarts++
	return nil
}
```

- [ ] **Step 5: Implement gateway.go driver**

`internal/nodedriver/gateway.go`:
```go
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
	pr := &agentv1.ApplyRequest{ApplyProfiles: req.ApplyProfiles, MtproxySecrets: req.MTProxySecrets}
	for _, p := range req.Profiles {
		pr.Profiles = append(pr.Profiles, ProfileToProto(p))
	}
	if req.Site != nil {
		pr.Site = SiteToProto(*req.Site)
	}
	// apply restarts services and waits for health: allow longer than a plain call.
	resp, err := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Apply{Apply: pr}}, 90*time.Second)
	if resp != nil && resp.GetApply() != nil {
		a := resp.GetApply()
		res := ApplyResult{OK: a.Ok, RestartedRelay: a.RestartedRelay, RestartedMTProxy: a.RestartedMtproxy, RolledBack: a.RolledBack, Log: a.Log}
		if err != nil {
			return res, err
		}
		if !a.Ok {
			return res, errors.New("apply failed on node")
		}
		return res, nil
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
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/nodedriver/ -v && go vet ./...`
Expected: PASS.

---

### Task 8: Node agent

**Files:**
- Create: `internal/agent/config.go`, `exec.go`, `files.go`, `health.go`, `apply.go`, `logs.go`, `handler.go`, `initnode.go`, `run.go`
- Test: `internal/agent/apply_test.go`, `internal/agent/initnode_test.go`, `internal/agent/handler_test.go`
- Modify: `cmd/agent/main.go`

**Interfaces:**
- Produces:
```go
package agent
const Version = "0.1.0"
type Config struct { PanelURL, Token, StateDir, TProxyBin, ConfigPath, ProfilesPath, MTProxyEnvPath, SiteDir, RelayAdminURL, MTProxyStatsURL, TProxyVersion string; HealthWait time.Duration }
func LoadConfig(getenv func(string) string) (Config, error) // TGWP_PANEL_URL, TGWP_TOKEN (or TGWP_TOKEN_FILE), TGWP_STATE_DIR (/var/lib/tgwp-agent), TGWP_TPROXY_BIN, TGWP_CONFIG, TGWP_PROFILES, TGWP_MTPROXY_ENV, TGWP_SITE_DIR, TGWP_RELAY_ADMIN (http://127.0.0.1:8081), TGWP_MTPROXY_STATS (http://127.0.0.1:8888/stats), TGWP_TPROXY_VERSION
type Exec interface {
    Run(ctx context.Context, name string, args ...string) ([]byte, error)          // combined output
    Start(ctx context.Context, name string, args ...string) (io.ReadCloser, error) // streaming stdout
}
type OSExec struct{}
type Handler struct{...}
func NewHandler(cfg Config, ex Exec, log *slog.Logger) *Handler
func (h *Handler) Handle(ctx, *agentv1.Request) *agentv1.Response
func (h *Handler) Health(ctx) *agentv1.HealthReport
func (h *Handler) TailLogs(ctx, *agentv1.TailLogsRequest, send func(*agentv1.LogChunk) error)
func (h *Handler) Apply(ctx, *agentv1.ApplyRequest) *agentv1.ApplyResult
func RenderProfilesJSON(ps []*agentv1.Profile) ([]byte, error)
func RenderMTProxyEnv(existing []byte, secrets []string) []byte
func InitNode(configPath, envPath, dropinPath string, maxProfiles int) error
func Run(ctx, cfg Config, h *Handler, log *slog.Logger) error
```

- [ ] **Step 1: Write failing tests**

`internal/agent/apply_test.go`:
```go
package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type fakeExec struct {
	mu       sync.Mutex
	calls    []string
	failOn   string // substring of command that should fail
	failMsg  string
	active   map[string]bool
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	if f.failOn != "" && strings.Contains(cmd, f.failOn) {
		return []byte(f.failMsg), errors.New("exit 1")
	}
	if name == "systemctl" && args[0] == "is-active" {
		if f.active == nil || f.active[args[1]] {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), errors.New("exit 3")
	}
	return nil, nil
}

func (f *fakeExec) Start(_ context.Context, name string, args ...string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("2026-09-03T10:00:00+0000 host tproxy-server[1]: started\n")), nil
}

func (f *fakeExec) has(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func testHandler(t *testing.T, ex *fakeExec, healthOK bool) (*Handler, Config) {
	t.Helper()
	dir := t.TempDir()
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthOK {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte("ok\n"))
		case "/readyz":
			_, _ = w.Write([]byte("ready\n"))
		case "/metrics":
			_, _ = w.Write([]byte("tproxy_sessions_live 3\n"))
		case "/stats":
			_, _ = w.Write([]byte("total_connections\t12\nactive_connections\t3\n"))
		}
	}))
	t.Cleanup(admin.Close)
	cfg := Config{
		StateDir: filepath.Join(dir, "state"), TProxyBin: "/usr/local/bin/tproxy-server",
		ConfigPath: filepath.Join(dir, "config.json"), ProfilesPath: filepath.Join(dir, "profiles.json"),
		MTProxyEnvPath: filepath.Join(dir, "mtproxy.env"), SiteDir: filepath.Join(dir, "site"),
		RelayAdminURL: admin.URL, MTProxyStatsURL: admin.URL + "/stats", TProxyVersion: "abc", HealthWait: 300 * time.Millisecond,
	}
	_ = os.WriteFile(cfg.ConfigPath, []byte(`{"public_hostname":"n.test"}`), 0o640)
	_ = os.WriteFile(cfg.ProfilesPath, []byte(`{"profiles":[{"name":"default","secret":"00000000000000000000000000000000","backend":"127.0.0.1:2398"}]}`), 0o400)
	_ = os.WriteFile(cfg.MTProxyEnvPath, []byte("MTPROXY_SECRET=00000000000000000000000000000000\nMTPROXY_WORKERS=2\nMTPROXY_MAX_CONNECTIONS=4096\n"), 0o640)
	_ = os.MkdirAll(cfg.SiteDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfg.SiteDir, "index.html"), []byte("old"), 0o644)
	return NewHandler(cfg, ex, slog.New(slog.DiscardHandler)), cfg
}

func TestApplyWritesFilesAndRestarts(t *testing.T) {
	ex := &fakeExec{}
	h, cfg := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{
		ApplyProfiles: true,
		Profiles: []*agentv1.Profile{
			{Name: "default", Secret: "00000000000000000000000000000000", Backend: "127.0.0.1:2398"},
			{Name: "kabc", Secret: "11111111111111111111111111111111", Backend: "127.0.0.1:2398", CarrierMode: "https-lanes", Limits: &agentv1.ProfileLimits{MaxSessions: 4}},
		},
		MtproxySecrets: []string{"00000000000000000000000000000000", "11111111111111111111111111111111"},
		Site:           &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "index.html", Content: []byte("new")}, {Path: "a/s.css", Content: []byte("p{}")}}},
	})
	if !res.Ok {
		t.Fatalf("apply failed: %s", res.Log)
	}
	if !res.RestartedMtproxy || !res.RestartedRelay {
		t.Fatalf("expected both restarts: %+v", res)
	}
	if !ex.has("-check") || !ex.has("systemctl restart mtproxy") || !ex.has("systemctl restart tproxy-server") {
		t.Fatalf("missing commands: %v", ex.calls)
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	if !strings.Contains(string(prof), `"name":"kabc"`) || !strings.Contains(string(prof), `"max_sessions":4`) || strings.Contains(string(prof), "max_streams") {
		t.Fatalf("profiles.json wrong: %s", prof)
	}
	st, _ := os.Stat(cfg.ProfilesPath)
	if st.Mode().Perm() != 0o400 {
		t.Fatalf("profiles mode %o", st.Mode().Perm())
	}
	env, _ := os.ReadFile(cfg.MTProxyEnvPath)
	if !strings.Contains(string(env), "MTPROXY_SECRETS=-S 00000000000000000000000000000000 -S 11111111111111111111111111111111") || !strings.Contains(string(env), "MTPROXY_WORKERS=2") {
		t.Fatalf("env wrong: %s", env)
	}
	idx, _ := os.ReadFile(filepath.Join(cfg.SiteDir, "index.html"))
	css, _ := os.ReadFile(filepath.Join(cfg.SiteDir, "a", "s.css"))
	if string(idx) != "new" || string(css) != "p{}" {
		t.Fatal("site not deployed")
	}
	entries, _ := os.ReadDir(filepath.Join(cfg.StateDir, "backup"))
	if len(entries) != 1 {
		t.Fatalf("expected one backup dir, got %d", len(entries))
	}
}

func TestApplyCheckFailureLeavesFilesUntouched(t *testing.T) {
	ex := &fakeExec{failOn: "-check", failMsg: "profile \"x\": secret must decode to 16 bytes"}
	h, cfg := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{ApplyProfiles: true,
		Profiles: []*agentv1.Profile{{Name: "x", Secret: "zz", Backend: "127.0.0.1:2398"}}, MtproxySecrets: []string{"zz"}})
	if res.Ok || !strings.Contains(res.Log, "16 bytes") {
		t.Fatalf("expected check failure, got %+v", res)
	}
	if ex.has("systemctl restart") {
		t.Fatal("must not restart after failed check")
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	if !strings.Contains(string(prof), `"name":"default"`) {
		t.Fatal("original profiles overwritten")
	}
}

func TestApplyRollsBackWhenHealthFails(t *testing.T) {
	ex := &fakeExec{}
	h, cfg := testHandler(t, ex, false)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{ApplyProfiles: true,
		Profiles: []*agentv1.Profile{{Name: "n", Secret: "11111111111111111111111111111111", Backend: "127.0.0.1:2398"}},
		MtproxySecrets: []string{"11111111111111111111111111111111"}})
	if res.Ok || !res.RolledBack {
		t.Fatalf("expected rollback, got %+v", res)
	}
	prof, _ := os.ReadFile(cfg.ProfilesPath)
	env, _ := os.ReadFile(cfg.MTProxyEnvPath)
	if !strings.Contains(string(prof), `"name":"default"`) || !strings.Contains(string(env), "MTPROXY_SECRET=00000000000000000000000000000000") {
		t.Fatalf("rollback did not restore files: %s / %s", prof, env)
	}
}

func TestApplySiteOnlyRestartsRelayOnly(t *testing.T) {
	ex := &fakeExec{}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "index.html", Content: []byte("x")}}}})
	if !res.Ok || res.RestartedMtproxy || !res.RestartedRelay {
		t.Fatalf("got %+v", res)
	}
}

func TestApplyUnchangedIsNoop(t *testing.T) {
	ex := &fakeExec{}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{ApplyProfiles: true,
		Profiles:       []*agentv1.Profile{{Name: "default", Secret: "00000000000000000000000000000000", Backend: "127.0.0.1:2398"}},
		MtproxySecrets: []string{"00000000000000000000000000000000"}})
	if !res.Ok || res.RestartedRelay || res.RestartedMtproxy {
		t.Fatalf("expected noop, got %+v", res)
	}
}

func TestRejectsSitePathTraversal(t *testing.T) {
	ex := &fakeExec{}
	h, _ := testHandler(t, ex, true)
	res := h.Apply(context.Background(), &agentv1.ApplyRequest{Site: &agentv1.SiteBundle{Files: []*agentv1.SiteFile{{Path: "../etc/x", Content: []byte("x")}}}})
	if res.Ok {
		t.Fatal("path traversal accepted")
	}
}
```

`internal/agent/handler_test.go`:
```go
package agent

import (
	"context"
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
```

`internal/agent/initnode_test.go`:
```go
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitNode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	envPath := filepath.Join(dir, "mtproxy.env")
	dropin := filepath.Join(dir, "secrets.conf")
	_ = os.WriteFile(cfgPath, []byte(`{"public_hostname":"n.test","listen":"127.0.0.1:8080","limits":{"max_sessions_global":64}}`), 0o640)
	_ = os.WriteFile(envPath, []byte("MTPROXY_SECRET=00000000000000000000000000000000\nMTPROXY_WORKERS=1\nMTPROXY_MAX_CONNECTIONS=4096\n"), 0o640)
	if err := InitNode(cfgPath, envPath, dropin, 128); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	raw, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(raw, &cfg)
	limits := cfg["limits"].(map[string]any)
	if limits["max_profiles"].(float64) != 128 || limits["max_sessions_global"].(float64) != 64 || cfg["listen"] != "127.0.0.1:8080" {
		t.Fatalf("config not merged: %s", raw)
	}
	env, _ := os.ReadFile(envPath)
	if !strings.Contains(string(env), "MTPROXY_SECRETS=-S 00000000000000000000000000000000") {
		t.Fatalf("env: %s", env)
	}
	d, _ := os.ReadFile(dropin)
	if !strings.Contains(string(d), "ExecStart=\n") || !strings.Contains(string(d), "$MTPROXY_SECRETS") {
		t.Fatalf("dropin: %s", d)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/`
Expected: compile errors.

- [ ] **Step 3: Implement config.go, exec.go, files.go**

`internal/agent/config.go`:
```go
// Package agent runs on a node and executes panel commands.
package agent

import (
	"errors"
	"os"
	"strings"
	"time"
)

const Version = "0.1.0"

type Config struct {
	PanelURL, Token, StateDir, TProxyBin, ConfigPath, ProfilesPath, MTProxyEnvPath, SiteDir string
	RelayAdminURL, MTProxyStatsURL, TProxyVersion                                          string
	HealthWait                                                                             time.Duration
}

func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	cfg := Config{
		PanelURL: strings.TrimRight(get("TGWP_PANEL_URL", ""), "/"), Token: get("TGWP_TOKEN", ""),
		StateDir: get("TGWP_STATE_DIR", "/var/lib/tgwp-agent"), TProxyBin: get("TGWP_TPROXY_BIN", "/usr/local/bin/tproxy-server"),
		ConfigPath: get("TGWP_CONFIG", "/etc/tproxy-server/config.json"), ProfilesPath: get("TGWP_PROFILES", "/etc/tproxy-server/profiles.json"),
		MTProxyEnvPath: get("TGWP_MTPROXY_ENV", "/etc/mtproxy/mtproxy.env"), SiteDir: get("TGWP_SITE_DIR", "/srv/tproxy-site"),
		RelayAdminURL: get("TGWP_RELAY_ADMIN", "http://127.0.0.1:8081"), MTProxyStatsURL: get("TGWP_MTPROXY_STATS", "http://127.0.0.1:8888/stats"),
		TProxyVersion: get("TGWP_TPROXY_VERSION", "unknown"), HealthWait: 20 * time.Second,
	}
	if cfg.Token == "" {
		if f := getenv("TGWP_TOKEN_FILE"); f != "" {
			b, err := os.ReadFile(f)
			if err != nil {
				return cfg, err
			}
			cfg.Token = strings.TrimSpace(string(b))
		}
	}
	if cfg.PanelURL == "" || cfg.Token == "" {
		return cfg, errors.New("TGWP_PANEL_URL and TGWP_TOKEN are required")
	}
	return cfg, nil
}
```

`internal/agent/exec.go`:
```go
package agent

import (
	"context"
	"io"
	"os/exec"
)

type Exec interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	Start(ctx context.Context, name string, args ...string) (io.ReadCloser, error)
}

type OSExec struct{}

func (OSExec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type cmdReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReader) Close() error {
	_ = c.ReadCloser.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	return nil
}

func (OSExec) Start(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{ReadCloser: out, cmd: cmd}, nil
}
```

`internal/agent/files.go`:
```go
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeAtomic writes data to a temp file in the same directory, sets mode, and renames over path.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// chown is best-effort via the exec layer so tests on macOS do not need root.
func (h *Handler) chown(ctx context.Context, path, owner string) {
	_, _ = h.exec.Run(ctx, "chown", owner, path)
}

// safeSitePath rejects absolute paths, traversal and empty segments.
func safeSitePath(p string) (string, error) {
	clean := filepath.Clean("/" + p)
	if strings.Contains(p, "..") || clean == "/" {
		return "", fmt.Errorf("invalid site path %q", p)
	}
	return strings.TrimPrefix(clean, "/"), nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
```

- [ ] **Step 4: Implement apply.go**

`internal/agent/apply.go`:
```go
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type profileJSON struct {
	Name        string          `json:"name"`
	Secret      string          `json:"secret"`
	Backend     string          `json:"backend"`
	CarrierMode string          `json:"carrier_mode,omitempty"`
	Limits      json.RawMessage `json:"limits,omitempty"`
}

func limitsJSON(l *agentv1.ProfileLimits) json.RawMessage {
	if l == nil {
		return nil
	}
	m := map[string]int32{}
	set := func(k string, v int32) {
		if v > 0 {
			m[k] = v
		}
	}
	set("max_sessions", l.MaxSessions)
	set("max_streams", l.MaxStreams)
	set("max_backend_dials_in_flight", l.MaxBackendDialsInFlight)
	set("new_sessions_per_minute", l.NewSessionsPerMinute)
	set("new_sessions_burst", l.NewSessionsBurst)
	set("new_streams_per_minute", l.NewStreamsPerMinute)
	set("new_streams_burst", l.NewStreamsBurst)
	set("max_streams_per_session", l.MaxStreamsPerSession)
	set("max_pending_per_session", l.MaxPendingPerSession)
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

// RenderProfilesJSON produces the relay profiles file (compact JSON).
func RenderProfilesJSON(ps []*agentv1.Profile) ([]byte, error) {
	if len(ps) == 0 {
		return nil, errors.New("at least one profile is required")
	}
	out := struct {
		Profiles []profileJSON `json:"profiles"`
	}{}
	for _, p := range ps {
		backend := p.Backend
		if backend == "" {
			backend = "127.0.0.1:2398"
		}
		out.Profiles = append(out.Profiles, profileJSON{Name: p.Name, Secret: p.Secret, Backend: backend, CarrierMode: p.CarrierMode, Limits: limitsJSON(p.Limits)})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// RenderMTProxyEnv keeps non-secret lines from the existing env and rewrites secret lines.
func RenderMTProxyEnv(existing []byte, secrets []string) []byte {
	var buf bytes.Buffer
	for _, line := range strings.Split(string(existing), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "MTPROXY_SECRET=") || strings.HasPrefix(t, "MTPROXY_SECRETS=") {
			continue
		}
		buf.WriteString(line + "\n")
	}
	if !bytes.Contains(buf.Bytes(), []byte("MTPROXY_WORKERS=")) {
		buf.WriteString("MTPROXY_WORKERS=1\n")
	}
	if !bytes.Contains(buf.Bytes(), []byte("MTPROXY_MAX_CONNECTIONS=")) {
		buf.WriteString("MTPROXY_MAX_CONNECTIONS=4096\n")
	}
	first := ""
	if len(secrets) > 0 {
		first = secrets[0]
	}
	buf.WriteString("MTPROXY_SECRET=" + first + "\n")
	flags := make([]string, 0, len(secrets))
	for _, s := range secrets {
		flags = append(flags, "-S "+s)
	}
	buf.WriteString("MTPROXY_SECRETS=" + strings.Join(flags, " ") + "\n")
	return buf.Bytes()
}

type applyLog struct{ b strings.Builder }

func (l *applyLog) f(format string, a ...any) { l.b.WriteString(fmt.Sprintf(format, a...) + "\n") }

// Apply validates, backs up, writes, restarts and verifies; on failure it restores the backup.
func (h *Handler) Apply(ctx context.Context, req *agentv1.ApplyRequest) *agentv1.ApplyResult {
	lg := &applyLog{}
	res := &agentv1.ApplyResult{}
	fail := func(err error) *agentv1.ApplyResult {
		lg.f("error: %v", err)
		res.Ok = false
		res.Log = lg.b.String()
		return res
	}

	backupDir := filepath.Join(h.cfg.StateDir, "backup", time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fail(err)
	}

	var newProfiles, newEnv []byte
	profilesChanged, envChanged := false, false
	if req.ApplyProfiles {
		var err error
		if newProfiles, err = RenderProfilesJSON(req.Profiles); err != nil {
			return fail(err)
		}
		oldProfiles, _ := os.ReadFile(h.cfg.ProfilesPath)
		profilesChanged = !bytes.Equal(oldProfiles, newProfiles)
		oldEnv, _ := os.ReadFile(h.cfg.MTProxyEnvPath)
		newEnv = RenderMTProxyEnv(oldEnv, req.MtproxySecrets)
		envChanged = !bytes.Equal(oldEnv, newEnv)
		if profilesChanged {
			candidate := filepath.Join(h.cfg.StateDir, "candidate-profiles.json")
			if err := writeAtomic(candidate, newProfiles, 0o600); err != nil {
				return fail(err)
			}
			out, err := h.exec.Run(ctx, h.cfg.TProxyBin, "-config", h.cfg.ConfigPath, "-profiles-file", candidate, "-check")
			_ = os.Remove(candidate)
			if err != nil {
				return fail(fmt.Errorf("relay -check rejected profiles: %s", strings.TrimSpace(string(out))))
			}
			lg.f("relay -check ok")
		}
	}

	var newSite map[string][]byte
	siteChanged := false
	if req.Site != nil {
		newSite = map[string][]byte{}
		for _, f := range req.Site.Files {
			p, err := safeSitePath(f.Path)
			if err != nil {
				return fail(err)
			}
			newSite[p] = f.Content
		}
		if _, ok := newSite["index.html"]; !ok {
			return fail(errors.New("site bundle must contain index.html"))
		}
		siteChanged = !h.siteEquals(newSite)
	}

	if !profilesChanged && !envChanged && !siteChanged {
		lg.f("nothing changed")
		res.Ok, res.Log = true, lg.b.String()
		return res
	}

	// Backup current state.
	for _, p := range []string{h.cfg.ProfilesPath, h.cfg.MTProxyEnvPath} {
		if data, err := os.ReadFile(p); err == nil {
			_ = os.WriteFile(filepath.Join(backupDir, filepath.Base(p)), data, 0o600)
		}
	}
	if siteChanged {
		_ = copyDir(h.cfg.SiteDir, filepath.Join(backupDir, "site"))
	}
	lg.f("backup written to %s", backupDir)

	rollback := func(cause error) *agentv1.ApplyResult {
		lg.f("rolling back: %v", cause)
		if data, err := os.ReadFile(filepath.Join(backupDir, filepath.Base(h.cfg.ProfilesPath))); err == nil {
			_ = writeAtomic(h.cfg.ProfilesPath, data, 0o400)
			h.chown(ctx, h.cfg.ProfilesPath, "root:tproxy")
		}
		if data, err := os.ReadFile(filepath.Join(backupDir, filepath.Base(h.cfg.MTProxyEnvPath))); err == nil {
			_ = writeAtomic(h.cfg.MTProxyEnvPath, data, 0o640)
			h.chown(ctx, h.cfg.MTProxyEnvPath, "root:mtproxy")
		}
		if siteChanged {
			_ = h.swapSiteDir(filepath.Join(backupDir, "site"))
		}
		if envChanged {
			_, _ = h.exec.Run(ctx, "systemctl", "restart", "mtproxy")
		}
		_, _ = h.exec.Run(ctx, "systemctl", "restart", "tproxy-server")
		_ = h.waitHealthy(ctx)
		res.Ok, res.RolledBack, res.Log = false, true, lg.b.String()
		return res
	}

	if profilesChanged {
		if err := writeAtomic(h.cfg.ProfilesPath, newProfiles, 0o400); err != nil {
			return rollback(err)
		}
		h.chown(ctx, h.cfg.ProfilesPath, "root:tproxy")
		lg.f("profiles.json written (%d profiles)", len(req.Profiles))
	}
	if envChanged {
		if err := writeAtomic(h.cfg.MTProxyEnvPath, newEnv, 0o640); err != nil {
			return rollback(err)
		}
		h.chown(ctx, h.cfg.MTProxyEnvPath, "root:mtproxy")
		lg.f("mtproxy.env written (%d secrets)", len(req.MtproxySecrets))
	}
	if siteChanged {
		staging := h.cfg.SiteDir + ".new"
		_ = os.RemoveAll(staging)
		for p, content := range newSite {
			full := filepath.Join(staging, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return rollback(err)
			}
			if err := os.WriteFile(full, content, 0o644); err != nil {
				return rollback(err)
			}
		}
		if err := h.swapSiteDir(staging); err != nil {
			return rollback(err)
		}
		lg.f("site deployed (%d files)", len(newSite))
	}

	if envChanged {
		if out, err := h.exec.Run(ctx, "systemctl", "restart", "mtproxy"); err != nil {
			return rollback(fmt.Errorf("restart mtproxy: %s", strings.TrimSpace(string(out))))
		}
		res.RestartedMtproxy = true
		lg.f("mtproxy restarted")
	}
	if out, err := h.exec.Run(ctx, "systemctl", "restart", "tproxy-server"); err != nil {
		return rollback(fmt.Errorf("restart tproxy-server: %s", strings.TrimSpace(string(out))))
	}
	res.RestartedRelay = true
	lg.f("tproxy-server restarted")
	if err := h.waitHealthy(ctx); err != nil {
		return rollback(err)
	}
	lg.f("relay healthy")
	res.Ok, res.Log = true, lg.b.String()
	return res
}

func (h *Handler) siteEquals(files map[string][]byte) bool {
	current := h.readSite()
	if len(current) != len(files) {
		return false
	}
	for p, c := range files {
		if !bytes.Equal(current[p], c) {
			return false
		}
	}
	return true
}

func (h *Handler) readSite() map[string][]byte {
	out := map[string][]byte{}
	_ = filepath.Walk(h.cfg.SiteDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(h.cfg.SiteDir, p)
		data, err := os.ReadFile(p)
		if err == nil {
			out[filepath.ToSlash(rel)] = data
		}
		return nil
	})
	return out
}

// swapSiteDir replaces SiteDir with src (a directory) via renames.
func (h *Handler) swapSiteDir(src string) error {
	old := h.cfg.SiteDir + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(h.cfg.SiteDir); err == nil {
		if err := os.Rename(h.cfg.SiteDir, old); err != nil {
			return err
		}
	}
	tmp := h.cfg.SiteDir + ".swap"
	_ = os.RemoveAll(tmp)
	if err := copyDir(src, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, h.cfg.SiteDir); err != nil {
		_ = os.Rename(old, h.cfg.SiteDir)
		return err
	}
	_ = os.RemoveAll(old)
	if strings.HasSuffix(src, ".new") {
		_ = os.RemoveAll(src)
	}
	return nil
}

func (h *Handler) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(h.cfg.HealthWait)
	for {
		if h.probe(ctx, "/healthz") && h.probe(ctx, "/readyz") {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("relay did not become healthy in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (h *Handler) probe(ctx context.Context, path string) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.RelayAdminURL+path, nil)
	resp, err := h.httpc.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == 200
}
```

- [ ] **Step 5: Implement health.go, logs.go, handler.go**

`internal/agent/health.go`:
```go
package agent

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	agentv1 "tgwebproxy/proto/agent/v1"
)

func (h *Handler) unitActive(ctx context.Context, unit string) bool {
	out, err := h.exec.Run(ctx, "systemctl", "is-active", unit)
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

func (h *Handler) profileCount() int {
	raw, err := os.ReadFile(h.cfg.ProfilesPath)
	if err != nil {
		return 0
	}
	var f struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	_ = json.Unmarshal(raw, &f)
	return len(f.Profiles)
}

func readUptime() int64 {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	return int64(f)
}

func readLoadPercent() float64 {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	load, _ := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	p := load / float64(runtime.NumCPU()) * 100
	if p > 100 {
		p = 100
	}
	return p
}

func readMemPercent() float64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var total, avail float64
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total == 0 {
		return 0
	}
	return (total - avail) / total * 100
}

func diskPercent(path string) float64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0
	}
	used := float64(st.Blocks-st.Bfree) / float64(st.Blocks) * 100
	return used
}

func (h *Handler) Health(ctx context.Context) *agentv1.HealthReport {
	return &agentv1.HealthReport{
		RelayActive: h.unitActive(ctx, "tproxy-server"), MtproxyActive: h.unitActive(ctx, "mtproxy"), CaddyActive: h.unitActive(ctx, "caddy"),
		Healthz: h.probe(ctx, "/healthz"), Readyz: h.probe(ctx, "/readyz"),
		TproxyVersion: h.cfg.TProxyVersion, AgentVersion: Version,
		UptimeSeconds: readUptime(), CpuPercent: readLoadPercent(), MemUsedPercent: readMemPercent(), DiskUsedPercent: diskPercent(h.cfg.SiteDir),
		ProfileCount: int32(h.profileCount()),
	}
}
```

`internal/agent/logs.go`:
```go
package agent

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

var allowedUnits = map[string]bool{"tproxy-server": true, "mtproxy": true, "caddy": true, "tgwp-agent": true}

// TailLogs streams journalctl output as LogChunks, ending with Done.
func (h *Handler) TailLogs(ctx context.Context, req *agentv1.TailLogsRequest, send func(*agentv1.LogChunk) error) {
	args := []string{"--no-pager", "-o", "short-iso", "-n", strconv.Itoa(int(max(req.Lines, 1)))}
	for _, u := range req.Services {
		if !allowedUnits[u] {
			_ = send(&agentv1.LogChunk{Done: true, Error: fmt.Sprintf("unit %q not allowed", u)})
			return
		}
		args = append(args, "-u", u)
	}
	if req.Follow {
		args = append(args, "-f")
	}
	rd, err := h.exec.Start(ctx, "journalctl", args...)
	if err != nil {
		_ = send(&agentv1.LogChunk{Done: true, Error: err.Error()})
		return
	}
	defer rd.Close()
	svc := ""
	if len(req.Services) == 1 {
		svc = req.Services[0]
	}
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	batch := &agentv1.LogChunk{}
	flush := func() {
		if len(batch.Lines) > 0 {
			_ = send(batch)
			batch = &agentv1.LogChunk{}
		}
	}
	for sc.Scan() {
		batch.Lines = append(batch.Lines, &agentv1.LogLine{Service: svc, Line: sc.Text(), UnixMs: time.Now().UnixMilli()})
		if len(batch.Lines) >= 50 || req.Follow {
			flush()
		}
		if ctx.Err() != nil {
			break
		}
	}
	flush()
	_ = send(&agentv1.LogChunk{Done: true})
}
```

`internal/agent/handler.go`:
```go
package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

type Handler struct {
	cfg   Config
	exec  Exec
	httpc *http.Client
	log   *slog.Logger
}

func NewHandler(cfg Config, ex Exec, log *slog.Logger) *Handler {
	return &Handler{cfg: cfg, exec: ex, httpc: &http.Client{Timeout: 5 * time.Second}, log: log}
}

func errResp(err error) *agentv1.Response { return &agentv1.Response{Error: err.Error()} }

func (h *Handler) Handle(ctx context.Context, req *agentv1.Request) *agentv1.Response {
	switch b := req.Body.(type) {
	case *agentv1.Request_Health:
		return &agentv1.Response{Body: &agentv1.Response_Health{Health: h.Health(ctx)}}
	case *agentv1.Request_GetProfiles:
		ps, err := h.readProfiles()
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
		text, err := h.get(ctx, h.cfg.RelayAdminURL+"/metrics")
		if err != nil {
			return errResp(err)
		}
		return &agentv1.Response{Body: &agentv1.Response_Metrics{Metrics: &agentv1.MetricsText{Text: text}}}
	case *agentv1.Request_Stats:
		text, err := h.get(ctx, h.cfg.MTProxyStatsURL)
		if err != nil {
			return errResp(err)
		}
		return &agentv1.Response{Body: &agentv1.Response_Stats{Stats: &agentv1.StatsMap{Values: parseStats(text)}}}
	case *agentv1.Request_RestartRelay:
		if out, err := h.exec.Run(ctx, "systemctl", "restart", "tproxy-server"); err != nil {
			return errResp(errorf("restart: %s", strings.TrimSpace(string(out))))
		}
		if err := h.waitHealthy(ctx); err != nil {
			return errResp(err)
		}
		return &agentv1.Response{Body: &agentv1.Response_Empty{Empty: &agentv1.Empty{}}}
	}
	return errResp(errorf("unsupported request"))
}

func (h *Handler) get(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := h.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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
			Name, Secret, Backend, CarrierMode string
			Limits                             *agentv1.ProfileLimits `json:"limits"`
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
```
Add `func errorf(format string, a ...any) error { return fmt.Errorf(format, a...) }` with the `fmt` import. Note: `readProfiles` JSON field names differ in case (`carrier_mode`): add explicit json tags `json:"name"`, `json:"secret"`, `json:"backend"`, `json:"carrier_mode"` on the anonymous struct fields. The `agentv1.ProfileLimits` protobuf struct has `json:"max_sessions,omitempty"` tags generated by protoc-gen-go, so unmarshalling works.

- [ ] **Step 6: Implement initnode.go and run.go**

`internal/agent/initnode.go`:
```go
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const mtproxyDropin = `[Service]
ExecStart=
ExecStart=/opt/MTProxy/objs/bin/mtproto-proxy -u mtproxy -p 8888 -H 2398 $MTPROXY_SECRETS --aes-pwd /etc/mtproxy/proxy-secret /etc/mtproxy/proxy-multi.conf -M ${MTPROXY_WORKERS} -C ${MTPROXY_MAX_CONNECTIONS}
`

// InitNode raises max_profiles in config.json, adds MTPROXY_SECRETS to mtproxy.env and installs the
// systemd drop-in that passes several -S flags. Run once by the installer after the official install.sh.
func InitNode(configPath, envPath, dropinPath string, maxProfiles int) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	limits, _ := cfg["limits"].(map[string]any)
	if limits == nil {
		limits = map[string]any{}
	}
	limits["max_profiles"] = maxProfiles
	cfg["limits"] = limits
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(configPath, append(out, '\n'), 0o640); err != nil {
		return err
	}

	env, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	secret := ""
	for _, line := range strings.Split(string(env), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "MTPROXY_SECRET="); ok {
			secret = v
		}
	}
	if err := writeAtomic(envPath, RenderMTProxyEnv(env, []string{secret}), 0o640); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dropinPath), 0o755); err != nil {
		return err
	}
	return writeAtomic(dropinPath, []byte(mtproxyDropin), 0o644)
}
```

`internal/agent/run.go`:
```go
package agent

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	agentv1 "tgwebproxy/proto/agent/v1"
)

// Run keeps one session open to the panel, reconnecting with backoff.
func Run(ctx context.Context, cfg Config, h *Handler, log *slog.Logger) error {
	u, err := url.Parse(cfg.PanelURL)
	if err != nil {
		return err
	}
	target := u.Host
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if u.Scheme == "http" {
		creds = insecure.NewCredentials()
		if !strings.Contains(target, ":") {
			target += ":80"
		}
	} else if !strings.Contains(target, ":") {
		target += ":443"
	}
	backoff := time.Second
	for {
		err := session(ctx, target, creds, cfg, h, log)
		if ctx.Err() != nil {
			return nil
		}
		log.Warn("session ended", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func session(ctx context.Context, target string, creds credentials.TransportCredentials, cfg Config, h *Handler, log *slog.Logger) error {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+cfg.Token)
	stream, err := agentv1.NewAgentGatewayClient(conn).Session(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&agentv1.Envelope{Body: &agentv1.Envelope_Hello{Hello: &agentv1.Hello{AgentVersion: Version, TproxyVersion: cfg.TProxyVersion, Hostname: readHostname(cfg)}}}); err != nil {
		return err
	}
	log.Info("connected to panel")

	sendMu := make(chan struct{}, 1)
	send := func(env *agentv1.Envelope) error {
		sendMu <- struct{}{}
		defer func() { <-sendMu }()
		return stream.Send(env)
	}

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			_ = send(&agentv1.Envelope{Body: &agentv1.Envelope_Heartbeat{Heartbeat: &agentv1.Heartbeat{Health: h.Health(ctx)}}})
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	for {
		env, err := stream.Recv()
		if err != nil {
			return err
		}
		req := env.GetRequest()
		if req == nil {
			continue
		}
		go func(rid string, req *agentv1.Request) {
			if tl, ok := req.Body.(*agentv1.Request_TailLogs); ok {
				h.TailLogs(ctx, tl.TailLogs, func(c *agentv1.LogChunk) error {
					return send(&agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_LogChunk{LogChunk: c}})
				})
				return
			}
			resp := h.Handle(ctx, req)
			_ = send(&agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_Response{Response: resp}})
		}(env.RequestId, req)
	}
}

func readHostname(cfg Config) string {
	raw, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return ""
	}
	var c struct {
		PublicHostname string `json:"public_hostname"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.PublicHostname
}
```
Add `"encoding/json"` and `"os"` imports for `readHostname`.

`cmd/agent/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tgwebproxy/internal/agent"
	"tgwebproxy/internal/logging"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(agent.Version)
			return
		case "init-node":
			fs := flag.NewFlagSet("init-node", flag.ExitOnError)
			cfgPath := fs.String("config", "/etc/tproxy-server/config.json", "relay config path")
			envPath := fs.String("mtproxy-env", "/etc/mtproxy/mtproxy.env", "mtproxy env path")
			dropin := fs.String("dropin", "/etc/systemd/system/mtproxy.service.d/secrets.conf", "systemd drop-in path")
			maxProfiles := fs.Int("max-profiles", 128, "relay max_profiles")
			_ = fs.Parse(os.Args[2:])
			if err := agent.InitNode(*cfgPath, *envPath, *dropin, *maxProfiles); err != nil {
				fmt.Fprintln(os.Stderr, "init-node:", err)
				os.Exit(1)
			}
			fmt.Println("node initialised")
			return
		}
	}
	cfg, err := agent.LoadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	log := logging.New(os.Getenv("LOG_LEVEL"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, cfg, agent.NewHandler(cfg, agent.OSExec{}, log), log); err != nil {
		log.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 7: Run tests and cross-compile**

Run: `go mod tidy && go test ./internal/agent/ -race -v && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/agent`
Expected: all PASS; linux build succeeds.

---

### Task 9: Node service, presence hooks, install flow, nodes API, gRPC wiring

**Files:**
- Create: `internal/store/queries/nodes.sql`, `internal/store/queries/profiles.sql`
- Create: `internal/nodesvc/presence.go`, `internal/nodesvc/presence_test.go`
- Create: `internal/nodeinstall/script.sh.tmpl`, `internal/nodeinstall/fallback-site/index.html`, `internal/nodeinstall/fallback-site/styles.css`, `internal/nodeinstall/render.go`, `internal/nodeinstall/render_test.go`
- Create: `internal/api/nodes.go`, `internal/api/install.go`, `internal/api/sse.go`
- Test: `internal/api/nodes_test.go`, `internal/api/install_test.go`
- Modify: `internal/api/server.go` (Deps gains `Driver nodedriver.Driver`, `ApplyNow func(context.Context, uuid.UUID)`, `SiteProvider func(context.Context, uuid.UUID) (map[string][]byte, error)`; `mountProtected` mounts nodes), `internal/api/apitest/apitest.go` (default `Driver: nodedriver.NewMock()`, expose `Mock *nodedriver.Mock`), `cmd/panel/main.go`

**Interfaces:**
- Produces:
  - `nodesvc.NewPresence(st *store.Store, log) *Presence` implementing `gateway.Auth` and `gateway.Hooks`.
  - `nodeinstall.Params{PanelURL, InstallToken, Hostname, ACMEEmail, Secret, TProxyCommit, AgentSHA256 string; Site map[string][]byte}`; `nodeinstall.Render(Params) (string, error)`; `nodeinstall.FallbackSite() map[string][]byte`.
  - API routes (all under `/api/v1`, protected unless noted):
    - `GET /nodes` → `{items:[node], total}`; `POST /nodes` `{name, hostname, acme_email, public_ip?}` → 201 `{node, install_command}`; `GET /nodes/{id}` → node with `health`, `online`, `profile_count`, `capacity`; `PATCH /nodes/{id}` `{name?, public_ip?, max_profiles?, acme_email?}`; `DELETE /nodes/{id}`;
    - `GET /nodes/{id}/install-command` → `{command, expires_at}` (regenerates token);
    - `GET /nodes/{id}/health` (live if online else last), `GET /nodes/{id}/profiles` (`?live=1`), `GET /nodes/{id}/stats`, `GET /nodes/{id}/metrics` (text/plain), `GET /nodes/{id}/logs?services=a,b&lines=200&follow=1` (SSE), `POST /nodes/{id}/restart`, `POST /nodes/{id}/apply`.
    - Public: `GET /install/{token}.sh`, `POST /install/{token}/register`, `GET /install/agent/linux-amd64`.
  - Node JSON shape: `{id, name, hostname, public_ip, acme_email, status, online, tproxy_version, agent_version, max_profiles, profile_count, dirty, last_seen_at, last_apply_at, created_at, health}`.

- [ ] **Step 1: Queries**

`internal/store/queries/nodes.sql`:
```sql
-- name: CreateNode :one
INSERT INTO nodes (name, hostname, public_ip, acme_email, install_token_hash, install_token_expires)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: GetNodeByHostname :one
SELECT * FROM nodes WHERE hostname = $1;

-- name: GetNodeByInstallToken :one
SELECT * FROM nodes WHERE install_token_hash = $1 AND install_token_expires > now();

-- name: GetNodeByAgentToken :one
SELECT * FROM nodes WHERE agent_token_hash = $1;

-- name: ListNodes :many
SELECT * FROM nodes ORDER BY created_at;

-- name: UpdateNode :one
UPDATE nodes SET name = $2, public_ip = $3, max_profiles = $4, acme_email = $5 WHERE id = $1 RETURNING *;

-- name: SetNodeInstallToken :exec
UPDATE nodes SET install_token_hash = $2, install_token_expires = $3 WHERE id = $1;

-- name: RegisterNode :exec
UPDATE nodes SET agent_token_hash = $2, install_token_hash = NULL, install_token_expires = NULL,
  tproxy_version = $3, agent_version = $4, status = 'offline' WHERE id = $1;

-- name: SetNodeOnline :exec
UPDATE nodes SET status = 'online', last_seen_at = now(), tproxy_version = $2, agent_version = $3 WHERE id = $1;

-- name: SetNodeHeartbeat :exec
UPDATE nodes SET status = $2, last_seen_at = now(), last_health = $3 WHERE id = $1;

-- name: SetNodeStatus :exec
UPDATE nodes SET status = $2 WHERE id = $1;

-- name: MarkStaleNodesOffline :many
UPDATE nodes SET status = 'offline' WHERE status IN ('online','degraded') AND last_seen_at < $1 RETURNING id;

-- name: SetNodeDirty :exec
UPDATE nodes SET dirty = $2 WHERE id = $1;

-- name: ListDirtyNodes :many
SELECT * FROM nodes WHERE dirty = true AND status IN ('online','degraded');

-- name: SetNodeApplied :exec
UPDATE nodes SET dirty = false, last_apply_at = now() WHERE id = $1;

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;

-- name: CountNodesByStatus :many
SELECT status, count(*) AS n FROM nodes GROUP BY status;
```

`internal/store/queries/profiles.sql`:
```sql
-- name: CreateProfile :one
INSERT INTO profiles (node_id, access_key_id, name, secret_enc, backend, carrier_mode, limits)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: ListNodeProfiles :many
SELECT * FROM profiles WHERE node_id = $1 ORDER BY created_at;

-- name: CountNodeProfiles :one
SELECT count(*) FROM profiles WHERE node_id = $1;

-- name: ListProfilesByKey :many
SELECT * FROM profiles WHERE access_key_id = $1;

-- name: UpdateProfileSecret :exec
UPDATE profiles SET secret_enc = $2, sync_state = 'pending' WHERE id = $1;

-- name: UpdateProfileSettings :exec
UPDATE profiles SET carrier_mode = $2, limits = $3, sync_state = 'pending' WHERE id = $1;

-- name: SetNodeProfilesSync :exec
UPDATE profiles SET sync_state = $2 WHERE node_id = $1;

-- name: DeleteProfilesByKey :exec
DELETE FROM profiles WHERE access_key_id = $1;

-- name: DeleteProfile :exec
DELETE FROM profiles WHERE id = $1;
```

Run: `sqlc generate`.

- [ ] **Step 2: Presence (gateway Auth + Hooks)**

`internal/nodesvc/presence_test.go`:
```go
package nodesvc_test

import (
	"context"
	"testing"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	agentv1 "tgwebproxy/proto/agent/v1"
	"log/slog"
)

func TestPresenceLifecycle(t *testing.T) {
	st := store.OpenTest(t)
	ctx := context.Background()
	n, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "a", Hostname: "a.test"})
	tok := "node-token"
	_ = st.Q.RegisterNode(ctx, db.RegisterNodeParams{ID: n.ID, AgentTokenHash: ptr(crypto.HashToken(tok)), TproxyVersion: "v", AgentVersion: "a"})
	p := nodesvc.NewPresence(st, slog.New(slog.DiscardHandler))

	id, err := p.NodeByToken(ctx, tok)
	if err != nil || id != n.ID {
		t.Fatalf("auth: %v %v", id, err)
	}
	if _, err := p.NodeByToken(ctx, "wrong"); err == nil {
		t.Fatal("wrong token accepted")
	}
	p.OnHello(ctx, n.ID, &agentv1.Hello{AgentVersion: "0.1.0", TproxyVersion: "52a5feb"})
	got, _ := st.Q.GetNode(ctx, n.ID)
	if got.Status != db.NodeStatusOnline || got.AgentVersion != "0.1.0" {
		t.Fatalf("after hello: %+v", got)
	}
	p.OnHeartbeat(ctx, n.ID, &agentv1.HealthReport{RelayActive: true, MtproxyActive: false, Healthz: true})
	got, _ = st.Q.GetNode(ctx, n.ID)
	if got.Status != db.NodeStatusDegraded || got.LastHealth == nil {
		t.Fatalf("after degraded heartbeat: %+v", got)
	}
	p.OnDisconnect(ctx, n.ID)
	got, _ = st.Q.GetNode(ctx, n.ID)
	if got.Status != db.NodeStatusOffline {
		t.Fatalf("after disconnect: %s", got.Status)
	}
}

func ptr(s string) *string { return &s }
```

`internal/nodesvc/presence.go`:
```go
// Package nodesvc holds node-related services shared by API and workers.
package nodesvc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	agentv1 "tgwebproxy/proto/agent/v1"
)

type Presence struct {
	st  *store.Store
	log *slog.Logger
}

func NewPresence(st *store.Store, log *slog.Logger) *Presence { return &Presence{st: st, log: log} }

func (p *Presence) NodeByToken(ctx context.Context, token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, errors.New("missing token")
	}
	n, err := p.st.Q.GetNodeByAgentToken(ctx, ptr(crypto.HashToken(token)))
	if err != nil {
		return uuid.Nil, errors.New("unknown token")
	}
	return n.ID, nil
}

func (p *Presence) OnHello(ctx context.Context, id uuid.UUID, h *agentv1.Hello) {
	if err := p.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: id, TproxyVersion: h.GetTproxyVersion(), AgentVersion: h.GetAgentVersion()}); err != nil {
		p.log.Error("set online", "err", err)
	}
}

func (p *Presence) OnHeartbeat(ctx context.Context, id uuid.UUID, h *agentv1.HealthReport) {
	status := db.NodeStatusOnline
	if !(h.GetRelayActive() && h.GetMtproxyActive() && h.GetHealthz() && h.GetReadyz()) {
		status = db.NodeStatusDegraded
	}
	raw, _ := json.Marshal(nodedriver.HealthFromProto(h))
	if err := p.st.Q.SetNodeHeartbeat(ctx, db.SetNodeHeartbeatParams{ID: id, Status: status, LastHealth: raw}); err != nil {
		p.log.Error("heartbeat", "err", err)
	}
}

func (p *Presence) OnDisconnect(ctx context.Context, id uuid.UUID) {
	_ = p.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: id, Status: db.NodeStatusOffline})
}

func ptr(s string) *string { return &s }
```

Run: `go test ./internal/nodesvc/` → PASS.

- [ ] **Step 3: Install script template and renderer**

`internal/nodeinstall/render_test.go`:
```go
package nodeinstall

import (
	"strings"
	"testing"
)

func TestRenderContainsEssentials(t *testing.T) {
	out, err := Render(Params{PanelURL: "https://p.test", InstallToken: "tok", Hostname: "n.test", ACMEEmail: "a@b.co",
		Secret: "00000000000000000000000000000000", TProxyCommit: "abc", AgentSHA256: "deadbeef", Site: FallbackSite()})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#!/usr/bin/env bash", "https://p.test/api/v1/install/tok/register", "--hostname \"n.test\"",
		"git -C", "checkout -q \"abc\"", "init-node", "--max-profiles 128", "sha256sum -c", "tgwp-agent.service", "base64 -d | tar"} {
		if !strings.Contains(out, want) {
			t.Errorf("script missing %q", want)
		}
	}
}

func TestFallbackSiteHasIndex(t *testing.T) {
	s := FallbackSite()
	if _, ok := s["index.html"]; !ok || len(s["styles.css"]) == 0 {
		t.Fatal("fallback site incomplete")
	}
}

func TestRenderRejectsMissingIndex(t *testing.T) {
	if _, err := Render(Params{Site: map[string][]byte{"a.css": nil}}); err == nil {
		t.Fatal("expected error")
	}
}
```

`internal/nodeinstall/fallback-site/index.html`:
```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Northwind Cartography</title>
<link rel="stylesheet" href="/styles.css">
</head>
<body>
<header class="top"><span class="brand">Northwind Cartography</span><nav><a href="/">Maps</a><a href="/about">About</a></nav></header>
<main>
<section class="hero"><h1>Hand-drawn maps for small towns</h1><p>We draw walking maps, trail sheets and event plans for municipalities and festivals.</p></section>
<section class="grid"><article><h2>Trail sheets</h2><p>Foldable A4 sheets with elevation notes.</p></article><article><h2>Event plans</h2><p>Stage, stalls and exits on one page.</p></article><article><h2>Town walks</h2><p>Numbered routes with short stories.</p></article></section>
</main>
<footer>Northwind Cartography, est. 2014</footer>
</body>
</html>
```

`internal/nodeinstall/fallback-site/styles.css`:
```css
:root{--ink:#1f2933;--paper:#f7f4ee;--line:#d9d2c5}
*{box-sizing:border-box}body{margin:0;font-family:Georgia,serif;color:var(--ink);background:var(--paper)}
.top{display:flex;justify-content:space-between;align-items:center;padding:20px 32px;border-bottom:1px solid var(--line)}
.brand{font-weight:700;letter-spacing:.04em}nav a{margin-left:20px;color:inherit;text-decoration:none}
main{max-width:920px;margin:0 auto;padding:48px 24px}.hero h1{font-size:2.2rem;margin:0 0 12px}.hero p{max-width:60ch;line-height:1.6}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:20px;margin-top:40px}
article{border:1px solid var(--line);padding:20px;background:#fff}article h2{margin:0 0 8px;font-size:1.1rem}
footer{text-align:center;padding:32px;color:#6b7280;font-size:.9rem}
```

`internal/nodeinstall/script.sh.tmpl`:
```bash
#!/usr/bin/env bash
# Generated by the panel for {{.Hostname}}. Installs tproxy-server + MTProxy + Caddy + tgwp-agent.
set -euo pipefail
umask 022
PANEL_URL="{{.PanelURL}}"
INSTALL_TOKEN="{{.InstallToken}}"
NODE_HOSTNAME="{{.Hostname}}"
ACME_EMAIL="{{.ACMEEmail}}"
WEB_SECRET="{{.Secret}}"
TPROXY_COMMIT="{{.TProxyCommit}}"
AGENT_SHA256="{{.AgentSHA256}}"
SITE_TAR_B64="{{.SiteTarB64}}"

if [[ "${EUID}" -ne 0 ]]; then echo "run as root" >&2; exit 1; fi
if [[ "$(uname -m)" != "x86_64" ]]; then echo "x86_64 Linux is required" >&2; exit 1; fi
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends git ca-certificates curl tar

SRC=/opt/tproxy-server-src
if [[ ! -d "$SRC/.git" ]]; then git clone https://github.com/telegramdesktop/tproxy-server.git "$SRC"; fi
git -C "$SRC" fetch --depth 1 origin "$TPROXY_COMMIT" || git -C "$SRC" fetch origin
git -C "$SRC" checkout -q "$TPROXY_COMMIT"

SITE_DIR="$(mktemp -d /tmp/tgwp-site.XXXXXX)"
echo "$SITE_TAR_B64" | base64 -d | tar -xz -C "$SITE_DIR"

if [[ ! -x /usr/local/bin/tproxy-server ]]; then
  (cd "$SRC" && ./deploy/install.sh --hostname "$NODE_HOSTNAME" --email "$ACME_EMAIL" --secret "$WEB_SECRET" --site-dir "$SITE_DIR")
else
  echo "tproxy-server already installed, skipping official installer"
fi

curl -fsSL -o /usr/local/bin/tgwp-agent.new "$PANEL_URL/api/v1/install/agent/linux-amd64"
echo "$AGENT_SHA256  /usr/local/bin/tgwp-agent.new" | sha256sum -c -
chmod 0755 /usr/local/bin/tgwp-agent.new
mv -f /usr/local/bin/tgwp-agent.new /usr/local/bin/tgwp-agent

/usr/local/bin/tgwp-agent init-node --config /etc/tproxy-server/config.json --mtproxy-env /etc/mtproxy/mtproxy.env \
  --dropin /etc/systemd/system/mtproxy.service.d/secrets.conf --max-profiles 128
systemctl daemon-reload

REG="$(curl -fsS -X POST -H 'Content-Type: application/json' \
  -d "{\"hostname\":\"$NODE_HOSTNAME\",\"tproxy_version\":\"$TPROXY_COMMIT\",\"agent_version\":\"$(/usr/local/bin/tgwp-agent version)\"}" \
  "$PANEL_URL/api/v1/install/$INSTALL_TOKEN/register")"
NODE_TOKEN="$(printf '%s' "$REG" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
if [[ -z "$NODE_TOKEN" ]]; then echo "registration failed: $REG" >&2; exit 1; fi

install -d -m 0700 /etc/tgwp-agent /var/lib/tgwp-agent
umask 077
cat > /etc/tgwp-agent/agent.env <<EOF
TGWP_PANEL_URL=$PANEL_URL
TGWP_TOKEN=$NODE_TOKEN
TGWP_TPROXY_VERSION=$TPROXY_COMMIT
EOF
umask 022
cat > /etc/systemd/system/tgwp-agent.service <<'EOF'
[Unit]
Description=WEB proxy panel node agent
After=network-online.target tproxy-server.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/tgwp-agent/agent.env
ExecStart=/usr/local/bin/tgwp-agent
Restart=always
RestartSec=3s

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl restart mtproxy tproxy-server
systemctl enable --now tgwp-agent
echo "Node $NODE_HOSTNAME registered with the panel."
```

`internal/nodeinstall/render.go`:
```go
// Package nodeinstall renders the one-shot node installer script.
package nodeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"text/template"
	"time"
)

//go:embed script.sh.tmpl
var scriptTmpl string

//go:embed fallback-site/*
var fallback embed.FS

type Params struct {
	PanelURL, InstallToken, Hostname, ACMEEmail, Secret, TProxyCommit, AgentSHA256 string
	Site                                                                        map[string][]byte
}

var tmpl = template.Must(template.New("install").Parse(scriptTmpl))

func Render(p Params) (string, error) {
	if _, ok := p.Site["index.html"]; !ok {
		return "", errors.New("site must contain index.html")
	}
	tarB64, err := tarGzBase64(p.Site)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	err = tmpl.Execute(&buf, struct {
		Params
		SiteTarB64 string
	}{p, tarB64})
	return buf.String(), err
}

func FallbackSite() map[string][]byte {
	out := map[string][]byte{}
	for _, name := range []string{"index.html", "styles.css"} {
		b, _ := fallback.ReadFile("fallback-site/" + name)
		out[name] = b
	}
	return out
}

func tarGzBase64(files map[string][]byte) (string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), ModTime: time.Unix(0, 0)}); err != nil {
			return "", err
		}
		if _, err := tw.Write(files[n]); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
```

Run: `go test ./internal/nodeinstall/` → PASS.

- [ ] **Step 4: Write failing API tests**

`internal/api/nodes_test.go`:
```go
package api_test

import (
	"bufio"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/nodedriver"
)

type nodeResp struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Hostname     string    `json:"hostname"`
	Status       string    `json:"status"`
	Online       bool      `json:"online"`
	ProfileCount int       `json:"profile_count"`
	MaxProfiles  int       `json:"max_profiles"`
	Dirty        bool      `json:"dirty"`
}

func createNode(t *testing.T, c *apitest.Client, host string) (nodeResp, string) {
	t.Helper()
	var out struct {
		Node           nodeResp `json:"node"`
		InstallCommand string   `json:"install_command"`
	}
	resp := c.Post("/api/v1/nodes", map[string]string{"name": host, "hostname": host, "acme_email": "a@b.co"})
	if resp.StatusCode != 201 {
		t.Fatalf("create node %d", resp.StatusCode)
	}
	c.JSON(resp, &out)
	return out.Node, out.InstallCommand
}

func TestCreateNodeCreatesDefaultProfileAndInstallCommand(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, cmd := createNode(t, c, "n1.test")
	if n.Status != "pending" || n.ProfileCount != 1 || n.MaxProfiles != 128 {
		t.Fatalf("node %+v", n)
	}
	if !strings.HasPrefix(cmd, "curl -fsSL http://panel.test/api/v1/install/") || !strings.HasSuffix(cmd, ".sh | sudo bash") {
		t.Fatalf("command %q", cmd)
	}
}

func TestCreateNodeValidation(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	resp := c.Post("/api/v1/nodes", map[string]string{"name": "x", "hostname": "Bad Host", "acme_email": "nope"})
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
	createNode(t, c, "dup.test")
	resp = c.Post("/api/v1/nodes", map[string]string{"name": "x", "hostname": "dup.test", "acme_email": "a@b.co"})
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestNodeListGetPatchDelete(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Mock.SetOnline(n.ID, true)

	var list struct {
		Items []nodeResp `json:"items"`
		Total int        `json:"total"`
	}
	c.JSON(c.Get("/api/v1/nodes"), &list)
	if list.Total != 1 || !list.Items[0].Online {
		t.Fatalf("list %+v", list)
	}
	var got nodeResp
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"name": "renamed", "max_profiles": 64}), &got)
	if got.Name != "renamed" || got.MaxProfiles != 64 {
		t.Fatalf("patch %+v", got)
	}
	if resp := c.Delete("/api/v1/nodes/" + n.ID.String()); resp.StatusCode != 204 {
		t.Fatalf("delete %d", resp.StatusCode)
	}
	if resp := c.Get("/api/v1/nodes/" + n.ID.String()); resp.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestNodeHealthStatsMetricsRestart(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	if resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/health"); resp.StatusCode != 503 {
		t.Fatalf("offline health expected 503, got %d", resp.StatusCode)
	}
	h.Mock.SetOnline(n.ID, true)
	h.Mock.SetHealth(n.ID, nodedriver.HealthReport{RelayActive: true, Healthz: true, TProxyVersion: "52a5feb"})
	h.Mock.SetStats(n.ID, map[string]string{"active_connections": "7"})
	var health struct {
		RelayActive   bool   `json:"relay_active"`
		TProxyVersion string `json:"tproxy_version"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/health"), &health)
	if !health.RelayActive || health.TProxyVersion != "52a5feb" {
		t.Fatalf("health %+v", health)
	}
	var stats map[string]string
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/stats"), &stats)
	if stats["active_connections"] != "7" {
		t.Fatalf("stats %+v", stats)
	}
	resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/metrics")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/restart", nil); resp.StatusCode != 204 {
		t.Fatalf("restart %d", resp.StatusCode)
	}
	if h.Mock.Restarts(n.ID) != 1 {
		t.Fatal("driver restart not called")
	}
}

func TestNodeLogsSSE(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	h.Mock.SetOnline(n.ID, true)
	resp := c.Get("/api/v1/nodes/" + n.ID.String() + "/logs?services=tproxy-server,mtproxy&lines=10")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("sse %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	defer resp.Body.Close()
	var events int
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data:") {
			events++
		}
	}
	if events != 2 {
		t.Fatalf("events = %d", events)
	}
}

func TestViewerCannotCreateNode(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("v", "pass-123456", "viewer")
	c := h.Login("v", "pass-123456")
	if resp := c.Post("/api/v1/nodes", map[string]string{"name": "x", "hostname": "x.test", "acme_email": "a@b.co"}); resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
```

`internal/api/install_test.go`:
```go
package api_test

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tgwebproxy/internal/api/apitest"
)

func TestInstallScriptAndRegister(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_ = os.MkdirAll(filepath.Join(h.Deps.Cfg.DataDir, "agent"), 0o755)
	_ = os.WriteFile(filepath.Join(h.Deps.Cfg.DataDir, "agent", "tgwp-agent-linux-amd64"), []byte("binary"), 0o755)
	_ = os.WriteFile(filepath.Join(h.Deps.Cfg.DataDir, "agent", "tgwp-agent-linux-amd64.sha256"), []byte("abc123\n"), 0o644)

	n, cmd := createNode(t, c, "n1.test")
	token := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(cmd)[1]

	anon := h.Anonymous()
	resp := anon.Get("/api/v1/install/" + token + ".sh")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "NODE_HOSTNAME=\"n1.test\"") || !strings.Contains(string(body), "AGENT_SHA256=\"abc123\"") {
		t.Fatalf("script %d: %.200s", resp.StatusCode, body)
	}
	if resp := anon.Get("/api/v1/install/agent/linux-amd64"); resp.StatusCode != 200 {
		t.Fatalf("agent download %d", resp.StatusCode)
	}

	var reg struct {
		NodeID string `json:"node_id"`
		Token  string `json:"token"`
	}
	resp = anon.Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test", "tproxy_version": "52a5feb", "agent_version": "0.1.0"})
	if resp.StatusCode != 200 {
		t.Fatalf("register %d", resp.StatusCode)
	}
	anon.JSON(resp, &reg)
	if reg.NodeID != n.ID.String() || len(reg.Token) < 40 {
		t.Fatalf("reg %+v", reg)
	}
	// token is single-use
	if resp := anon.Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test"}); resp.StatusCode != 404 {
		t.Fatalf("second register expected 404, got %d", resp.StatusCode)
	}
	// presence auth accepts the issued token
	id, err := h.Presence.NodeByToken(t.Context(), reg.Token)
	if err != nil || id != n.ID {
		t.Fatalf("presence auth %v %v", id, err)
	}
	var got nodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.Status != "offline" {
		t.Fatalf("status after register = %s", got.Status)
	}
}

func TestInstallUnknownToken(t *testing.T) {
	h := apitest.New(t)
	if resp := h.Anonymous().Get("/api/v1/install/nope.sh"); resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
```

Update `apitest.Harness`: add fields `Mock *nodedriver.Mock` and `Presence *nodesvc.Presence`; in `New`, create `mock := nodedriver.NewMock()`, set `deps.Driver = mock`, `deps.Presence = nodesvc.NewPresence(st, log)`.

- [ ] **Step 5: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'Node|Install' 2>&1 | head`
Expected: compile errors.

- [ ] **Step 6: Extend Deps and implement nodes.go**

In `internal/api/server.go` add to `Deps` and `Server`:
```go
Driver       nodedriver.Driver
Presence     *nodesvc.Presence
ApplyNow     func(ctx context.Context, nodeID uuid.UUID) // nil until Part C; handlers treat nil as "mark dirty only"
SiteProvider func(ctx context.Context, nodeID uuid.UUID) (map[string][]byte, error) // nil → fallback site
```
and in `Handler()`:
```go
r.Get("/install/{token}.sh", s.handleInstallScript)
r.Post("/install/{token}/register", s.handleInstallRegister)
r.Get("/install/agent/{platform}", s.handleAgentDownload)
```
inside the `/api/v1` route (public, before the protected group). `mountProtected`:
```go
func (s *Server) mountProtected(r chi.Router) {
	r.Get("/nodes", s.handleListNodes)
	r.With(RequireRole(writers...)).Post("/nodes", s.handleCreateNode)
	r.Route("/nodes/{id}", func(r chi.Router) {
		r.Get("/", s.handleGetNode)
		r.With(RequireRole(writers...)).Patch("/", s.handlePatchNode)
		r.With(RequireRole(writers...)).Delete("/", s.handleDeleteNode)
		r.With(RequireRole(writers...)).Get("/install-command", s.handleInstallCommand)
		r.Get("/health", s.handleNodeHealth)
		r.Get("/profiles", s.handleNodeProfiles)
		r.Get("/stats", s.handleNodeStats)
		r.Get("/metrics", s.handleNodeMetrics)
		r.Get("/logs", s.handleNodeLogs)
		r.With(RequireRole(writers...)).Post("/restart", s.handleNodeRestart)
		r.With(RequireRole(writers...)).Post("/apply", s.handleNodeApply)
	})
}
```

`internal/api/nodes.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

const installTokenTTL = 24 * time.Hour

type nodeJSON struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	Hostname      string          `json:"hostname"`
	PublicIP      string          `json:"public_ip"`
	ACMEEmail     string          `json:"acme_email"`
	Status        string          `json:"status"`
	Online        bool            `json:"online"`
	TProxyVersion string          `json:"tproxy_version"`
	AgentVersion  string          `json:"agent_version"`
	MaxProfiles   int             `json:"max_profiles"`
	ProfileCount  int             `json:"profile_count"`
	Dirty         bool            `json:"dirty"`
	LastSeenAt    *time.Time      `json:"last_seen_at"`
	LastApplyAt   *time.Time      `json:"last_apply_at"`
	CreatedAt     time.Time       `json:"created_at"`
	Health        json.RawMessage `json:"health,omitempty"`
}

func (s *Server) nodeJSON(ctx context.Context, n db.Node) nodeJSON {
	count, _ := s.store.Q.CountNodeProfiles(ctx, n.ID)
	out := nodeJSON{
		ID: n.ID, Name: n.Name, Hostname: n.Hostname, PublicIP: n.PublicIp, ACMEEmail: n.AcmeEmail, Status: string(n.Status),
		Online: s.driver != nil && s.driver.Online(n.ID), TProxyVersion: n.TproxyVersion, AgentVersion: n.AgentVersion,
		MaxProfiles: int(n.MaxProfiles), ProfileCount: int(count), Dirty: n.Dirty, LastSeenAt: n.LastSeenAt, LastApplyAt: n.LastApplyAt,
		CreatedAt: n.CreatedAt,
	}
	if len(n.LastHealth) > 0 {
		out.Health = n.LastHealth
	}
	return out
}

func (s *Server) loadNode(w http.ResponseWriter, r *http.Request) (db.Node, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.Node{}, false
	}
	n, err := s.store.Q.GetNode(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.Node{}, false
	}
	return n, true
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListNodes(r.Context())
	if err != nil {
		internal(w)
		return
	}
	items := make([]nodeJSON, 0, len(rows))
	for _, n := range rows {
		items = append(items, s.nodeJSON(r.Context(), n))
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

type createNodeReq struct {
	Name      string `json:"name"`
	Hostname  string `json:"hostname"`
	ACMEEmail string `json:"acme_email"`
	PublicIP  string `json:"public_ip"`
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req createNodeReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	req.Hostname = strings.ToLower(strings.TrimSpace(req.Hostname))
	fields := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		fields["name"] = "required"
	}
	if err := domain.ValidateHostname(req.Hostname); err != nil {
		fields["hostname"] = err.Error()
	}
	if _, err := mail.ParseAddress(req.ACMEEmail); err != nil {
		fields["acme_email"] = "valid email required for Let's Encrypt"
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	if _, err := s.store.Q.GetNodeByHostname(r.Context(), req.Hostname); err == nil {
		conflict(w, "hostname already exists")
		return
	}
	token, err := crypto.NewToken(24)
	if err != nil {
		internal(w)
		return
	}
	hash := crypto.HashToken(token)
	exp := time.Now().Add(installTokenTTL)
	var node db.Node
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		var err error
		node, err = q.CreateNode(r.Context(), db.CreateNodeParams{Name: req.Name, Hostname: req.Hostname, PublicIp: req.PublicIP, AcmeEmail: req.ACMEEmail, InstallTokenHash: &hash, InstallTokenExpires: &exp})
		if err != nil {
			return err
		}
		secret, err := crypto.NewSecretHex()
		if err != nil {
			return err
		}
		enc, err := s.box.EncryptString(secret)
		if err != nil {
			return err
		}
		_, err = q.CreateProfile(r.Context(), db.CreateProfileParams{NodeID: node.ID, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")})
		return err
	})
	if err != nil {
		s.log.Error("create node", "err", err)
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.create", "node", node.ID.String(), map[string]any{"hostname": node.Hostname})
	writeJSON(w, 201, map[string]any{"node": s.nodeJSON(r.Context(), node), "install_command": s.installCommand(token), "expires_at": exp})
}

func (s *Server) installCommand(token string) string {
	return "curl -fsSL " + s.cfg.PublicURL + "/api/v1/install/" + token + ".sh | sudo bash"
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, s.nodeJSON(r.Context(), n))
}

type patchNodeReq struct {
	Name        *string `json:"name"`
	PublicIP    *string `json:"public_ip"`
	MaxProfiles *int    `json:"max_profiles"`
	ACMEEmail   *string `json:"acme_email"`
}

func (s *Server) handlePatchNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	var req patchNodeReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if req.Name != nil {
		n.Name = *req.Name
	}
	if req.PublicIP != nil {
		n.PublicIp = *req.PublicIP
	}
	if req.ACMEEmail != nil {
		n.AcmeEmail = *req.ACMEEmail
	}
	if req.MaxProfiles != nil {
		if *req.MaxProfiles < 1 || *req.MaxProfiles > 1024 {
			validation(w, map[string]string{"max_profiles": "1..1024"})
			return
		}
		n.MaxProfiles = int32(*req.MaxProfiles)
	}
	updated, err := s.store.Q.UpdateNode(r.Context(), db.UpdateNodeParams{ID: n.ID, Name: n.Name, PublicIp: n.PublicIp, MaxProfiles: n.MaxProfiles, AcmeEmail: n.AcmeEmail})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.update", "node", n.ID.String(), nil)
	writeJSON(w, 200, s.nodeJSON(r.Context(), updated))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if err := s.store.Q.DeleteNode(r.Context(), n.ID); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.delete", "node", n.ID.String(), map[string]any{"hostname": n.Hostname})
	w.WriteHeader(204)
}

func (s *Server) handleInstallCommand(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	token, err := crypto.NewToken(24)
	if err != nil {
		internal(w)
		return
	}
	hash := crypto.HashToken(token)
	exp := time.Now().Add(installTokenTTL)
	if err := s.store.Q.SetNodeInstallToken(r.Context(), db.SetNodeInstallTokenParams{ID: n.ID, InstallTokenHash: &hash, InstallTokenExpires: &exp}); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.install_token", "node", n.ID.String(), nil)
	writeJSON(w, 200, map[string]any{"command": s.installCommand(token), "expires_at": exp})
}

func (s *Server) driverErr(w http.ResponseWriter, err error) {
	if errors.Is(err, nodedriver.ErrOffline) {
		writeError(w, 503, "node_offline", "node is offline", nil)
		return
	}
	writeError(w, 502, "node_error", err.Error(), nil)
}

func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	h, err := s.driver.Health(r.Context(), n.ID)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	writeJSON(w, 200, healthJSON(h))
}

func healthJSON(h nodedriver.HealthReport) map[string]any {
	return map[string]any{
		"relay_active": h.RelayActive, "mtproxy_active": h.MTProxyActive, "caddy_active": h.CaddyActive,
		"healthz": h.Healthz, "readyz": h.Readyz, "tproxy_version": h.TProxyVersion, "agent_version": h.AgentVersion,
		"uptime_seconds": h.UptimeSeconds, "cpu_percent": h.CPUPercent, "mem_used_percent": h.MemUsedPercent,
		"disk_used_percent": h.DiskUsedPercent, "profile_count": h.ProfileCount,
	}
}

func (s *Server) handleNodeProfiles(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("live") == "1" {
		ps, err := s.driver.GetProfiles(r.Context(), n.ID)
		if err != nil {
			s.driverErr(w, err)
			return
		}
		items := make([]map[string]any, 0, len(ps))
		for _, p := range ps {
			items = append(items, map[string]any{"name": p.Name, "backend": p.Backend, "carrier_mode": p.CarrierMode, "limits": p.Limits})
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
		return
	}
	rows, err := s.store.Q.ListNodeProfiles(r.Context(), n.ID)
	if err != nil {
		internal(w)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		items = append(items, map[string]any{"id": p.ID, "name": p.Name, "access_key_id": p.AccessKeyID, "backend": p.Backend,
			"carrier_mode": p.CarrierMode, "limits": json.RawMessage(p.Limits), "sync_state": p.SyncState, "created_at": p.CreatedAt})
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleNodeStats(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	st, err := s.driver.Stats(r.Context(), n.ID)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) handleNodeMetrics(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	text, err := s.driver.Metrics(r.Context(), n.ID)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(text))
}

func (s *Server) handleNodeRestart(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	if err := s.driver.RestartRelay(r.Context(), n.ID); err != nil {
		s.driverErr(w, err)
		return
	}
	s.Audit(r.Context(), "node.restart_relay", "node", n.ID.String(), nil)
	w.WriteHeader(204)
}

func (s *Server) handleNodeApply(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	_ = s.store.Q.SetNodeDirty(r.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: true})
	if s.applyNow != nil {
		s.applyNow(context.WithoutCancel(r.Context()), n.ID)
	}
	s.Audit(r.Context(), "node.apply", "node", n.ID.String(), nil)
	writeJSON(w, 202, map[string]any{"queued": true})
}

func (s *Server) handleNodeLogs(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	services := strings.Split(r.URL.Query().Get("services"), ",")
	if services[0] == "" {
		services = []string{"tproxy-server"}
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	follow := r.URL.Query().Get("follow") == "1"
	ch, err := s.driver.TailLogs(r.Context(), n.ID, services, lines, follow)
	if err != nil {
		s.driverErr(w, err)
		return
	}
	sse := newSSE(w)
	for line := range ch {
		if err := sse.send("log", map[string]any{"service": line.Service, "line": line.Line, "time": line.Time}); err != nil {
			return
		}
	}
	_ = sse.send("end", map[string]any{})
}
```
Add fields `driver nodedriver.Driver`, `presence *nodesvc.Presence`, `applyNow func(context.Context, uuid.UUID)`, `siteProvider func(context.Context, uuid.UUID) (map[string][]byte, error)` to `Server` and set them in `New`.

`internal/api/sse.go`:
```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f, _ := w.(http.Flusher)
	if f != nil {
		f.Flush()
	}
	return &sseWriter{w: w, f: f}
}

func (s *sseWriter) send(event string, v any) error {
	b, _ := json.Marshal(v)
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	if s.f != nil {
		s.f.Flush()
	}
	return nil
}
```

- [ ] **Step 7: Install handlers**

`internal/api/install.go`:
```go
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodeinstall"
	"tgwebproxy/internal/store/db"
)

func (s *Server) agentSHA256() string {
	b, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "agent", "tgwp-agent-linux-amd64.sha256"))
	if err != nil {
		return ""
	}
	return strings.Fields(string(b))[0]
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	n, err := s.store.Q.GetNodeByInstallToken(r.Context(), ptr(crypto.HashToken(token)))
	if err != nil {
		notFound(w)
		return
	}
	profiles, err := s.store.Q.ListNodeProfiles(r.Context(), n.ID)
	if err != nil || len(profiles) == 0 {
		internal(w)
		return
	}
	secret, err := s.box.DecryptString(profiles[0].SecretEnc)
	if err != nil {
		internal(w)
		return
	}
	site := nodeinstall.FallbackSite()
	if s.siteProvider != nil {
		if custom, err := s.siteProvider(r.Context(), n.ID); err == nil && custom != nil {
			site = custom
		}
	}
	script, err := nodeinstall.Render(nodeinstall.Params{
		PanelURL: s.cfg.PublicURL, InstallToken: token, Hostname: n.Hostname, ACMEEmail: n.AcmeEmail, Secret: secret,
		TProxyCommit: s.cfg.TProxyCommit, AgentSHA256: s.agentSHA256(), Site: site,
	})
	if err != nil {
		internal(w)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}

type registerReq struct {
	Hostname      string `json:"hostname"`
	TProxyVersion string `json:"tproxy_version"`
	AgentVersion  string `json:"agent_version"`
}

func (s *Server) handleInstallRegister(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	n, err := s.store.Q.GetNodeByInstallToken(r.Context(), ptr(crypto.HashToken(token)))
	if err != nil {
		notFound(w)
		return
	}
	var req registerReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	nodeToken, err := crypto.NewToken(32)
	if err != nil {
		internal(w)
		return
	}
	if err := s.store.Q.RegisterNode(r.Context(), db.RegisterNodeParams{ID: n.ID, AgentTokenHash: ptr(crypto.HashToken(nodeToken)), TproxyVersion: req.TProxyVersion, AgentVersion: req.AgentVersion}); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.register", "node", n.ID.String(), map[string]any{"hostname": n.Hostname})
	writeJSON(w, 200, map[string]any{"node_id": n.ID, "token": nodeToken, "panel_url": s.cfg.PublicURL})
}

func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	if platform != "linux-amd64" {
		notFound(w)
		return
	}
	path := filepath.Join(s.cfg.DataDir, "agent", "tgwp-agent-"+platform)
	if _, err := os.Stat(path); err != nil {
		writeError(w, 404, "agent_missing", "agent binary not built; run make agent-linux", nil)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

func ptr(s string) *string { return &s }
```

- [ ] **Step 8: Wire gRPC + driver in panel main**

In `cmd/panel/main.go` `serve()`:
```go
reg := gateway.NewRegistry()
presence := nodesvc.NewPresence(st, log)
var driver nodedriver.Driver
if cfg.NodeDriver == "mock" {
	driver = nodedriver.NewMock()
} else {
	driver = nodedriver.NewGateway(reg, 15*time.Second)
}
grpcSrv := grpc.NewServer()
agentv1.RegisterAgentGatewayServer(grpcSrv, gateway.NewServer(reg, presence, presence, log))
srv := api.New(api.Deps{Store: st, Box: box, Signer: crypto.NewSigner(cfg.SessionSecret), Log: log, Cfg: cfg, Driver: driver, Presence: presence})
httpHandler := srv.Handler()
mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		grpcSrv.ServeHTTP(w, r)
		return
	}
	httpHandler.ServeHTTP(w, r)
})
httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: h2c.NewHandler(mux, &http2.Server{}), ReadHeaderTimeout: 10 * time.Second}
```
Imports: `golang.org/x/net/http2`, `golang.org/x/net/http2/h2c`, `google.golang.org/grpc`, `tgwebproxy/internal/gateway`, `tgwebproxy/internal/nodesvc`, `tgwebproxy/internal/nodedriver`, `agentv1 "tgwebproxy/proto/agent/v1"`.

- [ ] **Step 9: Run everything**

Run: `go mod tidy && sqlc generate && go build ./... && go test ./... -race && golangci-lint run ./...`
Expected: all PASS, lint clean.

- [ ] **Step 10: Manual end-to-end with a local agent**

```bash
make agent-linux   # produces data/agent/... (linux binary for download; local agent runs from source)
export NODE_DRIVER=gateway PANEL_PUBLIC_URL=http://localhost:8080 && go run ./cmd/panel serve &
# create node via curl (login first as in Part A smoke), then register manually:
TOKEN=$(curl -s -b c.txt -H 'Content-Type: application/json' -H "X-CSRF-Token: $(grep tgwp_csrf c.txt | awk '{print $7}')" \
  -d '{"name":"local","hostname":"local.test","acme_email":"a@b.co"}' localhost:8080/api/v1/nodes | sed -n 's/.*install\/\([^/]*\)\.sh.*/\1/p')
NODE_TOKEN=$(curl -s -X POST -H 'Content-Type: application/json' -d '{"hostname":"local.test","tproxy_version":"dev","agent_version":"dev"}' localhost:8080/api/v1/install/$TOKEN/register | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
mkdir -p /tmp/fakenode && echo '{"profiles":[{"name":"default","secret":"00000000000000000000000000000000","backend":"127.0.0.1:2398"}]}' > /tmp/fakenode/profiles.json
TGWP_PANEL_URL=http://localhost:8080 TGWP_TOKEN=$NODE_TOKEN TGWP_STATE_DIR=/tmp/fakenode TGWP_PROFILES=/tmp/fakenode/profiles.json \
  TGWP_CONFIG=/tmp/fakenode/config.json TGWP_MTPROXY_ENV=/tmp/fakenode/mtproxy.env TGWP_SITE_DIR=/tmp/fakenode/site go run ./cmd/agent
```
Expected: panel logs `agent connected`; `GET /api/v1/nodes` shows `online: true`; `GET /nodes/{id}/health` returns a report (units inactive on macOS, that is fine).
