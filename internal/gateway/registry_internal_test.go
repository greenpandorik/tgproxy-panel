package gateway

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "tgwebproxy/proto/agent/v1"
)

func env(rid string) *agentv1.Envelope {
	return &agentv1.Envelope{RequestId: rid, Body: &agentv1.Envelope_LogChunk{LogChunk: &agentv1.LogChunk{}}}
}

func TestDeliverBlocksOnFullChannel(t *testing.T) {
	c := newConn(uuid.New())
	ch := c.register("r1")
	for i := 0; i < cap(ch); i++ {
		ch <- env("r1")
	}

	done := make(chan struct{})
	go func() {
		c.deliver(env("r1"), slog.New(slog.DiscardHandler))
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("deliver returned immediately on a full channel; it must wait")
	case <-time.After(50 * time.Millisecond):
	}

	<-ch // make room; deliver must now hand the envelope over rather than drop it
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deliver did not proceed after the channel drained")
	}
	if len(ch) != cap(ch) {
		t.Fatalf("envelope not delivered: %d queued, want %d", len(ch), cap(ch))
	}
}

func TestDeliverDropsWithWarningAfterTimeout(t *testing.T) {
	old := deliverTimeout.Load()
	deliverTimeout.Store(int64(20 * time.Millisecond))
	t.Cleanup(func() { deliverTimeout.Store(old) })

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := newConn(uuid.New())
	ch := c.register("r1")
	for i := 0; i < cap(ch); i++ {
		ch <- env("r1")
	}

	start := time.Now()
	c.deliver(env("r1"), log)
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("returned after %v, want at least the deliver timeout", elapsed)
	}
	if out := buf.String(); !strings.Contains(out, "dropped agent envelope") {
		t.Fatalf("no warning logged on drop: %q", out)
	}
}

// A closed connection must not hold the Recv pump for the full timeout.
func TestDeliverReturnsWhenConnClosed(t *testing.T) {
	c := newConn(uuid.New())
	ch := c.register("r1")
	for i := 0; i < cap(ch); i++ {
		ch <- env("r1")
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.close()
	}()
	start := time.Now()
	c.deliver(env("r1"), slog.New(slog.DiscardHandler))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deliver waited %v after close, want an immediate return", elapsed)
	}
}

// Unknown request ids (a reply that arrives after the caller gave up) are dropped without waiting at all.
func TestDeliverUnknownRequestIsImmediate(t *testing.T) {
	c := newConn(uuid.New())
	start := time.Now()
	c.deliver(env("nobody-waiting"), slog.New(slog.DiscardHandler))
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("waited %v for an unregistered request id", elapsed)
	}
}

func TestDeliverTimeoutDefault(t *testing.T) {
	if got := time.Duration(deliverTimeout.Load()); got != 2*time.Second {
		t.Fatalf("deliverTimeout = %v, want 2s", got)
	}
}

func TestDeliverNoWaitDropsImmediately(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := newConn(uuid.New())
	ch := c.register("r1")
	for i := 0; i < cap(ch); i++ {
		ch <- env("r1")
	}

	start := time.Now()
	c.deliverNoWait(env("r1"), log)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("deliverNoWait waited %v on a full channel; it must drop immediately", elapsed)
	}
	if out := buf.String(); !strings.Contains(out, "dropped agent envelope") {
		t.Fatalf("no warning logged on drop: %q", out)
	}
	if len(ch) != cap(ch) {
		t.Fatalf("channel len = %d, want it left untouched at %d", len(ch), cap(ch))
	}

	// With room available it still delivers.
	<-ch
	c.deliverNoWait(env("r1"), log)
	if len(ch) != cap(ch) {
		t.Fatalf("envelope not delivered into the free slot: %d queued", len(ch))
	}
}
