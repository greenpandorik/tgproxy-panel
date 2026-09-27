package nodedriver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	agentv1 "tgwebproxy/proto/agent/v1"
)

var ErrUnsupportedReliability = errors.New("agent does not support reliability policies; upgrade the agent first")

// Optional interface: old/mock drivers cannot silently accept unsupported policies.
type ReliabilityDriver interface {
	Reliability(context.Context, uuid.UUID, json.RawMessage) (json.RawMessage, error)
}

func (g *Gateway) Reliability(ctx context.Context, id uuid.UUID, policy json.RawMessage) (json.RawMessage, error) {
	r, e := g.call(ctx, id, &agentv1.Request{Body: &agentv1.Request_Reliability{Reliability: &agentv1.ReliabilityRequest{PolicyJson: policy}}}, 90*time.Second)
	if e != nil {
		return nil, e
	}
	if r.GetReliability() == nil {
		return nil, ErrUnsupportedReliability
	}
	return r.GetReliability().Json, nil
}
