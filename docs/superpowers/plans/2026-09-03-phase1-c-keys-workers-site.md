# Phase 1 / Part C — Keys, apply worker, site templates, branding, dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the product core: access keys (shared/personal/batch) with links and QR, the batching apply worker with expiry/stats/offline detection, the site-template validator + first preset, branding, dashboard summary and panel metrics.

**Architecture:** `internal/keys` service owns key lifecycle and profile creation per bound node, marking nodes dirty. `internal/worker` turns dirty nodes into `nodedriver.Apply` calls (full desired state per node), records `apply_jobs`, flips key/profile states. `internal/sitekit` normalises HTML into a CSP-safe bundle. Branding is a DB row exposed publicly for the SPA.

**Tech Stack:** golang.org/x/net/html, prometheus/client_golang (panel metrics) + prometheus/common/expfmt (parse relay metrics), chi, sqlc.

**Spec:** `docs/superpowers/specs/2026-09-03-webproxy-panel-design.md` (§3.4–§3.7, §3.9, §5)

## Global Constraints

Same as Parts A and B. Additionally:
- A node's desired profile set = its `default` profile + one profile per active/pending key bound to it. Revoked keys have no profiles.
- MTProxy secret list for a node = secrets of all its profiles (deduplicated, `default` first).
- Every mutation that changes a node's desired state sets `nodes.dirty = true`; nothing calls the driver from a request handler except explicit "apply now".
- Site bundles must pass `sitekit.Normalize` before storage.

---

## File structure (Part C)

```
internal/store/queries/keys.sql, apply.sql, sites.sql, branding.sql, stats.sql
internal/keys/service.go          # Create/Batch/Revoke/Rotate/Delete/Links/Bulk
internal/keys/service_test.go
internal/worker/desired.go        # DesiredState(nodeID) -> nodedriver.ApplyRequest
internal/worker/apply.go          # Apply worker (ticker + Trigger)
internal/worker/expiry.go
internal/worker/stats.go          # metrics/stats snapshots + offline detection
internal/worker/runner.go
internal/worker/*_test.go
internal/sitekit/normalize.go     # validator + inline extraction
internal/sitekit/presets/studio/index.html
internal/sitekit/presets.go
internal/sitekit/normalize_test.go
internal/branding/css.go          # custom css / svg sanitizer
internal/branding/css_test.go
internal/api/keys.go, keys_test.go
internal/api/sites.go, sites_test.go
internal/api/branding.go, branding_test.go
internal/api/dashboard.go, dashboard_test.go
internal/api/settings.go
internal/api/metrics.go           # panel /metrics
cmd/panel/main.go                 # start workers, wire ApplyNow + SiteProvider
```

---

### Task 10: Keys service and API

**Files:**
- Create: `internal/store/queries/keys.sql`, `internal/keys/service.go`, `internal/keys/service_test.go`, `internal/api/keys.go`, `internal/api/keys_test.go`
- Modify: `internal/api/server.go` (`Deps.Keys *keys.Service`, mount routes), `internal/api/apitest/apitest.go` (construct `keys.New(st, box)`)

**Interfaces:**
- Produces (package `keys`):
```go
type Service struct{ st *store.Store; box *crypto.Box }
func New(st *store.Store, box *crypto.Box) *Service
type CreateInput struct { Label, OwnerLabel, Note string; Type domain.KeyType; CarrierMode domain.CarrierMode; Limits domain.ProfileLimits; ExpiresAt *time.Time; NodeIDs []uuid.UUID; CreatedBy uuid.UUID }
func (s *Service) Create(ctx, CreateInput) (db.AccessKey, error)              // ErrCapacity, ErrValidation
func (s *Service) CreateBatch(ctx, in CreateInput, prefix string, count int) ([]db.AccessKey, error)
func (s *Service) Revoke(ctx, keyID) error                                        // personal: delete profiles; shared: same (secret dies)
func (s *Service) Rotate(ctx, keyID) (db.AccessKey, error)                        // new secret for shared keys, profiles updated, key back to pending
func (s *Service) Delete(ctx, keyID) error
func (s *Service) Update(ctx, keyID, label, ownerLabel, note string, expiresAt *time.Time, carrierMode domain.CarrierMode, limits domain.ProfileLimits) (db.AccessKey, error)
func (s *Service) Bind(ctx, keyID, nodeID) error; func (s *Service) Unbind(ctx, keyID, nodeID) error
func (s *Service) Secret(ctx, key db.AccessKey) (string, error)
type Link struct { NodeID uuid.UUID; NodeName, Hostname, TMe, Tg string }
func (s *Service) Links(ctx, keyID) ([]Link, error)
var ErrCapacity = errors.New("node profile capacity reached"); var ErrNotFound; type ValidationError map[string]string (implements error)
```
- API routes: `GET /keys?type=&status=&node=&q=&page=&per_page=` → `{items, total}`; `POST /keys`; `POST /keys/batch` `{...CreateInput, prefix, count}`; `GET /keys/{id}` (includes `links`, `secret` for writers only); `PATCH /keys/{id}`; `DELETE /keys/{id}`; `POST /keys/{id}/revoke`; `POST /keys/{id}/rotate`; `GET /keys/{id}/links`; `GET /keys/{id}/qr?node=<id>&size=256` (image/png); `POST /keys/{id}/bindings` `{node_id}`; `DELETE /keys/{id}/bindings/{node_id}`; `POST /keys/bulk` `{action: revoke|delete|extend, ids: [], expires_at?}`.
- Key JSON: `{id, label, type, owner_label, status, carrier_mode, limits, expires_at, revoked_at, note, created_at, nodes: [{node_id, node_name, hostname, profile_sync}], client_support: {desktop:"stable", android:"experimental", ios:"planned"}}`.

- [ ] **Step 1: Queries**

`internal/store/queries/keys.sql`:
```sql
-- name: CreateKey :one
INSERT INTO access_keys (label, type, owner_label, secret_enc, carrier_mode, limits, expires_at, note, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: GetKey :one
SELECT * FROM access_keys WHERE id = $1;

-- name: ListKeys :many
SELECT k.* FROM access_keys k
WHERE (sqlc.narg('type')::key_type IS NULL OR k.type = sqlc.narg('type'))
  AND (sqlc.narg('status')::key_status IS NULL OR k.status = sqlc.narg('status'))
  AND (sqlc.narg('node_id')::uuid IS NULL OR EXISTS (SELECT 1 FROM key_bindings b WHERE b.access_key_id = k.id AND b.node_id = sqlc.narg('node_id')))
  AND (sqlc.narg('q')::text IS NULL OR k.label ILIKE '%' || sqlc.narg('q') || '%' OR k.owner_label ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY k.created_at DESC LIMIT $1 OFFSET $2;

-- name: CountKeys :one
SELECT count(*) FROM access_keys k
WHERE (sqlc.narg('type')::key_type IS NULL OR k.type = sqlc.narg('type'))
  AND (sqlc.narg('status')::key_status IS NULL OR k.status = sqlc.narg('status'))
  AND (sqlc.narg('node_id')::uuid IS NULL OR EXISTS (SELECT 1 FROM key_bindings b WHERE b.access_key_id = k.id AND b.node_id = sqlc.narg('node_id')))
  AND (sqlc.narg('q')::text IS NULL OR k.label ILIKE '%' || sqlc.narg('q') || '%' OR k.owner_label ILIKE '%' || sqlc.narg('q') || '%');

-- name: UpdateKey :one
UPDATE access_keys SET label = $2, owner_label = $3, note = $4, expires_at = $5, carrier_mode = $6, limits = $7 WHERE id = $1 RETURNING *;

-- name: SetKeyStatus :exec
UPDATE access_keys SET status = $2, revoked_at = CASE WHEN $2 = 'revoked' THEN now() ELSE revoked_at END WHERE id = $1;

-- name: SetKeySecret :exec
UPDATE access_keys SET secret_enc = $2, status = 'pending', revoked_at = NULL WHERE id = $1;

-- name: SetKeyExpiry :exec
UPDATE access_keys SET expires_at = $2 WHERE id = $1;

-- name: DeleteKey :exec
DELETE FROM access_keys WHERE id = $1;

-- name: ListExpiredActiveKeys :many
SELECT * FROM access_keys WHERE status IN ('pending','active') AND expires_at IS NOT NULL AND expires_at <= now();

-- name: CountKeysByStatus :many
SELECT status, count(*) AS n FROM access_keys GROUP BY status;

-- name: CreateBinding :exec
INSERT INTO key_bindings (access_key_id, node_id, profile_id) VALUES ($1, $2, $3);

-- name: DeleteBinding :exec
DELETE FROM key_bindings WHERE access_key_id = $1 AND node_id = $2;

-- name: ListKeyBindings :many
SELECT b.*, n.name AS node_name, n.hostname, p.sync_state FROM key_bindings b
JOIN nodes n ON n.id = b.node_id JOIN profiles p ON p.id = b.profile_id
WHERE b.access_key_id = $1 ORDER BY n.name;

-- name: ListNodeKeyBindings :many
SELECT b.*, k.status AS key_status FROM key_bindings b JOIN access_keys k ON k.id = b.access_key_id WHERE b.node_id = $1;

-- name: ActivatePendingKeysForNode :exec
UPDATE access_keys k SET status = 'active' WHERE k.status = 'pending'
  AND NOT EXISTS (SELECT 1 FROM profiles p WHERE p.access_key_id = k.id AND p.sync_state <> 'synced');
```

Run `sqlc generate`.

- [ ] **Step 2: Write failing service tests**

`internal/keys/service_test.go`:
```go
package keys_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

func setup(t *testing.T) (*keys.Service, *store.Store, uuid.UUID) {
	t.Helper()
	st := store.OpenTest(t)
	box, _ := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)})
	ctx := context.Background()
	n, err := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := box.EncryptString("00000000000000000000000000000000")
	_, _ = st.Q.CreateProfile(ctx, db.CreateProfileParams{NodeID: n.ID, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")})
	return keys.New(st, box), st, n.ID
}

func TestCreatePersonalCreatesProfileAndMarksDirty(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	k, err := svc.Create(ctx, keys.CreateInput{Label: "Ivan", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if err != nil {
		t.Fatal(err)
	}
	if k.Status != db.KeyStatusPending || k.Type != db.KeyTypePERSONAL {
		t.Fatalf("key %+v", k)
	}
	profiles, _ := st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	if len(profiles) != 1 || profiles[0].Name != domain.ProfileName(k.ID) || profiles[0].NodeID != nodeID {
		t.Fatalf("profiles %+v", profiles)
	}
	n, _ := st.Q.GetNode(ctx, nodeID)
	if !n.Dirty {
		t.Fatal("node must be dirty")
	}
	secret, _ := svc.Secret(ctx, k)
	if domain.ValidateSecretHex(secret) != nil {
		t.Fatalf("bad secret %q", secret)
	}
	links, _ := svc.Links(ctx, k.ID)
	if len(links) != 1 || links[0].TMe != "https://t.me/webproxy?server=n.test&secret="+secret {
		t.Fatalf("links %+v", links)
	}
}

func TestCreateValidation(t *testing.T) {
	svc, _, nodeID := setup(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, keys.CreateInput{Label: "", Type: domain.KeyShared, NodeIDs: []uuid.UUID{nodeID}})
	var ve keys.ValidationError
	if !errors.As(err, &ve) || ve["label"] == "" {
		t.Fatalf("expected label validation, got %v", err)
	}
	_, err = svc.Create(ctx, keys.CreateInput{Label: "x", Type: domain.KeyShared, CarrierMode: "bogus", NodeIDs: []uuid.UUID{nodeID}})
	if !errors.As(err, &ve) || ve["carrier_mode"] == "" {
		t.Fatalf("expected carrier validation, got %v", err)
	}
	_, err = svc.Create(ctx, keys.CreateInput{Label: "x", Type: domain.KeyShared, CarrierMode: "https"})
	if !errors.As(err, &ve) || ve["node_ids"] == "" {
		t.Fatalf("expected node_ids validation, got %v", err)
	}
}

func TestCapacityLimit(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	_, _ = st.Q.UpdateNode(ctx, db.UpdateNodeParams{ID: nodeID, Name: "n", MaxProfiles: 2})
	if _, err := svc.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, keys.CreateInput{Label: "b", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if !errors.Is(err, keys.ErrCapacity) {
		t.Fatalf("expected ErrCapacity, got %v", err)
	}
}

func TestBatchCreate(t *testing.T) {
	svc, _, nodeID := setup(t)
	ks, err := svc.CreateBatch(context.Background(), keys.CreateInput{Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}, "team", 3)
	if err != nil || len(ks) != 3 || ks[0].Label != "team-1" || ks[2].Label != "team-3" {
		t.Fatalf("batch %v %v", ks, err)
	}
}

func TestRevokeRemovesProfilesAndRotateRenewsSecret(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	k, _ := svc.Create(ctx, keys.CreateInput{Label: "team", Type: domain.KeyShared, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	before, _ := svc.Secret(ctx, k)
	k2, err := svc.Rotate(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Secret(ctx, k2)
	if before == after {
		t.Fatal("rotate must change the secret")
	}
	profiles, _ := st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	got, _ := svc.Secret(ctx, db.AccessKey{SecretEnc: profiles[0].SecretEnc})
	if got != after {
		t.Fatal("profile secret not rotated")
	}
	if err := svc.Revoke(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	profiles, _ = st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	rk, _ := st.Q.GetKey(ctx, k.ID)
	if len(profiles) != 0 || rk.Status != db.KeyStatusRevoked || rk.RevokedAt == nil {
		t.Fatalf("after revoke: %d profiles, key %+v", len(profiles), rk)
	}
	if err := svc.Revoke(ctx, k.ID); err != nil {
		t.Fatalf("revoke must be idempotent: %v", err)
	}
}

func TestBindUnbind(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	n2, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n2", Hostname: "n2.test"})
	k, _ := svc.Create(ctx, keys.CreateInput{Label: "k", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}})
	if err := svc.Bind(ctx, k.ID, n2.ID); err != nil {
		t.Fatal(err)
	}
	links, _ := svc.Links(ctx, k.ID)
	if len(links) != 2 {
		t.Fatalf("links %d", len(links))
	}
	if err := svc.Unbind(ctx, k.ID, n2.ID); err != nil {
		t.Fatal(err)
	}
	profiles, _ := st.Q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: k.ID, Valid: true})
	if len(profiles) != 1 {
		t.Fatalf("profiles after unbind %d", len(profiles))
	}
}
```

- [ ] **Step 3: Run to verify fail**

Run: `go test ./internal/keys/`
Expected: compile errors.

- [ ] **Step 4: Implement service**

`internal/keys/service.go`:
```go
// Package keys manages access keys and their per-node profiles.
package keys

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/qrlink"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

var (
	ErrCapacity = errors.New("node profile capacity reached")
	ErrNotFound = errors.New("key not found")
)

type ValidationError map[string]string

func (v ValidationError) Error() string { return fmt.Sprintf("validation failed: %v", map[string]string(v)) }

type Service struct {
	st  *store.Store
	box *crypto.Box
}

func New(st *store.Store, box *crypto.Box) *Service { return &Service{st: st, box: box} }

type CreateInput struct {
	Label, OwnerLabel, Note string
	Type                    domain.KeyType
	CarrierMode             domain.CarrierMode
	Limits                  domain.ProfileLimits
	ExpiresAt               *time.Time
	NodeIDs                 []uuid.UUID
	CreatedBy               uuid.UUID
}

func (in CreateInput) validate(requireLabel bool) error {
	ve := ValidationError{}
	if requireLabel && strings.TrimSpace(in.Label) == "" {
		ve["label"] = "required"
	}
	if in.Type != domain.KeyShared && in.Type != domain.KeyPersonal {
		ve["type"] = "SHARED or PERSONAL"
	}
	if !in.CarrierMode.Valid() {
		ve["carrier_mode"] = "https, https-lanes, websocket or websocket-lanes"
	}
	if err := in.Limits.Validate(); err != nil {
		ve["limits"] = err.Error()
	}
	if len(in.NodeIDs) == 0 {
		ve["node_ids"] = "bind at least one node"
	}
	if in.ExpiresAt != nil && in.ExpiresAt.Before(time.Now()) {
		ve["expires_at"] = "must be in the future"
	}
	if len(ve) > 0 {
		return ve
	}
	return nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (db.AccessKey, error) {
	if err := in.validate(true); err != nil {
		return db.AccessKey{}, err
	}
	secret, err := crypto.NewSecretHex()
	if err != nil {
		return db.AccessKey{}, err
	}
	enc, err := s.box.EncryptString(secret)
	if err != nil {
		return db.AccessKey{}, err
	}
	limits, _ := json.Marshal(in.Limits)
	var key db.AccessKey
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		var err error
		key, err = q.CreateKey(ctx, db.CreateKeyParams{
			Label: in.Label, Type: db.KeyType(in.Type), OwnerLabel: in.OwnerLabel, SecretEnc: enc, CarrierMode: string(in.CarrierMode),
			Limits: limits, ExpiresAt: in.ExpiresAt, Note: in.Note, CreatedBy: uuid.NullUUID{UUID: in.CreatedBy, Valid: in.CreatedBy != uuid.Nil},
		})
		if err != nil {
			return err
		}
		for _, nodeID := range in.NodeIDs {
			if err := s.bindTx(ctx, q, key, nodeID); err != nil {
				return err
			}
		}
		return nil
	})
	return key, err
}

func (s *Service) CreateBatch(ctx context.Context, in CreateInput, prefix string, count int) ([]db.AccessKey, error) {
	if count < 1 || count > 100 {
		return nil, ValidationError{"count": "1..100"}
	}
	if strings.TrimSpace(prefix) == "" {
		return nil, ValidationError{"prefix": "required"}
	}
	out := make([]db.AccessKey, 0, count)
	for i := 1; i <= count; i++ {
		item := in
		item.Label = fmt.Sprintf("%s-%d", prefix, i)
		k, err := s.Create(ctx, item)
		if err != nil {
			return out, err
		}
		out = append(out, k)
	}
	return out, nil
}

// bindTx creates the node profile for the key and the binding, checking capacity.
func (s *Service) bindTx(ctx context.Context, q *db.Queries, key db.AccessKey, nodeID uuid.UUID) error {
	node, err := q.GetNode(ctx, nodeID)
	if err != nil {
		return ValidationError{"node_ids": "unknown node " + nodeID.String()}
	}
	count, err := q.CountNodeProfiles(ctx, nodeID)
	if err != nil {
		return err
	}
	if count >= int64(node.MaxProfiles) {
		return fmt.Errorf("%w: %s has %d/%d profiles", ErrCapacity, node.Hostname, count, node.MaxProfiles)
	}
	p, err := q.CreateProfile(ctx, db.CreateProfileParams{
		NodeID: nodeID, AccessKeyID: uuid.NullUUID{UUID: key.ID, Valid: true}, Name: domain.ProfileName(key.ID),
		SecretEnc: key.SecretEnc, Backend: "127.0.0.1:2398", CarrierMode: key.CarrierMode, Limits: key.Limits,
	})
	if err != nil {
		return err
	}
	if err := q.CreateBinding(ctx, db.CreateBindingParams{AccessKeyID: key.ID, NodeID: nodeID, ProfileID: p.ID}); err != nil {
		return err
	}
	return q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: nodeID, Dirty: true})
}

func (s *Service) Bind(ctx context.Context, keyID, nodeID uuid.UUID) error {
	key, err := s.st.Q.GetKey(ctx, keyID)
	if err != nil {
		return ErrNotFound
	}
	if key.Status == db.KeyStatusRevoked {
		return ValidationError{"status": "key is revoked"}
	}
	return s.st.Tx(ctx, func(q *db.Queries) error { return s.bindTx(ctx, q, key, nodeID) })
}

func (s *Service) Unbind(ctx context.Context, keyID, nodeID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		bindings, err := q.ListKeyBindings(ctx, keyID)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			if b.NodeID == nodeID {
				if err := q.DeleteBinding(ctx, db.DeleteBindingParams{AccessKeyID: keyID, NodeID: nodeID}); err != nil {
					return err
				}
				if err := q.DeleteProfile(ctx, b.ProfileID); err != nil {
					return err
				}
				return q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: nodeID, Dirty: true})
			}
		}
		return nil
	})
}

func (s *Service) dirtyKeyNodes(ctx context.Context, q *db.Queries, keyID uuid.UUID) error {
	bindings, err := q.ListKeyBindings(ctx, keyID)
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if err := q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: b.NodeID, Dirty: true}); err != nil {
			return err
		}
	}
	return nil
}

// Revoke removes the key's profiles from all nodes and marks it revoked. Idempotent.
func (s *Service) Revoke(ctx context.Context, keyID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetKey(ctx, keyID); err != nil {
			return ErrNotFound
		}
		if err := s.dirtyKeyNodes(ctx, q, keyID); err != nil {
			return err
		}
		if err := q.DeleteProfilesByKey(ctx, uuid.NullUUID{UUID: keyID, Valid: true}); err != nil {
			return err
		}
		return q.SetKeyStatus(ctx, db.SetKeyStatusParams{ID: keyID, Status: db.KeyStatusRevoked})
	})
}

// Rotate issues a new secret; all holders of the old one lose access after apply.
func (s *Service) Rotate(ctx context.Context, keyID uuid.UUID) (db.AccessKey, error) {
	secret, err := crypto.NewSecretHex()
	if err != nil {
		return db.AccessKey{}, err
	}
	enc, err := s.box.EncryptString(secret)
	if err != nil {
		return db.AccessKey{}, err
	}
	var key db.AccessKey
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetKey(ctx, keyID); err != nil {
			return ErrNotFound
		}
		if err := q.SetKeySecret(ctx, db.SetKeySecretParams{ID: keyID, SecretEnc: enc}); err != nil {
			return err
		}
		profiles, err := q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: keyID, Valid: true})
		if err != nil {
			return err
		}
		for _, p := range profiles {
			if err := q.UpdateProfileSecret(ctx, db.UpdateProfileSecretParams{ID: p.ID, SecretEnc: enc}); err != nil {
				return err
			}
		}
		if err := s.dirtyKeyNodes(ctx, q, keyID); err != nil {
			return err
		}
		key, err = q.GetKey(ctx, keyID)
		return err
	})
	return key, err
}

func (s *Service) Delete(ctx context.Context, keyID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		if err := s.dirtyKeyNodes(ctx, q, keyID); err != nil {
			return err
		}
		return q.DeleteKey(ctx, keyID) // profiles/bindings cascade
	})
}

func (s *Service) Update(ctx context.Context, keyID uuid.UUID, label, ownerLabel, note string, expiresAt *time.Time, carrier domain.CarrierMode, limits domain.ProfileLimits) (db.AccessKey, error) {
	if strings.TrimSpace(label) == "" {
		return db.AccessKey{}, ValidationError{"label": "required"}
	}
	if !carrier.Valid() {
		return db.AccessKey{}, ValidationError{"carrier_mode": "invalid"}
	}
	if err := limits.Validate(); err != nil {
		return db.AccessKey{}, ValidationError{"limits": err.Error()}
	}
	raw, _ := json.Marshal(limits)
	var key db.AccessKey
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		old, err := q.GetKey(ctx, keyID)
		if err != nil {
			return ErrNotFound
		}
		key, err = q.UpdateKey(ctx, db.UpdateKeyParams{ID: keyID, Label: label, OwnerLabel: ownerLabel, Note: note, ExpiresAt: expiresAt, CarrierMode: string(carrier), Limits: raw})
		if err != nil {
			return err
		}
		if old.CarrierMode != string(carrier) || string(old.Limits) != string(raw) {
			profiles, err := q.ListProfilesByKey(ctx, uuid.NullUUID{UUID: keyID, Valid: true})
			if err != nil {
				return err
			}
			for _, p := range profiles {
				if err := q.UpdateProfileSettings(ctx, db.UpdateProfileSettingsParams{ID: p.ID, CarrierMode: string(carrier), Limits: raw}); err != nil {
					return err
				}
			}
			return s.dirtyKeyNodes(ctx, q, keyID)
		}
		return nil
	})
	return key, err
}

func (s *Service) Secret(ctx context.Context, key db.AccessKey) (string, error) {
	return s.box.DecryptString(key.SecretEnc)
}

type Link struct {
	NodeID   uuid.UUID `json:"node_id"`
	NodeName string    `json:"node_name"`
	Hostname string    `json:"hostname"`
	TMe      string    `json:"tme"`
	Tg       string    `json:"tg"`
}

func (s *Service) Links(ctx context.Context, keyID uuid.UUID) ([]Link, error) {
	key, err := s.st.Q.GetKey(ctx, keyID)
	if err != nil {
		return nil, ErrNotFound
	}
	secret, err := s.Secret(ctx, key)
	if err != nil {
		return nil, err
	}
	bindings, err := s.st.Q.ListKeyBindings(ctx, keyID)
	if err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, Link{NodeID: b.NodeID, NodeName: b.NodeName, Hostname: b.Hostname, TMe: qrlink.TMe(b.Hostname, secret), Tg: qrlink.Tg(b.Hostname, secret)})
	}
	return out, nil
}
```

- [ ] **Step 5: Run service tests**

Run: `go test ./internal/keys/ -v`
Expected: PASS.

- [ ] **Step 6: Write failing API tests**

`internal/api/keys_test.go`:
```go
package api_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
)

type keyResp struct {
	ID     uuid.UUID `json:"id"`
	Label  string    `json:"label"`
	Type   string    `json:"type"`
	Status string    `json:"status"`
	Secret string    `json:"secret"`
	Links  []struct {
		Hostname string `json:"hostname"`
		TMe      string `json:"tme"`
	} `json:"links"`
	Nodes []struct {
		NodeID uuid.UUID `json:"node_id"`
	} `json:"nodes"`
}

func ownerWithNode(t *testing.T) (*apitest.Harness, *apitest.Client, nodeResp) {
	t.Helper()
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createNode(t, c, "n1.test")
	return h, c, n
}

func TestCreateKeyAndFetch(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var k keyResp
	resp := c.Post("/api/v1/keys", map[string]any{"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)
	if k.Status != "pending" || len(k.Links) != 1 || k.Links[0].Hostname != "n1.test" || len(k.Secret) != 32 {
		t.Fatalf("key %+v", k)
	}
	var got keyResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.Label != "Ivan" || len(got.Nodes) != 1 {
		t.Fatalf("get %+v", got)
	}
	png := c.Get("/api/v1/keys/" + k.ID.String() + "/qr?node=" + n.ID.String())
	body, _ := io.ReadAll(png.Body)
	if png.StatusCode != 200 || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("qr %d", png.StatusCode)
	}
}

func TestListKeysFilters(t *testing.T) {
	_, c, n := ownerWithNode(t)
	for _, in := range []map[string]any{
		{"label": "a", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}},
		{"label": "team", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}},
	} {
		if resp := c.Post("/api/v1/keys", in); resp.StatusCode != 201 {
			t.Fatalf("create %d", resp.StatusCode)
		}
	}
	var list struct {
		Items []keyResp `json:"items"`
		Total int       `json:"total"`
	}
	c.JSON(c.Get("/api/v1/keys?type=SHARED"), &list)
	if list.Total != 1 || list.Items[0].Label != "team" {
		t.Fatalf("filter type: %+v", list)
	}
	c.JSON(c.Get("/api/v1/keys?q=tea"), &list)
	if list.Total != 1 {
		t.Fatalf("filter q: %+v", list)
	}
	c.JSON(c.Get("/api/v1/keys?per_page=1"), &list)
	if list.Total != 2 || len(list.Items) != 1 {
		t.Fatalf("pagination: %+v", list)
	}
}

func TestBatchRevokeBulk(t *testing.T) {
	_, c, n := ownerWithNode(t)
	var batch struct {
		Items []keyResp `json:"items"`
	}
	resp := c.Post("/api/v1/keys/batch", map[string]any{"prefix": "vip", "count": 3, "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	if resp.StatusCode != 201 {
		t.Fatalf("batch %d", resp.StatusCode)
	}
	c.JSON(resp, &batch)
	if len(batch.Items) != 3 {
		t.Fatalf("batch items %d", len(batch.Items))
	}
	if resp := c.Post("/api/v1/keys/"+batch.Items[0].ID.String()+"/revoke", nil); resp.StatusCode != 200 {
		t.Fatalf("revoke %d", resp.StatusCode)
	}
	resp = c.Post("/api/v1/keys/bulk", map[string]any{"action": "revoke", "ids": []string{batch.Items[1].ID.String(), batch.Items[2].ID.String()}})
	if resp.StatusCode != 200 {
		t.Fatalf("bulk %d", resp.StatusCode)
	}
	var list struct{ Total int `json:"total"` }
	c.JSON(c.Get("/api/v1/keys?status=revoked"), &list)
	if list.Total != 3 {
		t.Fatalf("revoked total %d", list.Total)
	}
}

func TestViewerSeesNoSecret(t *testing.T) {
	h, c, n := ownerWithNode(t)
	var k keyResp
	c.JSON(c.Post("/api/v1/keys", map[string]any{"label": "x", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}}), &k)
	h.CreateAdmin("v", "pass-123456", "viewer")
	v := h.Login("v", "pass-123456")
	var got keyResp
	v.JSON(v.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.Secret != "" || len(got.Links) != 0 {
		t.Fatalf("viewer must not see secret/links: %+v", got)
	}
}

func TestCapacityReturns409(t *testing.T) {
	_, c, n := ownerWithNode(t)
	c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"max_profiles": 1})
	resp := c.Post("/api/v1/keys", map[string]any{"label": "x", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 7: Implement keys API**

`internal/api/keys.go`:
```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/qrlink"
	"tgwebproxy/internal/store/db"
)

var clientSupport = map[string]string{"desktop": "stable", "android": "experimental", "ios": "planned"}

func (s *Server) mountKeys(r chi.Router) {
	r.Get("/keys", s.handleListKeys)
	r.With(RequireRole(writers...)).Post("/keys", s.handleCreateKey)
	r.With(RequireRole(writers...)).Post("/keys/batch", s.handleBatchKeys)
	r.With(RequireRole(writers...)).Post("/keys/bulk", s.handleBulkKeys)
	r.Route("/keys/{id}", func(r chi.Router) {
		r.Get("/", s.handleGetKey)
		r.With(RequireRole(writers...)).Patch("/", s.handlePatchKey)
		r.With(RequireRole(writers...)).Delete("/", s.handleDeleteKey)
		r.With(RequireRole(writers...)).Post("/revoke", s.handleRevokeKey)
		r.With(RequireRole(writers...)).Post("/rotate", s.handleRotateKey)
		r.With(RequireRole(writers...)).Get("/links", s.handleKeyLinks)
		r.With(RequireRole(writers...)).Get("/qr", s.handleKeyQR)
		r.With(RequireRole(writers...)).Post("/bindings", s.handleBindKey)
		r.With(RequireRole(writers...)).Delete("/bindings/{node}", s.handleUnbindKey)
	})
}

type keyJSON struct {
	ID            uuid.UUID         `json:"id"`
	Label         string            `json:"label"`
	Type          string            `json:"type"`
	OwnerLabel    string            `json:"owner_label"`
	Status        string            `json:"status"`
	CarrierMode   string            `json:"carrier_mode"`
	Limits        json.RawMessage   `json:"limits"`
	ExpiresAt     *time.Time        `json:"expires_at"`
	RevokedAt     *time.Time        `json:"revoked_at"`
	Note          string            `json:"note"`
	CreatedAt     time.Time         `json:"created_at"`
	Nodes         []keyNodeJSON     `json:"nodes"`
	Secret        string            `json:"secret,omitempty"`
	Links         []keys.Link       `json:"links,omitempty"`
	ClientSupport map[string]string `json:"client_support"`
}

type keyNodeJSON struct {
	NodeID   uuid.UUID `json:"node_id"`
	NodeName string    `json:"node_name"`
	Hostname string    `json:"hostname"`
	Sync     string    `json:"profile_sync"`
}

func (s *Server) keyJSON(r *http.Request, k db.AccessKey, withSecret bool) keyJSON {
	out := keyJSON{ID: k.ID, Label: k.Label, Type: string(k.Type), OwnerLabel: k.OwnerLabel, Status: string(k.Status), CarrierMode: k.CarrierMode,
		Limits: k.Limits, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt, Note: k.Note, CreatedAt: k.CreatedAt, Nodes: []keyNodeJSON{}, ClientSupport: clientSupport}
	bindings, _ := s.store.Q.ListKeyBindings(r.Context(), k.ID)
	for _, b := range bindings {
		out.Nodes = append(out.Nodes, keyNodeJSON{NodeID: b.NodeID, NodeName: b.NodeName, Hostname: b.Hostname, Sync: string(b.SyncState)})
	}
	if withSecret && k.Status != db.KeyStatusRevoked {
		out.Secret, _ = s.keys.Secret(r.Context(), k)
		out.Links, _ = s.keys.Links(r.Context(), k.ID)
	}
	return out
}

func isWriter(r *http.Request) bool {
	p, _ := PrincipalFrom(r.Context())
	return p.Role == RoleOwner || p.Role == RoleAdmin
}

func (s *Server) keysErr(w http.ResponseWriter, err error) {
	var ve keys.ValidationError
	switch {
	case errors.As(err, &ve):
		validation(w, ve)
	case errors.Is(err, keys.ErrCapacity):
		conflict(w, err.Error())
	case errors.Is(err, keys.ErrNotFound):
		notFound(w)
	default:
		s.log.Error("keys", "err", err)
		internal(w)
	}
}

type keyInput struct {
	Label       string               `json:"label"`
	Type        string               `json:"type"`
	OwnerLabel  string               `json:"owner_label"`
	Note        string               `json:"note"`
	CarrierMode string               `json:"carrier_mode"`
	Limits      domain.ProfileLimits `json:"limits"`
	ExpiresAt   *time.Time           `json:"expires_at"`
	NodeIDs     []uuid.UUID          `json:"node_ids"`
	Prefix      string               `json:"prefix"`
	Count       int                  `json:"count"`
}

func (in keyInput) toCreate(by uuid.UUID) keys.CreateInput {
	cm := in.CarrierMode
	if cm == "" {
		cm = "https"
	}
	return keys.CreateInput{Label: in.Label, OwnerLabel: in.OwnerLabel, Note: in.Note, Type: domain.KeyType(in.Type),
		CarrierMode: domain.CarrierMode(cm), Limits: in.Limits, ExpiresAt: in.ExpiresAt, NodeIDs: in.NodeIDs, CreatedBy: by}
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var in keyInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	p, _ := PrincipalFrom(r.Context())
	k, err := s.keys.Create(r.Context(), in.toCreate(p.UserID))
	if err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.create", "key", k.ID.String(), map[string]any{"label": k.Label, "type": k.Type})
	writeJSON(w, 201, s.keyJSON(r, k, true))
}

func (s *Server) handleBatchKeys(w http.ResponseWriter, r *http.Request) {
	var in keyInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	p, _ := PrincipalFrom(r.Context())
	ks, err := s.keys.CreateBatch(r.Context(), in.toCreate(p.UserID), in.Prefix, in.Count)
	if err != nil {
		s.keysErr(w, err)
		return
	}
	items := make([]keyJSON, 0, len(ks))
	for _, k := range ks {
		items = append(items, s.keyJSON(r, k, true))
	}
	s.Audit(r.Context(), "key.batch_create", "key", "", map[string]any{"prefix": in.Prefix, "count": len(ks)})
	writeJSON(w, 201, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per < 1 || per > 200 {
		per = 50
	}
	var typ db.NullKeyType
	if v := q.Get("type"); v != "" {
		typ = db.NullKeyType{KeyType: db.KeyType(v), Valid: true}
	}
	var status db.NullKeyStatus
	if v := q.Get("status"); v != "" {
		status = db.NullKeyStatus{KeyStatus: db.KeyStatus(v), Valid: true}
	}
	var nodeID uuid.NullUUID
	if id, err := uuid.Parse(q.Get("node")); err == nil {
		nodeID = uuid.NullUUID{UUID: id, Valid: true}
	}
	var search *string
	if v := q.Get("q"); v != "" {
		search = &v
	}
	rows, err := s.store.Q.ListKeys(r.Context(), db.ListKeysParams{Limit: int32(per), Offset: int32((page - 1) * per), Type: typ, Status: status, NodeID: nodeID, Q: search})
	if err != nil {
		s.log.Error("list keys", "err", err)
		internal(w)
		return
	}
	total, _ := s.store.Q.CountKeys(r.Context(), db.CountKeysParams{Type: typ, Status: status, NodeID: nodeID, Q: search})
	items := make([]keyJSON, 0, len(rows))
	for _, k := range rows {
		items = append(items, s.keyJSON(r, k, false))
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "per_page": per})
}

func (s *Server) loadKey(w http.ResponseWriter, r *http.Request) (db.AccessKey, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.AccessKey{}, false
	}
	k, err := s.store.Q.GetKey(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.AccessKey{}, false
	}
	return k, true
}

func (s *Server) handleGetKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, s.keyJSON(r, k, isWriter(r)))
}

func (s *Server) handlePatchKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	var in struct {
		Label       *string               `json:"label"`
		OwnerLabel  *string               `json:"owner_label"`
		Note        *string               `json:"note"`
		CarrierMode *string               `json:"carrier_mode"`
		Limits      *domain.ProfileLimits `json:"limits"`
		ExpiresAt   *time.Time            `json:"expires_at"`
		ClearExpiry bool                  `json:"clear_expiry"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	label, owner, note, cm := k.Label, k.OwnerLabel, k.Note, k.CarrierMode
	var limits domain.ProfileLimits
	_ = json.Unmarshal(k.Limits, &limits)
	exp := k.ExpiresAt
	if in.Label != nil {
		label = *in.Label
	}
	if in.OwnerLabel != nil {
		owner = *in.OwnerLabel
	}
	if in.Note != nil {
		note = *in.Note
	}
	if in.CarrierMode != nil {
		cm = *in.CarrierMode
	}
	if in.Limits != nil {
		limits = *in.Limits
	}
	if in.ExpiresAt != nil {
		exp = in.ExpiresAt
	}
	if in.ClearExpiry {
		exp = nil
	}
	updated, err := s.keys.Update(r.Context(), k.ID, label, owner, note, exp, domain.CarrierMode(cm), limits)
	if err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.update", "key", k.ID.String(), nil)
	writeJSON(w, 200, s.keyJSON(r, updated, true))
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	if err := s.keys.Delete(r.Context(), k.ID); err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.delete", "key", k.ID.String(), map[string]any{"label": k.Label})
	w.WriteHeader(204)
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	if err := s.keys.Revoke(r.Context(), k.ID); err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.revoke", "key", k.ID.String(), map[string]any{"label": k.Label, "type": k.Type})
	k, _ = s.store.Q.GetKey(r.Context(), k.ID)
	writeJSON(w, 200, s.keyJSON(r, k, false))
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	updated, err := s.keys.Rotate(r.Context(), k.ID)
	if err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.rotate", "key", k.ID.String(), map[string]any{"label": k.Label})
	writeJSON(w, 200, s.keyJSON(r, updated, true))
}

func (s *Server) handleKeyLinks(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	links, err := s.keys.Links(r.Context(), k.ID)
	if err != nil {
		s.keysErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": links, "client_support": clientSupport})
}

func (s *Server) handleKeyQR(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	nodeID, err := uuid.Parse(r.URL.Query().Get("node"))
	if err != nil {
		badRequest(w, "node query parameter required")
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 128 || size > 1024 {
		size = 256
	}
	links, err := s.keys.Links(r.Context(), k.ID)
	if err != nil {
		s.keysErr(w, err)
		return
	}
	for _, l := range links {
		if l.NodeID == nodeID {
			png, err := qrlink.PNG(l.TMe, size)
			if err != nil {
				internal(w)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", "inline; filename=\""+k.Label+"-"+l.Hostname+".png\"")
			_, _ = w.Write(png)
			return
		}
	}
	notFound(w)
}

func (s *Server) handleBindKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	var in struct {
		NodeID uuid.UUID `json:"node_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.keys.Bind(r.Context(), k.ID, in.NodeID); err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.bind", "key", k.ID.String(), map[string]any{"node_id": in.NodeID})
	k, _ = s.store.Q.GetKey(r.Context(), k.ID)
	writeJSON(w, 200, s.keyJSON(r, k, true))
}

func (s *Server) handleUnbindKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	nodeID, err := uuid.Parse(chi.URLParam(r, "node"))
	if err != nil {
		notFound(w)
		return
	}
	if err := s.keys.Unbind(r.Context(), k.ID, nodeID); err != nil {
		s.keysErr(w, err)
		return
	}
	s.Audit(r.Context(), "key.unbind", "key", k.ID.String(), map[string]any{"node_id": nodeID})
	w.WriteHeader(204)
}

func (s *Server) handleBulkKeys(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action    string      `json:"action"`
		IDs       []uuid.UUID `json:"ids"`
		ExpiresAt *time.Time  `json:"expires_at"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(in.IDs) == 0 || len(in.IDs) > 500 {
		validation(w, map[string]string{"ids": "1..500 ids"})
		return
	}
	done, failed := 0, map[string]string{}
	for _, id := range in.IDs {
		var err error
		switch in.Action {
		case "revoke":
			err = s.keys.Revoke(r.Context(), id)
		case "delete":
			err = s.keys.Delete(r.Context(), id)
		case "extend":
			if in.ExpiresAt == nil {
				validation(w, map[string]string{"expires_at": "required for extend"})
				return
			}
			err = s.store.Q.SetKeyExpiry(r.Context(), db.SetKeyExpiryParams{ID: id, ExpiresAt: in.ExpiresAt})
		default:
			validation(w, map[string]string{"action": "revoke, delete or extend"})
			return
		}
		if err != nil {
			failed[id.String()] = err.Error()
		} else {
			done++
		}
	}
	s.Audit(r.Context(), "key.bulk_"+in.Action, "key", "", map[string]any{"count": done})
	writeJSON(w, 200, map[string]any{"done": done, "failed": failed})
}
```
Add `keys *keys.Service` to `Server`/`Deps` and call `s.mountKeys(r)` from `mountProtected`. In `apitest.New`, set `deps.Keys = keys.New(st, box)`.

- [ ] **Step 8: Run tests**

Run: `go test ./internal/api/ -run Key -v && go test ./... && golangci-lint run ./...`
Expected: PASS.

---

### Task 11: Workers — apply, expiry, stats/offline; apply jobs API

**Files:**
- Create: `internal/store/queries/apply.sql`, `internal/store/queries/stats.sql`
- Create: `internal/worker/desired.go`, `internal/worker/apply.go`, `internal/worker/expiry.go`, `internal/worker/stats.go`, `internal/worker/runner.go`
- Test: `internal/worker/desired_test.go`, `internal/worker/apply_test.go`, `internal/worker/expiry_test.go`, `internal/worker/stats_test.go`
- Modify: `internal/api/nodes.go` (`GET /nodes/{id}/jobs`), `internal/api/server.go`, `cmd/panel/main.go`

**Interfaces:**
- Produces (package `worker`):
```go
func DesiredState(ctx, st *store.Store, box *crypto.Box, nodeID uuid.UUID) (nodedriver.ApplyRequest, error) // profiles (default first), secrets, site (only if node_sites.bundle_hash != deployed_hash)
type Apply struct{...}
func NewApply(st, box, driver nodedriver.Driver, interval time.Duration, log) *Apply
func (a *Apply) Trigger(nodeID uuid.UUID)              // non-blocking; used by API "apply now"
func (a *Apply) ApplyNode(ctx, nodeID) error           // synchronous single pass for one node
func (a *Apply) Run(ctx)                               // ticker loop over ListDirtyNodes + triggers
type Expiry struct{}; func NewExpiry(st, keys *keys.Service, log) *Expiry; func (e *Expiry) RunOnce(ctx) (int, error); func (e *Expiry) Run(ctx, every time.Duration)
type Stats struct{}; func NewStats(st, driver, offlineAfter time.Duration, log) *Stats; func (s *Stats) RunOnce(ctx) error; func (s *Stats) Run(ctx, every time.Duration)
func ParseRelayMetrics(text string) RelayMetrics  // struct{SessionsLive, StreamsLive int; BytesUp, BytesDown, SessionsCreated, LimitHits int64}
func Start(ctx, Apply, Expiry, Stats)  // goroutines
```
- API: `GET /nodes/{id}/jobs?limit=20` → `{items:[{id,status,kind,started_at,finished_at,error,log,created_at}]}`.

- [ ] **Step 1: Queries**

`internal/store/queries/apply.sql`:
```sql
-- name: CreateApplyJob :one
INSERT INTO apply_jobs (node_id, kind, status, started_at) VALUES ($1, $2, 'running', now()) RETURNING *;

-- name: FinishApplyJob :exec
UPDATE apply_jobs SET status = $2, finished_at = now(), error = $3, log = $4 WHERE id = $1;

-- name: ListNodeApplyJobs :many
SELECT * FROM apply_jobs WHERE node_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: GetNodeSite :one
SELECT * FROM node_sites WHERE node_id = $1;

-- name: SetNodeSiteDeployed :exec
UPDATE node_sites SET deployed_hash = $2 WHERE node_id = $1;
```

`internal/store/queries/stats.sql`:
```sql
-- name: InsertSnapshot :exec
INSERT INTO node_stats_snapshots (node_id, sessions_live, streams_live, bytes_up, bytes_down, sessions_created, limit_hits, mtproxy_raw, relay_raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListSnapshots :many
SELECT * FROM node_stats_snapshots WHERE node_id = $1 AND taken_at >= $2 AND taken_at <= $3 ORDER BY taken_at;

-- name: LatestSnapshots :many
SELECT DISTINCT ON (node_id) * FROM node_stats_snapshots ORDER BY node_id, taken_at DESC;

-- name: DeleteOldSnapshots :exec
DELETE FROM node_stats_snapshots WHERE taken_at < $1;

-- name: InsertAlert :one
INSERT INTO alerts (node_id, kind, message) VALUES ($1, $2, $3) RETURNING *;

-- name: ResolveNodeAlerts :exec
UPDATE alerts SET resolved_at = now() WHERE node_id = $1 AND kind = $2 AND resolved_at IS NULL;

-- name: ListOpenAlerts :many
SELECT a.*, n.name AS node_name FROM alerts a LEFT JOIN nodes n ON n.id = a.node_id WHERE a.resolved_at IS NULL ORDER BY a.created_at DESC;

-- name: ResolveAlert :exec
UPDATE alerts SET resolved_at = now() WHERE id = $1;
```

Run `sqlc generate`.

- [ ] **Step 2: Write failing tests**

`internal/worker/desired_test.go`:
```go
package worker_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

type fixture struct {
	st   *store.Store
	box  *crypto.Box
	keys *keys.Service
	node db.Node
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := store.OpenTest(t)
	box, _ := crypto.NewBox(1, map[int][]byte{1: bytes.Repeat([]byte{1}, 32)})
	ctx := context.Background()
	n, _ := st.Q.CreateNode(ctx, db.CreateNodeParams{Name: "n", Hostname: "n.test"})
	enc, _ := box.EncryptString("00000000000000000000000000000000")
	_, _ = st.Q.CreateProfile(ctx, db.CreateProfileParams{NodeID: n.ID, Name: "default", SecretEnc: enc, Backend: "127.0.0.1:2398", CarrierMode: "https", Limits: []byte("{}")})
	return &fixture{st: st, box: box, keys: keys.New(st, box), node: n}
}

func TestDesiredStateIncludesDefaultAndKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "websocket", Limits: domain.ProfileLimits{MaxSessions: 3}, NodeIDs: []uuid.UUID{f.node.ID}})
	req, err := worker.DesiredState(ctx, f.st, f.box, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !req.ApplyProfiles || len(req.Profiles) != 2 || req.Profiles[0].Name != "default" || req.Profiles[1].Name != domain.ProfileName(k.ID) {
		t.Fatalf("profiles %+v", req.Profiles)
	}
	if req.Profiles[1].CarrierMode != "websocket" || req.Profiles[1].Limits == nil || req.Profiles[1].Limits.MaxSessions != 3 {
		t.Fatalf("profile settings lost: %+v", req.Profiles[1])
	}
	if len(req.MTProxySecrets) != 2 || req.MTProxySecrets[0] != "00000000000000000000000000000000" {
		t.Fatalf("secrets %v", req.MTProxySecrets)
	}
	if req.Site != nil {
		t.Fatal("no site assigned, Site must be nil")
	}
}

func TestDesiredStateOmitsRevoked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	_ = f.keys.Revoke(ctx, k.ID)
	req, _ := worker.DesiredState(ctx, f.st, f.box, f.node.ID)
	if len(req.Profiles) != 1 || len(req.MTProxySecrets) != 1 {
		t.Fatalf("revoked key leaked: %+v", req)
	}
}
```

`internal/worker/apply_test.go`:
```go
package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestApplyNodeHappyPath(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})

	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if len(mock.Applied(f.node.ID)) != 1 {
		t.Fatal("driver not called")
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	if n.Dirty || n.LastApplyAt == nil {
		t.Fatalf("node not marked applied: %+v", n)
	}
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status != db.KeyStatusActive {
		t.Fatalf("key status %s", kk.Status)
	}
	profiles, _ := f.st.Q.ListNodeProfiles(ctx, f.node.ID)
	for _, p := range profiles {
		if p.SyncState != db.SyncStateSynced {
			t.Fatalf("profile %s sync %s", p.Name, p.SyncState)
		}
	}
	jobs, _ := f.st.Q.ListNodeApplyJobs(ctx, db.ListNodeApplyJobsParams{NodeID: f.node.ID, Limit: 10})
	if len(jobs) != 1 || jobs[0].Status != db.ApplyStatusOk {
		t.Fatalf("jobs %+v", jobs)
	}
}

func TestApplyNodeFailureKeepsDirtyAndPending(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.FailNextApply(f.node.ID, "relay -check rejected profiles")
	k, _ := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{f.node.ID}})
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err == nil {
		t.Fatal("expected error")
	}
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if !n.Dirty || kk.Status != db.KeyStatusPending {
		t.Fatalf("state after failure: dirty=%v key=%s", n.Dirty, kk.Status)
	}
	jobs, _ := f.st.Q.ListNodeApplyJobs(ctx, db.ListNodeApplyJobsParams{NodeID: f.node.ID, Limit: 10})
	if jobs[0].Status != db.ApplyStatusRolledBack || jobs[0].Error == "" {
		t.Fatalf("job %+v", jobs[0])
	}
}

func TestApplyOfflineNodeSkipped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	if err := a.ApplyNode(ctx, f.node.ID); err == nil {
		t.Fatal("expected offline error")
	}
	if len(mock.Applied(f.node.ID)) != 0 {
		t.Fatal("must not call driver")
	}
}

func TestTriggerRunsApply(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	_ = f.st.Q.SetNodeStatus(ctx, db.SetNodeStatusParams{ID: f.node.ID, Status: db.NodeStatusOnline})
	_ = f.st.Q.SetNodeDirty(ctx, db.SetNodeDirtyParams{ID: f.node.ID, Dirty: true})
	a := worker.NewApply(f.st, f.box, mock, time.Hour, slog.New(slog.DiscardHandler))
	go a.Run(ctx)
	a.Trigger(f.node.ID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(mock.Applied(f.node.ID)) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("trigger did not apply")
}
```

`internal/worker/expiry_test.go`:
```go
package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestExpiryRevokesExpiredKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	soon := time.Now().Add(time.Second)
	k, err := f.keys.Create(ctx, keys.CreateInput{Label: "a", Type: domain.KeyPersonal, CarrierMode: "https", ExpiresAt: &soon, NodeIDs: []uuid.UUID{f.node.ID}})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	_ = f.st.Q.SetKeyExpiry(ctx, db.SetKeyExpiryParams{ID: k.ID, ExpiresAt: &past})
	n, err := worker.NewExpiry(f.st, f.keys, slog.New(slog.DiscardHandler)).RunOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expired %d err %v", n, err)
	}
	kk, _ := f.st.Q.GetKey(ctx, k.ID)
	if kk.Status != db.KeyStatusRevoked {
		t.Fatalf("status %s", kk.Status)
	}
}
```

`internal/worker/stats_test.go`:
```go
package worker_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/worker"
)

func TestParseRelayMetrics(t *testing.T) {
	text := "# HELP tproxy_sessions_live x\n# TYPE tproxy_sessions_live gauge\ntproxy_sessions_live 4\ntproxy_streams_live 12\ntproxy_bytes_up_total 1000\ntproxy_bytes_down_total 2000\ntproxy_sessions_created_total 7\ntproxy_limit_hits_total 1\n"
	m := worker.ParseRelayMetrics(text)
	if m.SessionsLive != 4 || m.StreamsLive != 12 || m.BytesUp != 1000 || m.BytesDown != 2000 || m.SessionsCreated != 7 || m.LimitHits != 1 {
		t.Fatalf("parsed %+v", m)
	}
}

func TestStatsSnapshotAndOffline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mock := nodedriver.NewMock()
	mock.SetOnline(f.node.ID, true)
	mock.SetMetrics(f.node.ID, "tproxy_sessions_live 2\ntproxy_streams_live 5\n")
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	s := worker.NewStats(f.st, mock, 90*time.Second, slog.New(slog.DiscardHandler))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	snaps, _ := f.st.Q.LatestSnapshots(ctx)
	if len(snaps) != 1 || snaps[0].SessionsLive != 2 || snaps[0].StreamsLive != 5 {
		t.Fatalf("snapshots %+v", snaps)
	}
	// simulate stale heartbeat
	_, _ = f.st.Pool.Exec(ctx, `UPDATE nodes SET last_seen_at = now() - interval '10 minutes'`)
	_ = s.RunOnce(ctx)
	n, _ := f.st.Q.GetNode(ctx, f.node.ID)
	alerts, _ := f.st.Q.ListOpenAlerts(ctx)
	if n.Status != db.NodeStatusOffline || len(alerts) != 1 || alerts[0].Kind != "node_offline" {
		t.Fatalf("offline detection: status=%s alerts=%d", n.Status, len(alerts))
	}
	// back online resolves the alert
	_ = f.st.Q.SetNodeOnline(ctx, db.SetNodeOnlineParams{ID: f.node.ID})
	_ = s.RunOnce(ctx)
	alerts, _ = f.st.Q.ListOpenAlerts(ctx)
	if len(alerts) != 0 {
		t.Fatalf("alert not resolved: %+v", alerts)
	}
}
```

- [ ] **Step 3: Run to verify fail**

Run: `go test ./internal/worker/` → compile errors.

- [ ] **Step 4: Implement desired.go**

`internal/worker/desired.go`:
```go
// Package worker runs background jobs: apply, expiry, stats, offline detection.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
)

// DesiredState builds the full ApplyRequest for a node from the database.
func DesiredState(ctx context.Context, st *store.Store, box *crypto.Box, nodeID uuid.UUID) (nodedriver.ApplyRequest, error) {
	rows, err := st.Q.ListNodeProfiles(ctx, nodeID)
	if err != nil {
		return nodedriver.ApplyRequest{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name == "default" && rows[j].Name != "default" })
	req := nodedriver.ApplyRequest{ApplyProfiles: true}
	seen := map[string]bool{}
	for _, p := range rows {
		secret, err := box.DecryptString(p.SecretEnc)
		if err != nil {
			return req, fmt.Errorf("decrypt profile %s: %w", p.Name, err)
		}
		prof := nodedriver.Profile{Name: p.Name, Secret: secret, Backend: p.Backend, CarrierMode: p.CarrierMode}
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
	site, err := st.Q.GetNodeSite(ctx, nodeID)
	if err == nil && (site.DeployedHash == nil || *site.DeployedHash != site.BundleHash) {
		files := map[string]string{}
		if err := json.Unmarshal(site.Bundle, &files); err != nil {
			return req, fmt.Errorf("site bundle: %w", err)
		}
		bundle := nodedriver.SiteBundle{Files: map[string][]byte{}}
		for p, b64 := range files {
			raw, err := decodeB64(b64)
			if err != nil {
				return req, fmt.Errorf("site file %s: %w", p, err)
			}
			bundle.Files[p] = raw
		}
		req.Site = &bundle
	}
	return req, nil
}
```
Add `decodeB64` using `encoding/base64.StdEncoding.DecodeString`. Site bundles are stored as `{path: base64}` JSON (Task 12 writes them in this format).

- [ ] **Step 5: Implement apply.go**

`internal/worker/apply.go`:
```go
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/crypto"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type Apply struct {
	st       *store.Store
	box      *crypto.Box
	driver   nodedriver.Driver
	interval time.Duration
	log      *slog.Logger
	trigger  chan uuid.UUID
	mu       sync.Mutex
	inFlight map[uuid.UUID]bool
}

func NewApply(st *store.Store, box *crypto.Box, driver nodedriver.Driver, interval time.Duration, log *slog.Logger) *Apply {
	return &Apply{st: st, box: box, driver: driver, interval: interval, log: log, trigger: make(chan uuid.UUID, 64), inFlight: map[uuid.UUID]bool{}}
}

func (a *Apply) Trigger(nodeID uuid.UUID) {
	select {
	case a.trigger <- nodeID:
	default:
	}
}

func (a *Apply) Run(ctx context.Context) {
	t := time.NewTicker(a.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-a.trigger:
			go a.applyLogged(ctx, id)
		case <-t.C:
			nodes, err := a.st.Q.ListDirtyNodes(ctx)
			if err != nil {
				a.log.Error("list dirty nodes", "err", err)
				continue
			}
			for _, n := range nodes {
				go a.applyLogged(ctx, n.ID)
			}
		}
	}
}

func (a *Apply) applyLogged(ctx context.Context, id uuid.UUID) {
	if err := a.ApplyNode(ctx, id); err != nil {
		a.log.Warn("apply failed", "node", id, "err", err)
	}
}

// ApplyNode pushes the desired state to one node and records the outcome.
func (a *Apply) ApplyNode(ctx context.Context, nodeID uuid.UUID) error {
	a.mu.Lock()
	if a.inFlight[nodeID] {
		a.mu.Unlock()
		return errors.New("apply already in progress")
	}
	a.inFlight[nodeID] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.inFlight, nodeID)
		a.mu.Unlock()
	}()

	if !a.driver.Online(nodeID) {
		return nodedriver.ErrOffline
	}
	req, err := DesiredState(ctx, a.st, a.box, nodeID)
	if err != nil {
		return err
	}
	kind := db.ApplyKindProfiles
	if req.Site != nil {
		kind = db.ApplyKindBoth
	}
	job, err := a.st.Q.CreateApplyJob(ctx, db.CreateApplyJobParams{NodeID: nodeID, Kind: kind})
	if err != nil {
		return err
	}
	res, applyErr := a.driver.Apply(ctx, nodeID, req)
	if applyErr != nil {
		status := db.ApplyStatusFailed
		if res.RolledBack {
			status = db.ApplyStatusRolledBack
		}
		_ = a.st.Q.FinishApplyJob(ctx, db.FinishApplyJobParams{ID: job.ID, Status: status, Error: applyErr.Error(), Log: res.Log})
		_ = a.st.Q.SetNodeProfilesSync(ctx, db.SetNodeProfilesSyncParams{NodeID: nodeID, SyncState: db.SyncStateFailed})
		return fmt.Errorf("apply: %w", applyErr)
	}
	return a.st.Tx(ctx, func(q *db.Queries) error {
		if err := q.FinishApplyJob(ctx, db.FinishApplyJobParams{ID: job.ID, Status: db.ApplyStatusOk, Log: res.Log}); err != nil {
			return err
		}
		if err := q.SetNodeProfilesSync(ctx, db.SetNodeProfilesSyncParams{NodeID: nodeID, SyncState: db.SyncStateSynced}); err != nil {
			return err
		}
		if req.Site != nil {
			site, err := q.GetNodeSite(ctx, nodeID)
			if err == nil {
				if err := q.SetNodeSiteDeployed(ctx, db.SetNodeSiteDeployedParams{NodeID: nodeID, DeployedHash: &site.BundleHash}); err != nil {
					return err
				}
			}
		}
		if err := q.SetNodeApplied(ctx, nodeID); err != nil {
			return err
		}
		return q.ActivatePendingKeysForNode(ctx)
	})
}
```

- [ ] **Step 6: Implement expiry.go, stats.go, runner.go**

`internal/worker/expiry.go`:
```go
package worker

import (
	"context"
	"log/slog"
	"time"

	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store"
)

type Expiry struct {
	st   *store.Store
	keys *keys.Service
	log  *slog.Logger
}

func NewExpiry(st *store.Store, k *keys.Service, log *slog.Logger) *Expiry { return &Expiry{st: st, keys: k, log: log} }

func (e *Expiry) RunOnce(ctx context.Context) (int, error) {
	rows, err := e.st.Q.ListExpiredActiveKeys(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range rows {
		if err := e.keys.Revoke(ctx, k.ID); err != nil {
			e.log.Error("expire key", "key", k.ID, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

func (e *Expiry) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := e.RunOnce(ctx); err != nil {
			e.log.Error("expiry", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
```

`internal/worker/stats.go`:
```go
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store"
	"tgwebproxy/internal/store/db"
)

type RelayMetrics struct {
	SessionsLive, StreamsLive                          int
	BytesUp, BytesDown, SessionsCreated, LimitHits     int64
}

// ParseRelayMetrics reads the handful of relay metrics we chart. Prometheus text format, no labels.
func ParseRelayMetrics(text string) RelayMetrics {
	var m RelayMetrics
	for _, line := range strings.Split(text, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		name, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			continue
		}
		switch name {
		case "tproxy_sessions_live":
			m.SessionsLive = int(f)
		case "tproxy_streams_live":
			m.StreamsLive = int(f)
		case "tproxy_bytes_up_total":
			m.BytesUp = int64(f)
		case "tproxy_bytes_down_total":
			m.BytesDown = int64(f)
		case "tproxy_sessions_created_total":
			m.SessionsCreated = int64(f)
		case "tproxy_limit_hits_total":
			m.LimitHits = int64(f)
		}
	}
	return m
}

type Stats struct {
	st           *store.Store
	driver       nodedriver.Driver
	offlineAfter time.Duration
	log          *slog.Logger
}

func NewStats(st *store.Store, driver nodedriver.Driver, offlineAfter time.Duration, log *slog.Logger) *Stats {
	return &Stats{st: st, driver: driver, offlineAfter: offlineAfter, log: log}
}

func (s *Stats) RunOnce(ctx context.Context) error {
	// 1. offline detection
	stale, err := s.st.Q.MarkStaleNodesOffline(ctx, ptrTime(time.Now().Add(-s.offlineAfter)))
	if err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := s.st.Q.InsertAlert(ctx, db.InsertAlertParams{NodeID: nullUUID(id), Kind: "node_offline", Message: "node stopped sending heartbeats"}); err != nil {
			s.log.Error("insert alert", "err", err)
		}
	}
	// 2. snapshots for online nodes; resolve offline alerts
	nodes, err := s.st.Q.ListNodes(ctx)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.Status == db.NodeStatusOffline || n.Status == db.NodeStatusPending {
			continue
		}
		_ = s.st.Q.ResolveNodeAlerts(ctx, db.ResolveNodeAlertsParams{NodeID: nullUUID(n.ID), Kind: "node_offline"})
		if !s.driver.Online(n.ID) {
			continue
		}
		text, err := s.driver.Metrics(ctx, n.ID)
		if err != nil {
			s.log.Warn("metrics", "node", n.ID, "err", err)
			continue
		}
		m := ParseRelayMetrics(text)
		stats, _ := s.driver.Stats(ctx, n.ID)
		raw, _ := json.Marshal(stats)
		if raw == nil {
			raw = []byte("{}")
		}
		if err := s.st.Q.InsertSnapshot(ctx, db.InsertSnapshotParams{NodeID: n.ID, SessionsLive: int32(m.SessionsLive), StreamsLive: int32(m.StreamsLive),
			BytesUp: m.BytesUp, BytesDown: m.BytesDown, SessionsCreated: m.SessionsCreated, LimitHits: m.LimitHits, MtproxyRaw: raw, RelayRaw: text}); err != nil {
			s.log.Error("snapshot", "err", err)
		}
	}
	return s.st.Q.DeleteOldSnapshots(ctx, time.Now().Add(-30*24*time.Hour))
}

func (s *Stats) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			s.log.Error("stats", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
```
Helpers `ptrTime(t time.Time) *time.Time` and `nullUUID(id uuid.UUID) uuid.NullUUID` in `runner.go`. Check generated param types: `MarkStaleNodesOffline` takes `*time.Time` (nullable column compare) — if sqlc generates `time.Time`, pass the value directly.

`internal/worker/runner.go`:
```go
package worker

import (
	"context"
	"time"

	"github.com/google/uuid"
)

func ptrTime(t time.Time) *time.Time     { return &t }
func nullUUID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

// Start launches all workers until ctx is cancelled.
func Start(ctx context.Context, a *Apply, e *Expiry, s *Stats) {
	go a.Run(ctx)
	go e.Run(ctx, time.Minute)
	go s.Run(ctx, time.Minute)
}
```

- [ ] **Step 7: Jobs endpoint and wiring**

Add to `internal/api/nodes.go` route `r.Get("/jobs", s.handleNodeJobs)` inside `/nodes/{id}`:
```go
func (s *Server) handleNodeJobs(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.store.Q.ListNodeApplyJobs(r.Context(), db.ListNodeApplyJobsParams{NodeID: n.ID, Limit: int32(limit)})
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}
```

In `cmd/panel/main.go` `serve()` after creating `srv` deps:
```go
keySvc := keys.New(st, box)
applyW := worker.NewApply(st, box, driver, time.Duration(cfg.ApplyInterval)*time.Second, log)
deps.Keys = keySvc
deps.ApplyNow = func(ctx context.Context, id uuid.UUID) { applyW.Trigger(id) }
worker.Start(ctx, applyW, worker.NewExpiry(st, keySvc, log), worker.NewStats(st, driver, time.Duration(cfg.OfflineAfter)*time.Second, log))
```
(build `deps` as a variable before `api.New(deps)`).

- [ ] **Step 8: Run tests**

Run: `go test ./internal/worker/ ./internal/api/ -race && go test ./... && golangci-lint run ./...`
Expected: PASS.

---

### Task 12: Site kit — validator/normaliser, first preset, templates API, node assignment

**Files:**
- Create: `internal/store/queries/sites.sql`, `internal/sitekit/normalize.go`, `internal/sitekit/presets.go`, `internal/sitekit/presets/studio/index.html`, `internal/sitekit/normalize_test.go`, `internal/api/sites.go`, `internal/api/sites_test.go`
- Modify: `internal/api/server.go` (mount, `SiteProvider` default), `cmd/panel/main.go` (seed presets, wire SiteProvider)

**Interfaces:**
- Produces (package `sitekit`):
```go
type Bundle struct { Files map[string][]byte }  // includes index.html
type Report struct { Errors []string; Warnings []string }
func Normalize(html string, assets map[string][]byte) (Bundle, Report, error) // error only on unparsable input; Report.Errors non-empty = reject
func (b Bundle) Hash() string                     // sha256 of sorted path+content
func (b Bundle) JSON() []byte                     // {path: base64}
func BundleFromJSON([]byte) (Bundle, error)
type Preset struct { Name, HTML string; Assets map[string][]byte }
func Presets() []Preset                            // phase 1: one preset "studio"
```
- API: `GET /site-templates` → `{items:[{id,name,is_preset,created_at,updated_at}]}`; `POST /site-templates` `{name, html, assets:{path:base64}}` (normalised on save; 422 with `report` on errors); `GET /site-templates/{id}` (with html+assets); `PUT /site-templates/{id}`; `DELETE /site-templates/{id}` (presets cannot be deleted); `POST /site-templates/validate` `{html, assets}` → `{report, files:[paths]}`; `POST /nodes/{id}/site` `{template_id}` → assigns (renders bundle, sets dirty); `GET /nodes/{id}/site` → `{template_id, bundle_hash, deployed_hash, files:[paths], updated_at}`; `GET /nodes/{id}/site/preview` → `text/html` of index.html with CSS inlined for preview only.

- [ ] **Step 1: Queries**

`internal/store/queries/sites.sql`:
```sql
-- name: CreateSiteTemplate :one
INSERT INTO site_templates (name, html, assets, is_preset) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetSiteTemplate :one
SELECT * FROM site_templates WHERE id = $1;

-- name: GetPresetByName :one
SELECT * FROM site_templates WHERE is_preset = true AND name = $1;

-- name: ListSiteTemplates :many
SELECT id, name, is_preset, created_at, updated_at FROM site_templates ORDER BY is_preset DESC, name;

-- name: UpdateSiteTemplate :one
UPDATE site_templates SET name = $2, html = $3, assets = $4, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteSiteTemplate :exec
DELETE FROM site_templates WHERE id = $1 AND is_preset = false;

-- name: UpsertNodeSite :exec
INSERT INTO node_sites (node_id, template_id, bundle, bundle_hash) VALUES ($1, $2, $3, $4)
ON CONFLICT (node_id) DO UPDATE SET template_id = EXCLUDED.template_id, bundle = EXCLUDED.bundle, bundle_hash = EXCLUDED.bundle_hash, updated_at = now();
```
Run `sqlc generate`.

- [ ] **Step 2: Write failing tests**

`internal/sitekit/normalize_test.go`:
```go
package sitekit

import (
	"strings"
	"testing"
)

func TestNormalizeExtractsInlineStyleAndScript(t *testing.T) {
	html := `<!doctype html><html><head><title>x</title><style>body{color:red}</style></head>
<body><h1 style="margin:0">Hi</h1><button id="b">Go</button><script>document.getElementById("b").addEventListener("click",()=>alert(1))</script></body></html>`
	b, rep, err := Normalize(html, nil)
	if err != nil || len(rep.Errors) != 0 {
		t.Fatalf("err %v report %+v", err, rep)
	}
	idx := string(b.Files["index.html"])
	if strings.Contains(idx, "<style") || strings.Contains(idx, "style=") || strings.Contains(idx, "alert(1)") {
		t.Fatalf("inline content left in index: %s", idx)
	}
	var css, js int
	for p := range b.Files {
		switch {
		case strings.HasSuffix(p, ".css"):
			css++
		case strings.HasSuffix(p, ".js"):
			js++
		}
	}
	if css != 1 || js != 1 {
		t.Fatalf("expected 1 css and 1 js, got %d/%d: %v", css, js, keys(b.Files))
	}
	if !strings.Contains(idx, `<link rel="stylesheet" href="/`) || !strings.Contains(idx, `<script src="/`) {
		t.Fatalf("no references inserted: %s", idx)
	}
	if len(rep.Warnings) == 0 {
		t.Fatal("expected warnings about extraction")
	}
}

func TestNormalizeRejectsForbidden(t *testing.T) {
	cases := map[string]string{
		"external script": `<html><body><script src="https://cdn.x/y.js"></script></body></html>`,
		"external css":    `<html><head><link rel="stylesheet" href="//fonts.googleapis.com/x.css"></head></html>`,
		"inline handler":  `<html><body><a onclick="x()">a</a></body></html>`,
		"javascript href": `<html><body><a href="javascript:void(0)">a</a></body></html>`,
		"iframe":          `<html><body><iframe src="/x"></iframe></body></html>`,
		"form":            `<html><body><form action="/x"></form></body></html>`,
		"external img":    `<html><body><img src="http://x.y/a.png"></body></html>`,
		"css import":      `<html><head><style>@import url("https://x/y.css");</style></head></html>`,
		"service worker":  `<html><body><script>navigator.serviceWorker.register("/sw.js")</script></body></html>`,
	}
	for name, html := range cases {
		_, rep, err := Normalize(html, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rep.Errors) == 0 {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeKeepsLocalAssets(t *testing.T) {
	html := `<html><head><link rel="stylesheet" href="/a.css"></head><body><img src="/i.png"><a href="mailto:a@b.c">m</a><a href="/about">about</a></body></html>`
	b, rep, _ := Normalize(html, map[string][]byte{"a.css": []byte("p{}"), "i.png": {1, 2}})
	if len(rep.Errors) != 0 || len(b.Files) != 3 {
		t.Fatalf("report %+v files %v", rep, keys(b.Files))
	}
}

func TestNormalizeMissingAssetIsError(t *testing.T) {
	_, rep, _ := Normalize(`<html><head><link rel="stylesheet" href="/missing.css"></head></html>`, nil)
	if len(rep.Errors) == 0 {
		t.Fatal("missing local asset must be an error")
	}
}

func TestBundleHashAndJSON(t *testing.T) {
	b := Bundle{Files: map[string][]byte{"index.html": []byte("a"), "s.css": []byte("b")}}
	h1 := b.Hash()
	b2, err := BundleFromJSON(b.JSON())
	if err != nil || b2.Hash() != h1 {
		t.Fatalf("json round trip: %v", err)
	}
	b.Files["s.css"] = []byte("c")
	if b.Hash() == h1 {
		t.Fatal("hash must change")
	}
}

func TestPresetsNormalizeClean(t *testing.T) {
	for _, p := range Presets() {
		_, rep, err := Normalize(p.HTML, p.Assets)
		if err != nil || len(rep.Errors) > 0 {
			t.Fatalf("preset %s: %v %+v", p.Name, err, rep.Errors)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 3: Run to verify fail**

Run: `go test ./internal/sitekit/` → compile errors.

- [ ] **Step 4: Implement normalize.go**

`internal/sitekit/normalize.go`:
```go
// Package sitekit validates operator sites against the relay/CSP rules and packages them.
package sitekit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Bundle struct{ Files map[string][]byte }

type Report struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

func randName(prefix, ext string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b) + ext
}

var (
	reImport      = regexp.MustCompile(`(?i)@import`)
	reURLExternal = regexp.MustCompile(`(?i)url\(\s*['"]?\s*(https?:)?//`)
	reSW          = regexp.MustCompile(`(?i)serviceWorker`)
)

// isLocal reports whether a URL points into the site itself.
func isLocal(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "tel:") {
		return true
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "data:") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == ""
}

func assetPath(raw string) string {
	u, _ := url.Parse(strings.TrimSpace(raw))
	return strings.TrimPrefix(u.Path, "/")
}

// Normalize parses html, rejects forbidden constructs, extracts inline CSS/JS into files and
// returns a deployable bundle. Report.Errors non-empty means the site must be rejected.
func Normalize(src string, assets map[string][]byte) (Bundle, Report, error) {
	var rep Report
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Bundle{}, rep, err
	}
	files := map[string][]byte{}
	for p, c := range assets {
		files[strings.TrimPrefix(p, "/")] = c
	}
	var css bytes.Buffer
	var js bytes.Buffer
	var head, body *html.Node
	var remove []*html.Node
	referenced := map[string]bool{}

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Head:
				head = n
			case atom.Body:
				body = n
			case atom.Iframe, atom.Frame, atom.Object, atom.Embed:
				rep.Errors = append(rep.Errors, fmt.Sprintf("<%s> is not allowed (CSP blocks framing and plugins)", n.Data))
			case atom.Form:
				rep.Errors = append(rep.Errors, "<form> is not allowed (form-action 'none')")
			case atom.Base:
				rep.Errors = append(rep.Errors, "<base> is not allowed (base-uri 'none')")
			case atom.Style:
				text := textOf(n)
				if reImport.MatchString(text) || reURLExternal.MatchString(text) {
					rep.Errors = append(rep.Errors, "<style> uses @import or an external url()")
				}
				css.WriteString(text + "\n")
				remove = append(remove, n)
				rep.Warnings = append(rep.Warnings, "inline <style> moved to a stylesheet file")
			case atom.Script:
				if srcAttr := attr(n, "src"); srcAttr != "" {
					if !isLocal(srcAttr) {
						rep.Errors = append(rep.Errors, "external script: "+srcAttr)
					} else {
						referenced[assetPath(srcAttr)] = true
					}
				} else {
					text := textOf(n)
					if reSW.MatchString(text) {
						rep.Errors = append(rep.Errors, "service workers are not allowed")
					}
					js.WriteString(text + "\n")
					remove = append(remove, n)
					rep.Warnings = append(rep.Warnings, "inline <script> moved to a script file")
				}
			case atom.Link:
				rel := strings.ToLower(attr(n, "rel"))
				href := attr(n, "href")
				if strings.Contains(rel, "manifest") {
					rep.Errors = append(rep.Errors, "web app manifest is not allowed")
				}
				if href != "" && !isLocal(href) {
					rep.Errors = append(rep.Errors, "external resource: "+href)
				} else if href != "" && (strings.Contains(rel, "stylesheet") || strings.Contains(rel, "icon")) {
					referenced[assetPath(href)] = true
				}
			case atom.Img, atom.Source, atom.Video, atom.Audio, atom.Track:
				if srcAttr := attr(n, "src"); srcAttr != "" {
					if !isLocal(srcAttr) {
						rep.Errors = append(rep.Errors, "external media: "+srcAttr)
					} else {
						referenced[assetPath(srcAttr)] = true
					}
				}
				if srcset := attr(n, "srcset"); srcset != "" {
					for _, part := range strings.Split(srcset, ",") {
						u := strings.Fields(strings.TrimSpace(part))
						if len(u) > 0 && !isLocal(u[0]) {
							rep.Errors = append(rep.Errors, "external media in srcset: "+u[0])
						}
					}
				}
			case atom.A:
				if href := attr(n, "href"); strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "javascript:") {
					rep.Errors = append(rep.Errors, "javascript: links are not allowed")
				}
			}
			var kept []html.Attribute
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				switch {
				case strings.HasPrefix(key, "on"):
					rep.Errors = append(rep.Errors, fmt.Sprintf("inline handler %s on <%s>", a.Key, n.Data))
				case key == "style":
					cls := "i-" + randName("", "")[1:]
					css.WriteString("." + cls + "{" + a.Val + "}\n")
					kept = append(kept, html.Attribute{Key: "class", Val: strings.TrimSpace(attr(n, "class") + " " + cls)})
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("style attribute on <%s> moved to class .%s", n.Data, cls))
				case key == "class" && hasStyleAttr(n):
					// merged above
				default:
					kept = append(kept, a)
				}
			}
			n.Attr = kept
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	for _, n := range remove {
		n.Parent.RemoveChild(n)
	}
	if head == nil || body == nil {
		rep.Errors = append(rep.Errors, "document must have <head> and <body>")
		return Bundle{}, rep, nil
	}
	if css.Len() > 0 {
		name := randName("s", ".css")
		files[name] = css.Bytes()
		head.AppendChild(&html.Node{Type: html.ElementNode, Data: "link", DataAtom: atom.Link,
			Attr: []html.Attribute{{Key: "rel", Val: "stylesheet"}, {Key: "href", Val: "/" + name}}})
	}
	if js.Len() > 0 {
		name := randName("j", ".js")
		files[name] = js.Bytes()
		body.AppendChild(&html.Node{Type: html.ElementNode, Data: "script", DataAtom: atom.Script,
			Attr: []html.Attribute{{Key: "src", Val: "/" + name}, {Key: "defer", Val: ""}}})
	}
	for p := range referenced {
		if _, ok := files[p]; !ok && p != "" {
			rep.Errors = append(rep.Errors, "referenced local file is missing from assets: /"+p)
		}
	}
	for p, c := range files {
		if strings.HasSuffix(p, ".css") && (reImport.Match(c) || reURLExternal.Match(c)) {
			rep.Errors = append(rep.Errors, "stylesheet "+p+" uses @import or an external url()")
		}
	}
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return Bundle{}, rep, err
	}
	files["index.html"] = out.Bytes()
	if len(rep.Errors) > 0 {
		return Bundle{}, rep, nil
	}
	return Bundle{Files: files}, rep, nil
}

func hasStyleAttr(n *html.Node) bool { return attr(n, "style") != "" }

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func (b Bundle) Hash() string {
	paths := make([]string, 0, len(b.Files))
	for p := range b.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(b.Files[p])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (b Bundle) JSON() []byte {
	m := map[string]string{}
	for p, c := range b.Files {
		m[p] = base64.StdEncoding.EncodeToString(c)
	}
	out, _ := json.Marshal(m)
	return out
}

func BundleFromJSON(raw []byte) (Bundle, error) {
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Bundle{}, err
	}
	b := Bundle{Files: map[string][]byte{}}
	for p, s := range m {
		c, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return Bundle{}, err
		}
		b.Files[p] = c
	}
	return b, nil
}

func (b Bundle) Paths() []string {
	out := make([]string, 0, len(b.Files))
	for p := range b.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
```
Note on the `class` merge: when a node has both `style` and `class`, the loop above appends a merged `class` attribute while processing `style` and skips the original `class`. Attribute order in `n.Attr` is arbitrary; implement it as: first compute `cls` if a style attr exists, then rebuild the attribute list once (drop `style`, drop `on*`, replace/add `class`). Write it that way rather than relying on loop order.

- [ ] **Step 5: First preset**

`internal/sitekit/presets/studio/index.html` — a complete single-file page for a fictional design studio ("Ferrule Studio"), with a `<style>` block (the normaliser moves it out), sections `<header>`, `<section data-block="hero">`, `<section data-block="work">` (3 cards), `<section data-block="process">`, `<section data-block="contact">` (address, email as `mailto:`), `<footer>`. No forms, no images, no scripts. Roughly 120 lines; write it with a distinct look (serif headings, warm neutral palette, generous spacing) so it does not resemble the fallback site from Part B.

`internal/sitekit/presets.go`:
```go
package sitekit

import "embed"

//go:embed presets/*/index.html
var presetFS embed.FS

type Preset struct {
	Name   string
	HTML   string
	Assets map[string][]byte
}

func Presets() []Preset {
	var out []Preset
	for _, name := range []string{"studio"} {
		b, _ := presetFS.ReadFile("presets/" + name + "/index.html")
		out = append(out, Preset{Name: name, HTML: string(b), Assets: map[string][]byte{}})
	}
	return out
}
```

Run: `go test ./internal/sitekit/ -v` → PASS.

- [ ] **Step 6: Write failing API tests**

`internal/api/sites_test.go`:
```go
package api_test

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const goodHTML = `<!doctype html><html><head><title>t</title><style>p{color:#333}</style></head><body><p>hello</p></body></html>`

func TestSiteTemplateCRUDAndValidate(t *testing.T) {
	_, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct {
			ID       uuid.UUID `json:"id"`
			Name     string    `json:"name"`
			IsPreset bool      `json:"is_preset"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/site-templates"), &list)
	if len(list.Items) != 1 || !list.Items[0].IsPreset || list.Items[0].Name != "studio" {
		t.Fatalf("presets not seeded: %+v", list.Items)
	}
	var val struct {
		Report struct {
			Errors   []string `json:"errors"`
			Warnings []string `json:"warnings"`
		} `json:"report"`
	}
	c.JSON(c.Post("/api/v1/site-templates/validate", map[string]any{"html": `<html><body><a onclick="x()">a</a></body></html>`}), &val)
	if len(val.Report.Errors) == 0 {
		t.Fatal("validate must report errors")
	}
	resp := c.Post("/api/v1/site-templates", map[string]any{"name": "bad", "html": `<html><body><script src="https://x/y.js"></script></body></html>`})
	if resp.StatusCode != 422 {
		t.Fatalf("bad template expected 422, got %d", resp.StatusCode)
	}
	var tpl struct {
		ID   uuid.UUID `json:"id"`
		HTML string    `json:"html"`
	}
	resp = c.Post("/api/v1/site-templates", map[string]any{"name": "mine", "html": goodHTML})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &tpl)
	if !strings.Contains(tpl.HTML, "<style>") {
		t.Fatal("stored html should keep the author's source")
	}
	if resp := c.Delete("/api/v1/site-templates/" + list.Items[0].ID.String()); resp.StatusCode != 409 {
		t.Fatalf("preset delete expected 409, got %d", resp.StatusCode)
	}
	if resp := c.Delete("/api/v1/site-templates/" + tpl.ID.String()); resp.StatusCode != 204 {
		t.Fatalf("delete %d", resp.StatusCode)
	}
}

func TestAssignSiteToNode(t *testing.T) {
	h, c, n := ownerWithNode(t)
	var tpl struct{ ID uuid.UUID `json:"id"` }
	c.JSON(c.Post("/api/v1/site-templates", map[string]any{"name": "mine", "html": goodHTML}), &tpl)
	resp := c.Post("/api/v1/nodes/"+n.ID.String()+"/site", map[string]any{"template_id": tpl.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("assign %d", resp.StatusCode)
	}
	var site struct {
		BundleHash   string   `json:"bundle_hash"`
		DeployedHash *string  `json:"deployed_hash"`
		Files        []string `json:"files"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/site"), &site)
	if site.BundleHash == "" || site.DeployedHash != nil || len(site.Files) != 2 {
		t.Fatalf("site %+v", site)
	}
	var got nodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if !got.Dirty {
		t.Fatal("assign must mark node dirty")
	}
	prev := c.Get("/api/v1/nodes/" + n.ID.String() + "/site/preview")
	body, _ := io.ReadAll(prev.Body)
	if prev.StatusCode != 200 || !strings.Contains(string(body), "color:#333") {
		t.Fatalf("preview %d: %s", prev.StatusCode, body)
	}
	// install script now embeds the assigned site
	_, cmd := createNode(t, c, "n2.test")
	_ = cmd
	files, err := h.Deps.SiteProvider(t.Context(), n.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("site provider %v %d", err, len(files))
	}
}
```

- [ ] **Step 7: Implement sites API**

`internal/api/sites.go`:
```go
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tgwebproxy/internal/sitekit"
	"tgwebproxy/internal/store/db"
)

func (s *Server) mountSites(r chi.Router) {
	r.Get("/site-templates", s.handleListTemplates)
	r.With(RequireRole(writers...)).Post("/site-templates", s.handleCreateTemplate)
	r.With(RequireRole(writers...)).Post("/site-templates/validate", s.handleValidateTemplate)
	r.Get("/site-templates/{id}", s.handleGetTemplate)
	r.With(RequireRole(writers...)).Put("/site-templates/{id}", s.handleUpdateTemplate)
	r.With(RequireRole(writers...)).Delete("/site-templates/{id}", s.handleDeleteTemplate)
}

type templateInput struct {
	Name   string            `json:"name"`
	HTML   string            `json:"html"`
	Assets map[string]string `json:"assets"`
}

func decodeAssets(in map[string]string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for p, b64 := range in {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(p, "/")] = raw
	}
	return out, nil
}

func encodeAssets(in map[string][]byte) map[string]string {
	out := map[string]string{}
	for p, c := range in {
		out[p] = base64.StdEncoding.EncodeToString(c)
	}
	return out
}

func templateJSON(t db.SiteTemplate, full bool) map[string]any {
	out := map[string]any{"id": t.ID, "name": t.Name, "is_preset": t.IsPreset, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt}
	if full {
		var assets map[string]string
		_ = json.Unmarshal(t.Assets, &assets)
		out["html"] = t.Html
		out["assets"] = assets
	}
	return out
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Q.ListSiteTemplates(r.Context())
	if err != nil {
		internal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows, "total": len(rows)})
}

func (s *Server) validateInput(w http.ResponseWriter, in templateInput) (map[string][]byte, sitekit.Bundle, bool) {
	assets, err := decodeAssets(in.Assets)
	if err != nil {
		validation(w, map[string]string{"assets": "values must be base64"})
		return nil, sitekit.Bundle{}, false
	}
	bundle, rep, err := sitekit.Normalize(in.HTML, assets)
	if err != nil {
		validation(w, map[string]string{"html": "cannot parse HTML"})
		return nil, sitekit.Bundle{}, false
	}
	if len(rep.Errors) > 0 {
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "site_invalid", "message": "site violates relay restrictions", "fields": map[string]string{"html": strings.Join(rep.Errors, "; ")}}, "report": rep})
		return nil, sitekit.Bundle{}, false
	}
	return assets, bundle, true
}

func (s *Server) handleValidateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	assets, err := decodeAssets(in.Assets)
	if err != nil {
		validation(w, map[string]string{"assets": "values must be base64"})
		return
	}
	bundle, rep, err := sitekit.Normalize(in.HTML, assets)
	if err != nil {
		validation(w, map[string]string{"html": "cannot parse HTML"})
		return
	}
	writeJSON(w, 200, map[string]any{"report": rep, "files": bundle.Paths()})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		validation(w, map[string]string{"name": "required"})
		return
	}
	assets, _, ok := s.validateInput(w, in)
	if !ok {
		return
	}
	raw, _ := json.Marshal(encodeAssets(assets))
	t, err := s.store.Q.CreateSiteTemplate(r.Context(), db.CreateSiteTemplateParams{Name: in.Name, Html: in.HTML, Assets: raw, IsPreset: false})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.create", "site_template", t.ID.String(), map[string]any{"name": t.Name})
	writeJSON(w, 201, templateJSON(t, true))
}

func (s *Server) loadTemplate(w http.ResponseWriter, r *http.Request) (db.SiteTemplate, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		notFound(w)
		return db.SiteTemplate{}, false
	}
	t, err := s.store.Q.GetSiteTemplate(r.Context(), id)
	if err != nil {
		notFound(w)
		return db.SiteTemplate{}, false
	}
	return t, true
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, templateJSON(t, true))
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	var in templateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	assets, _, ok := s.validateInput(w, in)
	if !ok {
		return
	}
	raw, _ := json.Marshal(encodeAssets(assets))
	name := in.Name
	if t.IsPreset {
		name = t.Name
	}
	updated, err := s.store.Q.UpdateSiteTemplate(r.Context(), db.UpdateSiteTemplateParams{ID: t.ID, Name: name, Html: in.HTML, Assets: raw})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.update", "site_template", t.ID.String(), nil)
	writeJSON(w, 200, templateJSON(updated, true))
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTemplate(w, r)
	if !ok {
		return
	}
	if t.IsPreset {
		conflict(w, "presets cannot be deleted")
		return
	}
	if err := s.store.Q.DeleteSiteTemplate(r.Context(), t.ID); err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "site_template.delete", "site_template", t.ID.String(), nil)
	w.WriteHeader(204)
}

// --- node site assignment ---

func (s *Server) renderTemplateBundle(t db.SiteTemplate) (sitekit.Bundle, error) {
	var assets map[string]string
	_ = json.Unmarshal(t.Assets, &assets)
	raw, err := decodeAssets(assets)
	if err != nil {
		return sitekit.Bundle{}, err
	}
	bundle, rep, err := sitekit.Normalize(t.Html, raw)
	if err != nil {
		return sitekit.Bundle{}, err
	}
	if len(rep.Errors) > 0 {
		return sitekit.Bundle{}, errorsJoin(rep.Errors)
	}
	return bundle, nil
}

func (s *Server) handleAssignSite(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	var in struct {
		TemplateID uuid.UUID `json:"template_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	t, err := s.store.Q.GetSiteTemplate(r.Context(), in.TemplateID)
	if err != nil {
		validation(w, map[string]string{"template_id": "unknown template"})
		return
	}
	bundle, err := s.renderTemplateBundle(t)
	if err != nil {
		writeError(w, 422, "site_invalid", err.Error(), nil)
		return
	}
	err = s.store.Tx(r.Context(), func(q *db.Queries) error {
		if err := q.UpsertNodeSite(r.Context(), db.UpsertNodeSiteParams{NodeID: n.ID, TemplateID: uuid.NullUUID{UUID: t.ID, Valid: true}, Bundle: bundle.JSON(), BundleHash: bundle.Hash()}); err != nil {
			return err
		}
		return q.SetNodeDirty(r.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: true})
	})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.site_assign", "node", n.ID.String(), map[string]any{"template_id": t.ID})
	s.handleGetNodeSite(w, r)
}

func (s *Server) handleGetNodeSite(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	site, err := s.store.Q.GetNodeSite(r.Context(), n.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"template_id": nil, "bundle_hash": "", "deployed_hash": nil, "files": []string{}})
		return
	}
	bundle, _ := sitekit.BundleFromJSON(site.Bundle)
	writeJSON(w, 200, map[string]any{"template_id": site.TemplateID, "bundle_hash": site.BundleHash, "deployed_hash": site.DeployedHash, "files": bundle.Paths(), "updated_at": site.UpdatedAt})
}

// handleSitePreview inlines the bundle's stylesheets so the admin can preview without deploying.
func (s *Server) handleSitePreview(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	site, err := s.store.Q.GetNodeSite(r.Context(), n.ID)
	if err != nil {
		notFound(w)
		return
	}
	bundle, err := sitekit.BundleFromJSON(site.Bundle)
	if err != nil {
		internal(w)
		return
	}
	page := string(bundle.Files["index.html"])
	for p, c := range bundle.Files {
		if strings.HasSuffix(p, ".css") {
			page = strings.Replace(page, `<link rel="stylesheet" href="/`+p+`"/>`, "<style>"+string(c)+"</style>", 1)
			page = strings.Replace(page, `<link rel="stylesheet" href="/`+p+`">`, "<style>"+string(c)+"</style>", 1)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	_, _ = w.Write([]byte(page))
}

// NodeSiteFiles is the SiteProvider used by the install script.
func (s *Server) NodeSiteFiles(ctx context.Context, nodeID uuid.UUID) (map[string][]byte, error) {
	site, err := s.store.Q.GetNodeSite(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	b, err := sitekit.BundleFromJSON(site.Bundle)
	if err != nil {
		return nil, err
	}
	return b.Files, nil
}
```
`errorsJoin(errs []string) error` → `errors.New(strings.Join(errs, "; "))`. Mount in `mountProtected`: `s.mountSites(r)` and inside the `/nodes/{id}` route: `r.With(RequireRole(writers...)).Post("/site", s.handleAssignSite)`, `r.Get("/site", s.handleGetNodeSite)`, `r.Get("/site/preview", s.handleSitePreview)`. In `api.New`, when `d.SiteProvider == nil` set `s.siteProvider = s.NodeSiteFiles`; expose it on `Deps` for tests by having `apitest.New` set `h.Deps.SiteProvider = srv.NodeSiteFiles` after constructing `srv` (make `api.New` return the server first, then build the httptest server).

- [ ] **Step 8: Seed presets on startup and in tests**

Add `store.SeedPresets(ctx, st)`:
```go
// in internal/store/seed.go
func SeedPresets(ctx context.Context, s *Store, presets []sitekit.Preset) error {
	for _, p := range presets {
		if _, err := s.Q.GetPresetByName(ctx, p.Name); err == nil {
			continue
		}
		raw, _ := json.Marshal(map[string]string{})
		if _, err := s.Q.CreateSiteTemplate(ctx, db.CreateSiteTemplateParams{Name: p.Name, Html: p.HTML, Assets: raw, IsPreset: true}); err != nil {
			return err
		}
	}
	return nil
}
```
Call it from `cmd/panel/main.go` after migrate (`store.SeedPresets(ctx, st, sitekit.Presets())`) and from `apitest.New`. `store` importing `sitekit` is fine (sitekit has no store dependency).

- [ ] **Step 9: Run tests**

Run: `go test ./internal/sitekit/ ./internal/api/ -race && go test ./... && golangci-lint run ./...`
Expected: PASS.

---

### Task 13: Branding

**Files:**
- Create: `internal/store/queries/branding.sql`, `internal/branding/css.go`, `internal/branding/css_test.go`, `internal/api/branding.go`, `internal/api/branding_test.go`

**Interfaces:**
- `branding.SanitizeCSS(css string) (string, []string)` returns cleaned CSS and a list of removed constructs (`@import`, external `url(`, `expression(`, `behavior:`, `</style`).
- `branding.SanitizeSVG(svg []byte) ([]byte, error)` rejects `<script>`, `on*` attributes, `href`/`xlink:href` not starting with `#`, `<foreignObject>`.
- `branding.ValidateColor(s string) bool` (`#rgb`, `#rrggbb`).
- API: `GET /branding` (public) → active profile JSON `{panel_name, logo_url, favicon_url, primary_color, accent_color, theme_default, login_bg_url, login_text, support_link, footer_text, custom_css}`; protected: `GET /branding/profiles` → `{items}`; `POST /branding/profiles` `{name}` (copies active); `PUT /branding/profiles/{id}` (all text fields; colors validated; css sanitised); `POST /branding/profiles/{id}/activate`; `DELETE /branding/profiles/{id}` (not the active one); `POST /branding/profiles/{id}/upload?kind=logo|favicon|login_bg` multipart `file` (png/svg/ico/jpg ≤512KB, stored at `DataDir/branding/<id>/<kind>.<ext>`); public `GET /branding/assets/{id}/{file}`.

- [ ] **Step 1: Queries**

`internal/store/queries/branding.sql`:
```sql
-- name: GetActiveBranding :one
SELECT * FROM branding_profiles WHERE is_active = true LIMIT 1;

-- name: GetBranding :one
SELECT * FROM branding_profiles WHERE id = $1;

-- name: ListBranding :many
SELECT * FROM branding_profiles ORDER BY is_active DESC, name;

-- name: CreateBranding :one
INSERT INTO branding_profiles (name, panel_name, logo_path, favicon_path, primary_color, accent_color, theme_default, login_bg_path, login_text, support_link, footer_text, custom_css)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING *;

-- name: UpdateBranding :one
UPDATE branding_profiles SET name = $2, panel_name = $3, primary_color = $4, accent_color = $5, theme_default = $6, login_text = $7, support_link = $8, footer_text = $9, custom_css = $10, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: SetBrandingAsset :exec
UPDATE branding_profiles SET
  logo_path = CASE WHEN $2 = 'logo' THEN $3 ELSE logo_path END,
  favicon_path = CASE WHEN $2 = 'favicon' THEN $3 ELSE favicon_path END,
  login_bg_path = CASE WHEN $2 = 'login_bg' THEN $3 ELSE login_bg_path END,
  updated_at = now() WHERE id = $1;

-- name: ActivateBranding :exec
UPDATE branding_profiles SET is_active = (id = $1);

-- name: DeleteBranding :exec
DELETE FROM branding_profiles WHERE id = $1 AND is_active = false;
```
`ActivateBranding` must run in a transaction with the partial unique index: first `UPDATE ... SET is_active=false WHERE is_active`, then set true. Write it as two statements (`DeactivateBranding :exec` + `ActivateBrandingOne :exec`) and call both inside `store.Tx`.

- [ ] **Step 2: Failing tests**

`internal/branding/css_test.go`:
```go
package branding

import (
	"strings"
	"testing"
)

func TestSanitizeCSS(t *testing.T) {
	in := `@import url("https://evil/x.css"); .a{background:url(https://evil/i.png)} .b{color:red;behavior:url(x.htc)} .c{width:expression(1)} .d{background:url(/local.png)}`
	out, removed := SanitizeCSS(in)
	for _, bad := range []string{"@import", "https://evil", "behavior", "expression("} {
		if strings.Contains(out, bad) {
			t.Errorf("still contains %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, "url(/local.png)") || !strings.Contains(out, "color:red") {
		t.Errorf("safe css lost: %s", out)
	}
	if len(removed) < 4 {
		t.Errorf("removed list too short: %v", removed)
	}
}

func TestSanitizeSVG(t *testing.T) {
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>1</script></svg>`)); err == nil {
		t.Error("script accepted")
	}
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><a href="https://x"><rect/></a></svg>`)); err == nil {
		t.Error("external href accepted")
	}
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect onload="x()"/></svg>`)); err == nil {
		t.Error("handler accepted")
	}
	if _, err := SanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4" fill="#0af"/></svg>`)); err != nil {
		t.Errorf("clean svg rejected: %v", err)
	}
}

func TestValidateColor(t *testing.T) {
	for _, ok := range []string{"#fff", "#3b82f6", "#ABCDEF"} {
		if !ValidateColor(ok) {
			t.Errorf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"red", "#ggg", "#12345", "url(x)"} {
		if ValidateColor(bad) {
			t.Errorf("%s accepted", bad)
		}
	}
}
```

`internal/api/branding_test.go`:
```go
package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestPublicBrandingAndUpdate(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var pub struct {
		PanelName    string `json:"panel_name"`
		PrimaryColor string `json:"primary_color"`
		ThemeDefault string `json:"theme_default"`
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &pub)
	if pub.PanelName != "WEB Proxy Panel" || pub.ThemeDefault != "dark" {
		t.Fatalf("defaults %+v", pub)
	}
	var list struct {
		Items []struct {
			ID       uuid.UUID `json:"id"`
			IsActive bool      `json:"is_active"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	id := list.Items[0].ID
	resp := c.Put("/api/v1/branding/profiles/"+id.String(), map[string]any{"name": "default", "panel_name": "Acme Proxy", "primary_color": "#112233", "accent_color": "#445566", "theme_default": "light", "custom_css": "@import url(https://x); .a{color:red}"})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update %d %s", resp.StatusCode, b)
	}
	var updated struct {
		CustomCSS string `json:"custom_css"`
	}
	c.JSON(resp, &updated)
	if bytes.Contains([]byte(updated.CustomCSS), []byte("@import")) {
		t.Fatal("css not sanitised")
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &pub)
	if pub.PanelName != "Acme Proxy" || pub.PrimaryColor != "#112233" || pub.ThemeDefault != "light" {
		t.Fatalf("public not updated %+v", pub)
	}
	if resp := c.Put("/api/v1/branding/profiles/"+id.String(), map[string]any{"name": "default", "panel_name": "x", "primary_color": "red", "accent_color": "#445566", "theme_default": "dark"}); resp.StatusCode != 422 {
		t.Fatalf("bad color expected 422, got %d", resp.StatusCode)
	}
}

func TestBrandingUpload(t *testing.T) {
	h, c, _ := ownerWithNode(t)
	var list struct {
		Items []struct{ ID uuid.UUID `json:"id"` } `json:"items"`
	}
	c.JSON(c.Get("/api/v1/branding/profiles"), &list)
	id := list.Items[0].ID
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "logo.svg")
	_, _ = fw.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`))
	_ = mw.Close()
	resp := c.PostRaw("/api/v1/branding/profiles/"+id.String()+"/upload?kind=logo", mw.FormDataContentType(), body.Bytes())
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload %d %s", resp.StatusCode, b)
	}
	var pub struct {
		LogoURL string `json:"logo_url"`
	}
	h.Anonymous().JSON(h.Anonymous().Get("/api/v1/branding"), &pub)
	if pub.LogoURL == "" {
		t.Fatal("logo url missing")
	}
	if r := h.Anonymous().Get(pub.LogoURL); r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("asset fetch %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
}
```
Add `PostRaw(path, contentType string, body []byte) *http.Response` to `apitest.Client` (same as `do` but with explicit content type and raw body).

- [ ] **Step 3: Implement**

`internal/branding/css.go`:
```go
// Package branding validates and sanitises operator-provided branding.
package branding

import (
	"bytes"
	"errors"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	reImport     = regexp.MustCompile(`(?i)@import[^;]*;?`)
	reExtURL     = regexp.MustCompile(`(?i)url\(\s*['"]?\s*(?:https?:)?//[^)]*\)`)
	reExpression = regexp.MustCompile(`(?i)expression\([^)]*\)`)
	reBehavior   = regexp.MustCompile(`(?i)behavior\s*:[^;}]*;?`)
	reColor      = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

func SanitizeCSS(css string) (string, []string) {
	var removed []string
	for name, re := range map[string]*regexp.Regexp{"@import": reImport, "external url()": reExtURL, "expression()": reExpression, "behavior": reBehavior} {
		for _, m := range re.FindAllString(css, -1) {
			removed = append(removed, name+": "+strings.TrimSpace(m))
		}
		css = re.ReplaceAllString(css, "")
	}
	css = strings.ReplaceAll(css, "</style", "")
	return css, removed
}

func ValidateColor(s string) bool { return reColor.MatchString(s) }

func SanitizeSVG(svg []byte) ([]byte, error) {
	if !bytes.Contains(bytes.ToLower(svg), []byte("<svg")) {
		return nil, errors.New("not an svg")
	}
	tok := html.NewTokenizer(bytes.NewReader(svg))
	for {
		tt := tok.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := tok.Token()
		name := strings.ToLower(t.Data)
		if name == "script" || name == "foreignobject" || name == "iframe" {
			return nil, errors.New("svg contains <" + name + ">")
		}
		for _, a := range t.Attr {
			k := strings.ToLower(a.Key)
			if strings.HasPrefix(k, "on") {
				return nil, errors.New("svg contains event handler " + a.Key)
			}
			if (k == "href" || k == "xlink:href") && !strings.HasPrefix(strings.TrimSpace(a.Val), "#") {
				return nil, errors.New("svg references an external href")
			}
		}
	}
	return svg, nil
}
```

`internal/api/branding.go`: implement the routes listed in Interfaces. Key points:
- `brandingJSON(b db.BrandingProfile)` maps paths to URLs: `logo_url = "/api/v1/branding/assets/<id>/<basename>"` when path non-empty, else `""`.
- `PUT` validates `primary_color`/`accent_color` with `branding.ValidateColor`, `theme_default ∈ {dark, light}`, `support_link` empty or `https?://`, runs `branding.SanitizeCSS`, returns `{...profile, css_removed: [...]}`.
- `upload`: `r.ParseMultipartForm(1<<20)`, `kind` ∈ logo/favicon/login_bg, size ≤ 512KB, sniff type with `http.DetectContentType` (svg detected as `text/xml`/`text/plain` → treat by extension `.svg` and run `SanitizeSVG`), allowed: `image/png`, `image/jpeg`, `image/x-icon`, `image/vnd.microsoft.icon`, svg. Save to `DataDir/branding/<id>/<kind>.<ext>` with `os.MkdirAll`, store relative path via `SetBrandingAsset`.
- `GET /branding/assets/{id}/{file}` (public): serve from `DataDir/branding/<id>/<file>` with `filepath.Base` on `file`, content type by extension (`.svg` → `image/svg+xml`), `Cache-Control: public, max-age=3600`.
- `POST /branding/profiles` copies the active profile's fields with the new `name`, `is_active=false`.
- `activate` runs deactivate+activate in `store.Tx`. `delete` refuses the active profile with 409.
- All mutations are owner/admin and audited (`branding.update`, `branding.activate`, `branding.upload`).

Mount `GET /branding` and `GET /branding/assets/{id}/{file}` in the public part of `/api/v1`; the rest in `mountProtected` via `s.mountBranding(r)`.

- [ ] **Step 4: Run tests**

Run: `sqlc generate && go test ./internal/branding/ ./internal/api/ -race && golangci-lint run ./...`
Expected: PASS.

---

### Task 14: Dashboard summary, alerts, settings, panel /metrics

**Files:**
- Create: `internal/api/dashboard.go`, `internal/api/dashboard_test.go`, `internal/api/settings.go`, `internal/api/metrics.go`, `internal/api/audit_api.go`

**Interfaces:**
- `GET /dashboard/summary` → `{nodes:{online,offline,degraded,pending,total}, keys:{active,pending,revoked,total}, sessions_live, streams_live, bytes_up, bytes_down, alerts:[{id,node_id,node_name,kind,message,created_at}], recent_jobs:[...]}` (sessions/streams/bytes summed over `LatestSnapshots`).
- `GET /monitoring/nodes/{id}/series?from=<rfc3339>&to=<rfc3339>` → `{points:[{t, sessions_live, streams_live, bytes_up, bytes_down}]}` (default last 24h, capped at 2000 points by sampling every Nth row).
- `GET /alerts` → `{items}`; `POST /alerts/{id}/resolve`.
- `GET /audit?page&per_page` → `{items:[{id, username, action, target_type, target_id, meta, ip, created_at}], total}`.
- `GET /settings` → `{apply_interval, offline_after, telegram_alerts:{enabled, bot_token_set, chat_id}, backup_schedule}`; `PUT /settings` (owner) stores into `settings` table; `apply_interval` and `offline_after` are read by workers via `settings.Reader` on each tick (fallback to config). Telegram alert sending itself is Phase 2; store only.
- `GET /metrics` (public, requires `Authorization: Bearer <METRICS_TOKEN>` when configured): panel process metrics via `promhttp` plus gauges `tgwp_nodes{status}`, `tgwp_keys{status}`, `tgwp_node_sessions_live{node}`, `tgwp_node_streams_live{node}` refreshed from DB on each scrape (collector implementing `prometheus.Collector`).

- [ ] **Step 1: Failing test**

`internal/api/dashboard_test.go`:
```go
package api_test

import (
	"io"
	"strings"
	"testing"

	"tgwebproxy/internal/store/db"
)

func TestDashboardSummaryAndMetrics(t *testing.T) {
	h, c, n := ownerWithNode(t)
	c.Post("/api/v1/keys", map[string]any{"label": "k", "type": "SHARED", "carrier_mode": "https", "node_ids": []string{n.ID.String()}})
	_ = h.Store.Q.InsertSnapshot(t.Context(), db.InsertSnapshotParams{NodeID: n.ID, SessionsLive: 3, StreamsLive: 9, BytesUp: 10, BytesDown: 20, MtproxyRaw: []byte("{}")})
	var sum struct {
		Nodes        map[string]int `json:"nodes"`
		Keys         map[string]int `json:"keys"`
		SessionsLive int            `json:"sessions_live"`
		StreamsLive  int            `json:"streams_live"`
	}
	c.JSON(c.Get("/api/v1/dashboard/summary"), &sum)
	if sum.Nodes["total"] != 1 || sum.Nodes["pending"] != 1 || sum.Keys["pending"] != 1 || sum.SessionsLive != 3 || sum.StreamsLive != 9 {
		t.Fatalf("summary %+v", sum)
	}
	resp := h.Anonymous().Get("/metrics")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `tgwp_nodes{status="pending"} 1`) || !strings.Contains(string(body), "tgwp_node_sessions_live") {
		t.Fatalf("metrics %d: %.300s", resp.StatusCode, body)
	}
	var series struct {
		Points []map[string]any `json:"points"`
	}
	c.JSON(c.Get("/api/v1/monitoring/nodes/"+n.ID.String()+"/series"), &series)
	if len(series.Points) != 1 {
		t.Fatalf("series %+v", series)
	}
	var audit struct{ Total int `json:"total"` }
	c.JSON(c.Get("/api/v1/audit"), &audit)
	if audit.Total < 2 {
		t.Fatalf("audit total %d", audit.Total)
	}
	if resp := c.Put("/api/v1/settings", map[string]any{"apply_interval": 30, "offline_after": 120}); resp.StatusCode != 200 {
		t.Fatalf("settings %d", resp.StatusCode)
	}
	var settings struct{ ApplyInterval int `json:"apply_interval"` }
	c.JSON(c.Get("/api/v1/settings"), &settings)
	if settings.ApplyInterval != 30 {
		t.Fatalf("settings %+v", settings)
	}
}
```

- [ ] **Step 2: Implement**

Write the handlers per the Interfaces block. For the collector:
```go
type dbCollector struct{ st *store.Store }
var (
	descNodes    = prometheus.NewDesc("tgwp_nodes", "nodes by status", []string{"status"}, nil)
	descKeys     = prometheus.NewDesc("tgwp_keys", "keys by status", []string{"status"}, nil)
	descSessions = prometheus.NewDesc("tgwp_node_sessions_live", "live relay sessions", []string{"node"}, nil)
	descStreams  = prometheus.NewDesc("tgwp_node_streams_live", "live relay streams", []string{"node"}, nil)
)
func (c dbCollector) Describe(ch chan<- *prometheus.Desc) { ch <- descNodes; ch <- descKeys; ch <- descSessions; ch <- descStreams }
func (c dbCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second); defer cancel()
	for _, s := range []string{"pending","online","offline","degraded"} { counts default 0 } // fill from CountNodesByStatus
	... CountKeysByStatus ...
	for _, snap := range LatestSnapshots { ch <- prometheus.MustNewConstMetric(descSessions, prometheus.GaugeValue, float64(snap.SessionsLive), snap.NodeID.String()) ... }
}
```
Register a dedicated `prometheus.NewRegistry()` with `collectors.NewGoCollector()`, `collectors.NewProcessCollector(...)` and `dbCollector`; serve with `promhttp.HandlerFor(reg, promhttp.HandlerOpts{})`. Emit every status label even when 0 so the test's `pending` line is stable.

Settings: `settings.Reader` in `internal/api/settings.go` is a small struct `{st}` with `Get(ctx, key string, into any) bool`; workers accept an optional `func(ctx) time.Duration` override for interval/offline (add `SetIntervalFunc` methods to `Apply` and `Stats` that are consulted on every tick). Keep the implementation minimal: `Apply.Run` re-reads the interval after each tick and resets the ticker if changed.

- [ ] **Step 3: Run everything**

Run: `go mod tidy && go test ./... -race && golangci-lint run ./... && go build ./...`
Expected: all green. Then a manual smoke with `NODE_DRIVER=mock`: create node, key, assign preset, `POST /nodes/{id}/apply`, observe `GET /nodes/{id}/jobs` shows `ok` and key status flips to `active` (the mock is always "online" after `SetOnline`; for the manual run, mock nodes created through the API are offline — use `make agent-linux` + the local agent from Part B Step 10 with `NODE_DRIVER=gateway` instead, which exercises the real path; `-check` will fail on macOS because `tproxy-server` is absent, and the job must show `rolled_back` with the check error in its log).
