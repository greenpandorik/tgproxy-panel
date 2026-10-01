package worker

import (
	"time"

	"github.com/google/uuid"
)

// keyReading is what one node reported about one access key in a sweep.
type keyReading struct {
	KeyID       uuid.UUID
	Shared      bool
	Connections int
	IPCount     int
	// IPs is nil when the agent does not list addresses.
	IPs []string
}

// nodeReading is one node's part of a sweep.
type nodeReading struct {
	NodeID      uuid.UUID
	Connections int
	Keys        []keyReading
	// Sessions is set on tproxy nodes only.
	Sessions int
}

type keyPresence struct {
	Connections int
	Devices     int
	Devices15m  int
}

type presence struct {
	People      int
	People15m   int
	Connections int
	// Keys has every key that was online at some point in the window.
	Keys map[uuid.UUID]keyPresence
}

// addrTally counts distinct addresses; ones a node counted but did not list count as distinct.
type addrTally struct {
	named   map[string]struct{}
	unnamed int
}

func (a *addrTally) add(k keyReading) {
	if a.named == nil {
		a.named = map[string]struct{}{}
	}
	for _, ip := range k.IPs {
		a.named[ip] = struct{}{}
	}
	a.unnamed += max(0, k.IPCount-len(k.IPs))
}

func (a *addrTally) devices() int { return max(1, len(a.named)+a.unnamed) }

// nodePeople is the head count of one node.
func nodePeople(n nodeReading) int {
	people := n.Sessions
	for _, k := range n.Keys {
		switch {
		case k.Connections <= 0:
		case k.Shared:
			var t addrTally
			t.add(k)
			people += t.devices()
		default:
			people++
		}
	}
	return people
}

// computePresence counts the fleet over a window of sweeps, oldest first, the last one being now.
func computePresence(window [][]nodeReading) presence {
	out := presence{Keys: map[uuid.UUID]keyPresence{}}
	type sweepKey struct {
		shared      bool
		connections int
		addrs       addrTally
	}
	type windowKey struct {
		shared bool
		named  map[string]struct{}
		peak   int
	}
	seen := map[uuid.UUID]*windowKey{}
	sessions := map[uuid.UUID]int{}
	last := len(window) - 1
	for i, sweep := range window {
		keys := map[uuid.UUID]*sweepKey{}
		for _, n := range sweep {
			sessions[n.NodeID] = max(sessions[n.NodeID], n.Sessions)
			if i == last {
				out.Connections += n.Connections
				out.People += n.Sessions
			}
			for _, k := range n.Keys {
				if k.Connections <= 0 {
					continue
				}
				sk := keys[k.KeyID]
				if sk == nil {
					sk = &sweepKey{}
					keys[k.KeyID] = sk
				}
				sk.shared = k.Shared
				sk.connections += k.Connections
				sk.addrs.add(k)
			}
		}
		for id, sk := range keys {
			wk := seen[id]
			if wk == nil {
				wk = &windowKey{named: map[string]struct{}{}}
				seen[id] = wk
			}
			wk.shared = sk.shared
			for ip := range sk.addrs.named {
				wk.named[ip] = struct{}{}
			}
			wk.peak = max(wk.peak, sk.addrs.unnamed)
			if i != last {
				continue
			}
			devices := sk.addrs.devices()
			out.Keys[id] = keyPresence{Connections: sk.connections, Devices: devices}
			if sk.shared {
				out.People += devices
			} else {
				out.People++
			}
		}
	}
	for _, n := range sessions {
		out.People15m += n
	}
	for id, wk := range seen {
		devices := max(1, len(wk.named)+wk.peak)
		kp := out.Keys[id]
		kp.Devices15m = devices
		out.Keys[id] = kp
		if wk.shared {
			out.People15m += devices
		} else {
			out.People15m++
		}
	}
	return out
}

const (
	presenceWindow = 15 * time.Minute
	presenceSweeps = 15
)

type pastSweep struct {
	at    time.Time
	nodes []nodeReading
}

// presenceRing holds the last presenceWindow of sweeps; addresses live here and nowhere else.
type presenceRing struct{ sweeps []pastSweep }

// add records a sweep and returns the ones still inside the window, oldest first.
func (r *presenceRing) add(at time.Time, nodes []nodeReading) [][]nodeReading {
	kept := make([]pastSweep, 0, presenceSweeps)
	for _, s := range r.sweeps {
		if at.Sub(s.at) < presenceWindow {
			kept = append(kept, s)
		}
	}
	kept = append(kept, pastSweep{at: at, nodes: nodes})
	if len(kept) > presenceSweeps {
		kept = kept[len(kept)-presenceSweeps:]
	}
	r.sweeps = kept
	out := make([][]nodeReading, len(kept))
	for i, s := range kept {
		out[i] = s.nodes
	}
	return out
}
