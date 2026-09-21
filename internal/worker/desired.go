// Package worker runs background jobs: apply, expiry, stats, offline detection.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/nodesvc"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type Desired struct {
	Req        nodedriver.ApplyRequest
	ProfileIDs []uuid.UUID
	Revisions  map[uuid.UUID]string
	DirtySeq   int64
	SiteHash   string
}

// profileRevision fingerprints everything about a profile that reaches the node, including the
// parts it inherits from its key. An apply carries the values that were current when it was built;
// by the time it finishes the operator may have changed the secret or the limits, and a success
// that reports the old values as synced would leave the key looking active while the node is still
// serving what it was serving before.
//
// It is derived rather than maintained on purpose: a counter has to be bumped at every write, and
// the write nobody remembers to bump is the one that causes this.
func profileRevision(p db.ListNodeProfilesWithKeyRow) string {
	h := sha256.New()
	write := func(parts ...any) {
		for _, v := range parts {
			_, _ = fmt.Fprintf(h, "%v\x00", v)
		}
	}
	write(p.Name, p.Backend, p.CarrierMode)
	// The ciphertext, not the secret: re-encrypting the same secret only makes a profile look
	// changed, which costs one more apply. Reading a changed secret as unchanged would not.
	h.Write(p.SecretEnc)
	h.Write(p.Limits)
	h.Write(p.KeyTelemtLimits)
	if p.KeyExpiresAt != nil {
		write(p.KeyExpiresAt.UTC().UnixNano())
	} else {
		write("no-expiry")
	}
	if p.KeyStatus.Valid {
		write(string(p.KeyStatus.KeyStatus))
	} else {
		write("no-key")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ProfilesStillAtRevision returns the profiles whose configuration is unchanged since the apply
// that carried it was built. Anything else has been edited in the meantime and stays pending.
func ProfilesStillAtRevision(ctx context.Context, q interface {
	ListNodeProfilesWithKey(ctx context.Context, nodeID uuid.UUID) ([]db.ListNodeProfilesWithKeyRow, error)
}, nodeID uuid.UUID, sent map[uuid.UUID]string,
) ([]uuid.UUID, error) {
	rows, err := q.ListNodeProfilesWithKey(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(sent))
	for _, p := range rows {
		if rev, ok := sent[p.ID]; ok && rev == profileRevision(p) {
			out = append(out, p.ID)
		}
	}
	return out, nil
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
	out := Desired{DirtySeq: node.DirtySeq, ProfileIDs: []uuid.UUID{}, Revisions: map[uuid.UUID]string{}}
	rows, err := q.ListNodeProfilesWithKey(ctx, nodeID)
	if err != nil {
		return Desired{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name == "default" && rows[j].Name != "default" })
	req := nodedriver.ApplyRequest{ApplyProfiles: true}
	if node.Engine == db.NodeEngineTelemt {
		req.TLSDomain, req.ClassicPort, req.PublicIP = node.TlsDomain, uint32(node.ClassicPort), node.PublicIp
		req.AdTag = node.AdTag
		policy := nodesvc.WebPolicyOf(node.TelemtWebPolicy)
		req.WebPolicy = &policy
	}
	seen := map[string]bool{}
	for _, p := range rows {
		secret, err := box.DecryptString(p.SecretEnc)
		if err != nil {
			out.Req = req
			return out, fmt.Errorf("decrypt profile %s: %w", p.Name, err)
		}
		out.ProfileIDs = append(out.ProfileIDs, p.ID)
		out.Revisions[p.ID] = profileRevision(p)
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
