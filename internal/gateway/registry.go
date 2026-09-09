// Package gateway accepts agent sessions and routes panel requests to them.
package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

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

var deliverTimeout atomic.Int64

func init() { deliverTimeout.Store(int64(2 * time.Second)) }

func (c *conn) deliver(env *agentv1.Envelope, log *slog.Logger) {
	ch := c.pendingChan(env.RequestId)
	if ch == nil {
		return
	}
	select {
	case ch <- env:
		return
	default:
	}
	wait := time.Duration(deliverTimeout.Load())
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case ch <- env:
	case <-c.closed:
	case <-t.C:
		c.warnDropped(log, env, wait)
	}
}

func (c *conn) deliverNoWait(env *agentv1.Envelope, log *slog.Logger) {
	ch := c.pendingChan(env.RequestId)
	if ch == nil {
		return
	}
	select {
	case ch <- env:
	default:
		c.warnDropped(log, env, 0)
	}
}

func (c *conn) pendingChan(requestID string) chan *agentv1.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending[requestID]
}

func (c *conn) warnDropped(log *slog.Logger, env *agentv1.Envelope, waited time.Duration) {
	if log == nil {
		return
	}
	log.Warn("dropped agent envelope: consumer not reading",
		"node_id", c.nodeID, "request_id", env.RequestId, "waited", waited)
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
