// Package worker runs background jobs: apply, expiry, stats, offline detection.
package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

// Desired is one snapshot of a node's desired state together with the bookkeeping the
// apply worker needs to write the outcome back safely: the ids of the profiles actually
// contained in Req, and the node's dirty_seq as of the snapshot.
type Desired struct {
	Req nodedriver.ApplyRequest
	// ProfileIDs are exactly the profiles present in Req.Profiles. Only these may be
	// marked synced/failed when the apply finishes.
	ProfileIDs []uuid.UUID
	// DirtySeq is the node's dirty_seq at snapshot time. SetNodeApplied only clears
	// `dirty` when the counter has not moved since.
	DirtySeq int64
	// SiteHash is the bundle_hash of the site carried in Req.Site, empty when this
	// snapshot pushes no site. The apply writes exactly this value to
	// node_sites.deployed_hash; re-reading the row after the apply would record a
	// bundle that was assigned mid-apply and never actually pushed.
	SiteHash string
}

// desiredQuerier is the slice of the store that DesiredState reads. Narrowing the
// dependency lets a test substitute a querier whose GetNodeSite fails with a real
// DB error (not pgx.ErrNoRows) and prove the error propagates instead of being
// read as "no site assigned".
type desiredQuerier interface {
	GetNode(ctx context.Context, id uuid.UUID) (db.Node, error)
	ListNodeProfilesWithKey(ctx context.Context, nodeID uuid.UUID) ([]db.ListNodeProfilesWithKeyRow, error)
	GetNodeSite(ctx context.Context, nodeID uuid.UUID) (db.NodeSite, error)
}

// DesiredState builds the full ApplyRequest for a node from the database.
//
// Read order matters and is deliberate: dirty_seq is read *before* the profile list. A
// mutation that commits between the two reads therefore leaves us with a stale dirty_seq
// (so the node stays dirty and is re-applied), never with a stale profile list paired with
// a fresh dirty_seq — which is the combination that would silently drop state.
func DesiredState(ctx context.Context, st *store.Store, box *crypto.Box, nodeID uuid.UUID) (Desired, error) {
	return desiredState(ctx, st.Q, box, nodeID)
}

func desiredState(ctx context.Context, q desiredQuerier, box *crypto.Box, nodeID uuid.UUID) (Desired, error) {
	node, err := q.GetNode(ctx, nodeID)
	if err != nil {
		return Desired{}, err
	}
	out := Desired{DirtySeq: node.DirtySeq, ProfileIDs: []uuid.UUID{}}
	rows, err := q.ListNodeProfilesWithKey(ctx, nodeID)
	if err != nil {
		return Desired{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name == "default" && rows[j].Name != "default" })
	req := nodedriver.ApplyRequest{ApplyProfiles: true}
	// The Fake-TLS listener travels with every apply on a telemt node, so an operator's edit
	// of tls_domain/classic_port actually reaches the node instead of only moving the panel's
	// idea of it. A tproxy node sends neither: it has no such listener and its agent would
	// ignore them anyway.
	if node.Engine == db.NodeEngineTelemt {
		req.TLSDomain, req.ClassicPort = node.TlsDomain, uint32(node.ClassicPort)
	}
	seen := map[string]bool{}
	for _, p := range rows {
		secret, err := box.DecryptString(p.SecretEnc)
		if err != nil {
			out.Req = req
			return out, fmt.Errorf("decrypt profile %s: %w", p.Name, err)
		}
		out.ProfileIDs = append(out.ProfileIDs, p.ID)
		// Enabled is written literally for both engines: every profile the panel still lists
		// is one it wants served, and a key that was revoked between the two reads below is
		// pushed as a disabled user rather than silently left running until its profile row
		// disappears. The tproxy agent ignores the field.
		prof := nodedriver.Profile{
			Name: p.Name, Secret: secret, Backend: p.Backend, CarrierMode: p.CarrierMode,
			ExpiresAt: p.KeyExpiresAt,
			Enabled:   !p.KeyStatus.Valid || p.KeyStatus.KeyStatus != db.KeyStatusRevoked,
		}
		var telemt domain.TelemtLimits
		if len(p.KeyTelemtLimits) > 0 {
			if err := json.Unmarshal(p.KeyTelemtLimits, &telemt); err == nil && telemt != (domain.TelemtLimits{}) {
				t := telemt
				prof.Telemt = &t
			}
		}
		var limits domain.ProfileLimits
		if len(p.Limits) > 0 && string(p.Limits) != "{}" {
			if err := json.Unmarshal(p.Limits, &limits); err == nil && limits != (domain.ProfileLimits{}) {
				l := limits
				prof.Limits = &l
			}
		}
		req.Profiles = append(req.Profiles, prof)
		if !seen[secret] {
			seen[secret] = true
			req.MTProxySecrets = append(req.MTProxySecrets, secret)
		}
	}
	out.Req = req
	site, err := q.GetNodeSite(ctx, nodeID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No site assigned to this node: nothing to deploy.
	case err != nil:
		// A transient DB error must never be silently read as "site unchanged" — that would
		// let an apply report success while the node keeps serving the old site.
		return out, fmt.Errorf("node site: %w", err)
	case site.DeployedHash == nil || *site.DeployedHash != site.BundleHash:
		files := map[string]string{}
		if err := json.Unmarshal(site.Bundle, &files); err != nil {
			return out, fmt.Errorf("site bundle: %w", err)
		}
		bundle := nodedriver.SiteBundle{Files: map[string][]byte{}}
		for p, b64 := range files {
			raw, err := decodeB64(b64)
			if err != nil {
				return out, fmt.Errorf("site file %s: %w", p, err)
			}
			bundle.Files[p] = raw
		}
		out.Req.Site = &bundle
		out.SiteHash = site.BundleHash
	}
	return out, nil
}

func decodeB64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
