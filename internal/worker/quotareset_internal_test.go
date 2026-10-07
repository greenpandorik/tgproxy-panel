package worker

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func TestQuotaResetMarksNodesWhenTheirPeriodStarts(t *testing.T) {
	st := store.OpenTest(t)
	box, _ := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)})
	ks := keys.New(st, box)
	ctx := context.Background()
	node := func(name string, engine db.NodeEngine) db.Node {
		n, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: name, Hostname: name + ".test", Engine: db.NullNodeEngine{NodeEngine: engine, Valid: true}})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	monthly, lifetime, tproxy := node("monthly", db.NodeEngineTelemt), node("lifetime", db.NodeEngineTelemt), node("tp", db.NodeEngineTproxy)
	key := func(label string, limits domain.TelemtLimits, nodes ...uuid.UUID) {
		if _, err := ks.Create(ctx, keys.CreateInput{Label: label, Type: domain.KeyPersonal, CarrierMode: "https", TelemtLimits: limits, NodeIDs: nodes}); err != nil {
			t.Fatal(err)
		}
	}
	key("m", domain.TelemtLimits{DataQuotaBytes: 1 << 30, DataQuotaPeriod: domain.QuotaPeriodMonth}, monthly.ID, tproxy.ID)
	key("l", domain.TelemtLimits{DataQuotaBytes: 1 << 30}, lifetime.ID)

	dirty := func() map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, n := range []db.Node{monthly, lifetime, tproxy} {
			got, err := st.Q.GetNode(ctx, n.ID)
			if err != nil {
				t.Fatal(err)
			}
			out[n.ID] = got.Dirty
			if err := st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: n.ID, Dirty: false}); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	dirty()

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	r := NewQuotaReset(st.Q, slog.New(slog.DiscardHandler))
	r.now = func() time.Time { return now }
	step := func(at time.Time, want int) map[uuid.UUID]bool {
		t.Helper()
		now = at
		n, err := r.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Fatalf("at %s marked %d nodes, want %d", at, n, want)
		}
		return dirty()
	}

	// The first pass catches a boundary missed while the panel was down.
	if got := step(now, 1); !got[monthly.ID] || got[lifetime.ID] || got[tproxy.ID] {
		t.Fatalf("first pass dirty: %v", got)
	}
	step(now.Add(time.Hour), 0)
	// A new week is not a new month.
	step(time.Date(2026, 10, 26, 0, 0, 30, 0, time.UTC), 0)
	if got := step(time.Date(2026, 11, 1, 0, 0, 30, 0, time.UTC), 1); !got[monthly.ID] {
		t.Fatalf("month rollover dirty: %v", got)
	}
	step(time.Date(2026, 11, 2, 0, 0, 30, 0, time.UTC), 0)
}

func TestDesiredStateCarriesTheQuotaPeriodStart(t *testing.T) {
	st := store.OpenTest(t)
	box, _ := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)})
	ctx := context.Background()
	n, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test", Engine: db.NullNodeEngine{NodeEngine: db.NodeEngineTelemt, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := keys.New(st, box).Create(ctx, keys.CreateInput{
		Label: "w", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{n.ID},
		TelemtLimits: domain.TelemtLimits{DataQuotaBytes: 1 << 30, DataQuotaPeriod: domain.QuotaPeriodWeek},
	})
	if err != nil {
		t.Fatal(err)
	}
	des, err := desiredState(ctx, st.Q, box, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.QuotaPeriodWeek.Start(time.Now())
	for _, p := range des.Req.Profiles {
		if p.Name == domain.ProfileName(k.ID) {
			if p.QuotaResetAt == nil || !p.QuotaResetAt.Equal(want) {
				t.Fatalf("quota reset at %v, want %v", p.QuotaResetAt, want)
			}
			return
		}
	}
	t.Fatalf("key profile missing: %+v", des.Req.Profiles)
}
