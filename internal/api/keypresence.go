package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/store/db"
)

// keyPresencePoints is how many buckets a /keys/{id}/presence series has.
const keyPresencePoints = 96

var keyPresenceRanges = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

type keyPresenceNowJSON struct {
	People      int32 `json:"people"`
	People15m   int32 `json:"people_15m"`
	Connections int32 `json:"connections"`
}

type keyPresenceNodeJSON struct {
	NodeID      uuid.UUID `json:"node_id"`
	NodeName    string    `json:"node_name"`
	People      int32     `json:"people"`
	Connections int32     `json:"connections"`
}

// keyPresencePointJSON is one bucket; the counts are null when no sample fell into it.
type keyPresencePointJSON struct {
	T           time.Time `json:"t"`
	People      *int32    `json:"people"`
	Connections *int32    `json:"connections"`
}

type keyPresenceJSON struct {
	Shared bool                   `json:"shared"`
	Range  string                 `json:"range"`
	Step   int64                  `json:"step"`
	Now    keyPresenceNowJSON     `json:"now"`
	Nodes  []keyPresenceNodeJSON  `json:"nodes"`
	Points []keyPresencePointJSON `json:"points"`
}

// keyPresenceSeries lays the bucketed rows over every bucket of the range, empty ones included.
func keyPresenceSeries(from time.Time, step int64, rows []db.KeyPresenceSeriesRow) []keyPresencePointJSON {
	points := make([]keyPresencePointJSON, keyPresencePoints)
	for i := range points {
		points[i].T = from.Add(time.Duration(int64(i+1)*step) * time.Second)
	}
	for _, row := range rows {
		if row.Bucket < 0 || int(row.Bucket) >= len(points) {
			continue
		}
		people, connections := row.People, row.Connections
		points[row.Bucket].People = &people
		points[row.Bucket].Connections = &connections
	}
	return points
}

func (s *Server) handleKeyPresence(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "24h"
	}
	span, ok := keyPresenceRanges[name]
	if !ok {
		badRequest(w, "range must be 24h or 7d")
		return
	}
	ctx := r.Context()
	shared := k.Type == db.KeyTypeSHARED
	out := keyPresenceJSON{
		Shared: shared, Range: name, Step: int64(span/time.Second) / keyPresencePoints,
		Nodes: []keyPresenceNodeJSON{}, Points: []keyPresencePointJSON{},
	}
	live, err := s.store.Q.KeyPresenceForKeys(ctx, db.KeyPresenceForKeysParams{KeyIds: []uuid.UUID{k.ID}, Since: liveSince()})
	if err != nil {
		s.log.Error("key presence", "err", err)
		internal(w)
		return
	}
	for _, row := range live {
		out.Now = keyPresenceNowJSON{People: row.Devices, People15m: row.Devices15m, Connections: row.Connections}
		if !shared {
			out.Now.People = min(row.Devices, 1)
			out.Now.People15m = min(row.Devices15m, 1)
		}
	}
	if !shared {
		writeJSON(w, 200, out)
		return
	}
	nodes, err := s.store.Q.KeyPresenceNodes(ctx, db.KeyPresenceNodesParams{AccessKeyID: k.ID, Since: liveSince()})
	if err != nil {
		s.log.Error("key presence nodes", "err", err)
		internal(w)
		return
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, keyPresenceNodeJSON(n))
	}
	to := time.Now()
	from := to.Add(-span)
	rows, err := s.store.Q.KeyPresenceSeries(ctx, db.KeyPresenceSeriesParams{AccessKeyID: k.ID, FromAt: from, ToAt: to, Step: out.Step})
	if err != nil {
		s.log.Error("key presence series", "err", err)
		internal(w)
		return
	}
	out.Points = keyPresenceSeries(from, out.Step, rows)
	writeJSON(w, 200, out)
}
