package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"strings"
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
