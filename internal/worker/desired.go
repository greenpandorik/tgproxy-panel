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

type Desired struct {
	Req        nodedriver.ApplyRequest
	ProfileIDs []uuid.UUID
	DirtySeq   int64
	SiteHash   string
}

// desiredQuerier is the slice of the store that DesiredState reads.
type desiredQuerier interface {
	GetNode(ctx context.Context, id uuid.UUID) (db.Node, error)
	ListNodeProfilesWithKey(ctx context.Context, nodeID uuid.UUID) ([]db.ListNodeProfilesWithKeyRow, error)
	GetNodeSite(ctx context.Context, nodeID uuid.UUID) (db.NodeSite, error)
}

// DesiredState builds the full ApplyRequest for a node from the database.
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
	if node.Engine == db.NodeEngineTelemt {
		req.TLSDomain, req.ClassicPort, req.PublicIP = node.TlsDomain, uint32(node.ClassicPort), node.PublicIp
		req.AdTag = node.AdTag
	}
	seen := map[string]bool{}
	for _, p := range rows {
		secret, err := box.DecryptString(p.SecretEnc)
		if err != nil {
			out.Req = req
			return out, fmt.Errorf("decrypt profile %s: %w", p.Name, err)
		}
		out.ProfileIDs = append(out.ProfileIDs, p.ID)
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
