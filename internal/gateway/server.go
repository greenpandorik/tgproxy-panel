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
			// Nothing in this switch may block for long: it is the session's
			// only Recv pump, so a slow dispatch delays every later envelope -
			// heartbeats included - and a stalled heartbeat is what the stats
			// worker reads as "node offline". LogChunks therefore drop on a full
			// channel instead of waiting; only Response, which is one-per-call,
			// gets the deliverTimeout grace.
			switch b := env.Body.(type) {
			case *agentv1.Envelope_Heartbeat:
				s.hooks.OnHeartbeat(ctx, nodeID, b.Heartbeat.GetHealth())
			case *agentv1.Envelope_Response:
				c.deliver(env, s.log)
			case *agentv1.Envelope_LogChunk:
				c.deliverNoWait(env, s.log)
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
