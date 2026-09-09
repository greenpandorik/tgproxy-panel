package worker

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/store/db"
)

// stubQuerier serves one node with one profile and whatever GetNodeSite result the test wants.
type stubQuerier struct {
	node     db.Node
	profiles []db.ListNodeProfilesWithKeyRow
	site     db.NodeSite
	siteErr  error
}

func (s stubQuerier) GetNode(context.Context, uuid.UUID) (db.Node, error) { return s.node, nil }

func (s stubQuerier) ListNodeProfilesWithKey(context.Context, uuid.UUID) ([]db.ListNodeProfilesWithKeyRow, error) {
	return s.profiles, nil
}

func (s stubQuerier) GetNodeSite(context.Context, uuid.UUID) (db.NodeSite, error) {
	return s.site, s.siteErr
}

func testQuerier(t *testing.T) (stubQuerier, *crypto.Box) {
	t.Helper()
	box, err := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{3}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.EncryptString("00000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	return stubQuerier{
		node:     db.Node{ID: id, DirtySeq: 7},
		profiles: []db.ListNodeProfilesWithKeyRow{{ID: uuid.New(), NodeID: id, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")}},
	}, box
}

func TestDesiredStatePropagatesSiteError(t *testing.T) {
	q, box := testQuerier(t)
	boom := errors.New("connection reset by peer")
	q.siteErr = boom

	out, err := desiredState(context.Background(), q, box, q.node.ID)
	if err == nil {
		t.Fatal("a non-ErrNoRows GetNodeSite error was swallowed")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error %v does not wrap the underlying DB error", err)
	}
	if out.Req.Site != nil {
		t.Fatal("a failed site lookup must not produce a site to deploy")
	}
}

// The "no site assigned" case is not an error.
func TestDesiredStateNoSiteIsNotAnError(t *testing.T) {
	q, box := testQuerier(t)
	q.siteErr = pgx.ErrNoRows

	out, err := desiredState(context.Background(), q, box, q.node.ID)
	if err != nil {
		t.Fatalf("pgx.ErrNoRows must not surface as an error: %v", err)
	}
	if out.Req.Site != nil {
		t.Fatal("no site assigned, yet a bundle was produced")
	}
	if out.DirtySeq != 7 || len(out.ProfileIDs) != 1 {
		t.Fatalf("snapshot bookkeeping lost: %+v", out)
	}
}
