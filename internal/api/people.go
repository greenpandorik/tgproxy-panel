package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tgwebproxy/internal/store/db"
)

// liveWindow is how old a reading may be and still describe the present.
const liveWindow = 3 * time.Minute

func liveSince() time.Time { return time.Now().Add(-liveWindow) }

type fleetPeopleJSON struct {
	PeopleOnline    *int32 `json:"people_online"`
	PeopleOnline15m *int32 `json:"people_online_15m"`
	Connections     *int32 `json:"connections"`
}

// fleetPeople is the newest fleet head count, with nulls when the stats worker has none fresh.
func (s *Server) fleetPeople(ctx context.Context) (fleetPeopleJSON, error) {
	row, err := s.store.Q.LatestFleetSnapshot(ctx, liveSince())
	if errors.Is(err, pgx.ErrNoRows) {
		return fleetPeopleJSON{}, nil
	}
	if err != nil {
		return fleetPeopleJSON{}, err
	}
	return fleetPeopleJSON{PeopleOnline: &row.PeopleOnline, PeopleOnline15m: &row.People15m, Connections: &row.Connections}, nil
}

// freshSnapshots is each node's newest snapshot inside liveWindow, by node.
func (s *Server) freshSnapshots(ctx context.Context) (map[uuid.UUID]db.LiveSnapshotsRow, error) {
	rows, err := s.store.Q.LiveSnapshots(ctx, liveSince())
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]db.LiveSnapshotsRow, len(rows))
	for _, row := range rows {
		out[row.NodeID] = row
	}
	return out, nil
}

func int4Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}
