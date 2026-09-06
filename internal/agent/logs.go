package agent

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

var allowedUnits = map[string]bool{"tproxy-server": true, "mtproxy": true, "telemt": true, "caddy": true, "tgwp-agent": true}

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
	defer func() { _ = rd.Close() }()
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
