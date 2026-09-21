package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	agentv1 "tgwebproxy/proto/agent/v1"
)

const (
	// maxGRPCMessageBytes matches the panel's server-side limit (cmd/panel/main.go).
	maxGRPCMessageBytes = 16 << 20
	healthySession      = 60 * time.Second
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
		started := time.Now()
		err := session(ctx, target, creds, cfg, h, log)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) >= healthySession {
			backoff = time.Second
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
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxGRPCMessageBytes),
			grpc.MaxCallSendMsgSize(maxGRPCMessageBytes),
		))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+cfg.Token)
	stream, err := agentv1.NewAgentGatewayClient(conn).Session(ctx)
	if err != nil {
		return err
	}
	hello := &agentv1.Hello{AgentVersion: Version, TproxyVersion: h.Health(ctx).GetTproxyVersion(), Hostname: readHostname(cfg)}
	if err := stream.Send(&agentv1.Envelope{Body: &agentv1.Envelope_Hello{Hello: hello}}); err != nil {
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

	tails := &tailRegistry{cancels: map[string]context.CancelFunc{}}
	defer tails.cancelAll()

	for {
		env, err := stream.Recv()
		if err != nil {
			return err
		}
		req := env.GetRequest()
		if req == nil {
			continue
		}
		if c, ok := req.Body.(*agentv1.Request_Cancel); ok {
			tails.cancel(c.Cancel.GetRequestId())
			continue
		}
		go func(rid string, req *agentv1.Request) {
			if tl, ok := req.Body.(*agentv1.Request_TailLogs); ok {
				tailCtx, release, ok := tails.start(ctx, rid)
				if !ok {
					_ = send(&agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_LogChunk{
						LogChunk: &agentv1.LogChunk{Done: true, Error: "too many log subscriptions on this node"},
					}})
					return
				}
				defer release()
				h.TailLogs(tailCtx, tl.TailLogs, func(c *agentv1.LogChunk) error {
					return send(&agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_LogChunk{LogChunk: c}})
				})
				return
			}
			resp := h.Handle(ctx, req)
			_ = send(&agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_Response{Response: resp}})
		}(env.RequestId, req)
	}
}

// maxTailSubscriptions caps the journalctl processes one panel session can leave running here.
const maxTailSubscriptions = 8

// tailRegistry gives every log subscription a context of its own, so the panel can end one
// without ending the session, and so a session that drops takes all of them with it.
type tailRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func (t *tailRegistry) start(parent context.Context, rid string) (context.Context, func(), bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.cancels) >= maxTailSubscriptions {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	t.cancels[rid] = cancel
	return ctx, func() {
		t.mu.Lock()
		delete(t.cancels, rid)
		t.mu.Unlock()
		cancel()
	}, true
}

func (t *tailRegistry) cancel(rid string) {
	t.mu.Lock()
	cancel := t.cancels[rid]
	delete(t.cancels, rid)
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (t *tailRegistry) cancelAll() {
	t.mu.Lock()
	cancels := t.cancels
	t.cancels = map[string]context.CancelFunc{}
	t.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func readHostname(cfg Config) string {
	if cfg.Engine == EngineTelemt {
		name, _ := os.Hostname()
		return name
	}
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
