package worker

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func personal(id uuid.UUID, conns int, ips ...string) keyReading {
	return keyReading{KeyID: id, Connections: conns, IPCount: len(ips), IPs: append([]string{}, ips...)}
}

func shared(id uuid.UUID, conns int, ips ...string) keyReading {
	k := personal(id, conns, ips...)
	k.Shared = true
	return k
}

func TestPresencePersonalKeyIsOnePerson(t *testing.T) {
	k := uuid.New()
	node := nodeReading{NodeID: uuid.New(), Connections: 9, Keys: []keyReading{personal(k, 9, "203.0.113.1", "198.51.100.1")}}
	p := computePresence([][]nodeReading{{node}})
	if p.People != 1 || p.People15m != 1 || p.Connections != 9 {
		t.Fatalf("presence = %+v, want one person behind nine connections", p)
	}
	if got := p.Keys[k]; got.Connections != 9 || got.Devices != 2 || got.Devices15m != 2 {
		t.Fatalf("key = %+v: a phone on LTE and a laptop on Wi-Fi are two devices of one person", got)
	}
	if n := nodePeople(node); n != 1 {
		t.Fatalf("node people = %d, want 1", n)
	}
}

func TestPresenceSharedKeyCountsAddresses(t *testing.T) {
	k := uuid.New()
	node := nodeReading{NodeID: uuid.New(), Connections: 12, Keys: []keyReading{shared(k, 12, "203.0.113.1", "203.0.113.2", "203.0.113.3")}}
	p := computePresence([][]nodeReading{{node}})
	if p.People != 3 || p.Keys[k].Devices != 3 {
		t.Fatalf("presence = %+v, want three people", p)
	}
	if n := nodePeople(node); n != 3 {
		t.Fatalf("node people = %d, want 3", n)
	}
}

func TestPresenceUnionAcrossNodes(t *testing.T) {
	team, ivan := uuid.New(), uuid.New()
	a := nodeReading{NodeID: uuid.New(), Connections: 7, Keys: []keyReading{
		shared(team, 4, "203.0.113.1", "203.0.113.2"),
		personal(ivan, 3, "198.51.100.7"),
	}}
	b := nodeReading{NodeID: uuid.New(), Connections: 5, Keys: []keyReading{
		shared(team, 3, "203.0.113.2", "203.0.113.3"),
		personal(ivan, 2, "198.51.100.8"),
	}}
	p := computePresence([][]nodeReading{{a, b}})
	if p.People != 4 {
		t.Fatalf("people = %d, want 3 behind the shared key plus Ivan once", p.People)
	}
	if p.Connections != 12 {
		t.Fatalf("connections = %d, want 12", p.Connections)
	}
	if got := p.Keys[team]; got.Connections != 7 || got.Devices != 3 {
		t.Fatalf("team = %+v: 203.0.113.2 is on both servers and counts once", got)
	}
	if got := p.Keys[ivan]; got.Connections != 5 || got.Devices != 2 {
		t.Fatalf("ivan = %+v", got)
	}
	if na, nb := nodePeople(a), nodePeople(b); na != 3 || nb != 3 {
		t.Fatalf("node people = %d/%d, want 3/3: each server counts what it sees", na, nb)
	}
}

func TestPresenceNeedsAConnection(t *testing.T) {
	gone, quiet, team := uuid.New(), uuid.New(), uuid.New()
	node := nodeReading{NodeID: uuid.New(), Keys: []keyReading{
		{KeyID: gone, Connections: 0, IPCount: 2, IPs: []string{"203.0.113.1", "203.0.113.2"}},
		{KeyID: quiet, Shared: true, Connections: 0},
		{KeyID: team, Shared: true, Connections: 2, IPs: []string{}},
	}}
	p := computePresence([][]nodeReading{{node}})
	if p.People != 1 {
		t.Fatalf("people = %d: only the key with a connection counts, and at least as one", p.People)
	}
	if _, ok := p.Keys[gone]; ok {
		t.Fatalf("a key without connections is not online: %+v", p.Keys)
	}
	if got := p.Keys[team]; got.Devices != 1 {
		t.Fatalf("team = %+v, want one device", got)
	}
}

func TestPresenceFallsBackToCountsFromOldAgents(t *testing.T) {
	team := uuid.New()
	old := func(conns, ips int) keyReading {
		return keyReading{KeyID: team, Shared: true, Connections: conns, IPCount: ips}
	}
	a := nodeReading{NodeID: uuid.New(), Keys: []keyReading{old(5, 3)}}
	b := nodeReading{NodeID: uuid.New(), Keys: []keyReading{old(2, 1)}}
	p := computePresence([][]nodeReading{{a, b}})
	if p.People != 4 || p.Keys[team].Devices != 4 {
		t.Fatalf("presence = %+v: without lists the counts add up", p)
	}
	if n := nodePeople(a); n != 3 {
		t.Fatalf("node people = %d, want 3", n)
	}

	later := nodeReading{NodeID: a.NodeID, Keys: []keyReading{old(5, 2)}}
	p = computePresence([][]nodeReading{{a, b}, {later}})
	if p.People != 2 || p.Keys[team].Devices15m != 4 {
		t.Fatalf("presence = %+v: the window takes the largest sweep, not the sum of them", p)
	}
}

func TestPresenceCountsAddressesBeyondAShortList(t *testing.T) {
	team := uuid.New()
	k := shared(team, 40, "203.0.113.1", "203.0.113.2")
	k.IPCount = 5
	p := computePresence([][]nodeReading{{{NodeID: uuid.New(), Keys: []keyReading{k}}}})
	if p.People != 5 {
		t.Fatalf("people = %d, want the node's own count when the list was cut short", p.People)
	}
}

func TestPresenceWindow(t *testing.T) {
	team, ivan, anna := uuid.New(), uuid.New(), uuid.New()
	node := uuid.New()
	sweeps := [][]nodeReading{
		{{NodeID: node, Keys: []keyReading{shared(team, 2, "203.0.113.1"), personal(ivan, 3, "198.51.100.7")}}},
		{{NodeID: node, Keys: []keyReading{shared(team, 2, "203.0.113.2")}}},
		{{NodeID: node, Keys: []keyReading{shared(team, 1, "203.0.113.2"), personal(anna, 1, "192.0.2.4")}}},
	}
	p := computePresence(sweeps)
	if p.People != 2 {
		t.Fatalf("people now = %d, want team's one address and Anna", p.People)
	}
	if p.People15m != 4 {
		t.Fatalf("people over 15 minutes = %d, want two team addresses, Ivan and Anna", p.People15m)
	}
	if got := p.Keys[team]; got.Devices != 1 || got.Devices15m != 2 {
		t.Fatalf("team = %+v", got)
	}
	if got, ok := p.Keys[ivan]; !ok || got.Connections != 0 || got.Devices != 0 || got.Devices15m != 1 {
		t.Fatalf("ivan left: offline now, still seen in the window: %+v (present %v)", got, ok)
	}
}

func TestPresenceTproxySessions(t *testing.T) {
	legacy := uuid.New()
	now := nodeReading{NodeID: legacy, Connections: 3, Sessions: 3}
	p := computePresence([][]nodeReading{{{NodeID: legacy, Connections: 5, Sessions: 5}}, {now}})
	if p.People != 3 || p.People15m != 5 || p.Connections != 3 {
		t.Fatalf("presence = %+v: tproxy sessions stand for people, the window keeps the peak", p)
	}
	if n := nodePeople(now); n != 3 {
		t.Fatalf("node people = %d, want 3", n)
	}
}

func TestPresenceEmpty(t *testing.T) {
	p := computePresence([][]nodeReading{{}})
	if p.People != 0 || p.People15m != 0 || p.Connections != 0 || len(p.Keys) != 0 {
		t.Fatalf("presence = %+v", p)
	}
}

func TestPresenceRingKeepsFifteenMinutes(t *testing.T) {
	var r presenceRing
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	mark := func(i int) []nodeReading { return []nodeReading{{Sessions: i}} }
	var window [][]nodeReading
	for i := range 20 {
		window = r.add(base.Add(time.Duration(i)*time.Minute), mark(i))
	}
	if len(window) != presenceSweeps || window[0][0].Sessions != 5 || window[len(window)-1][0].Sessions != 19 {
		t.Fatalf("window holds %d sweeps from %d to %d, want the last 15", len(window), window[0][0].Sessions, window[len(window)-1][0].Sessions)
	}
	window = r.add(base.Add(40*time.Minute), mark(40))
	if len(window) != 1 || window[0][0].Sessions != 40 {
		t.Fatalf("after a long gap only the new sweep is left, got %d", len(window))
	}
}
