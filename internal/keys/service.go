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

func (v ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %v", map[string]string(v))
}

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
	TelemtLimits            domain.TelemtLimits
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
	if err := in.TelemtLimits.Validate(); err != nil {
		ve["telemt_limits"] = err.Error()
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

// createTx creates one key and its per-node profiles inside an existing transaction.
func (s *Service) createTx(ctx context.Context, q *db.Queries, in CreateInput) (db.AccessKey, error) {
	secret, err := crypto.NewSecretHex()
	if err != nil {
		return db.AccessKey{}, err
	}
	enc, err := s.box.EncryptString(secret)
	if err != nil {
		return db.AccessKey{}, err
	}
	limits, _ := json.Marshal(in.Limits)
	telemtLimits, _ := json.Marshal(in.TelemtLimits)
	key, err := q.CreateKey(ctx, db.CreateKeyParams{
		Label: in.Label, Type: db.KeyType(in.Type), OwnerLabel: in.OwnerLabel, SecretEnc: enc, CarrierMode: string(in.CarrierMode),
		Limits: limits, ExpiresAt: in.ExpiresAt, Note: in.Note, CreatedBy: uuid.NullUUID{UUID: in.CreatedBy, Valid: in.CreatedBy != uuid.Nil},
		TelemtLimits: telemtLimits,
	})
	if err != nil {
		return db.AccessKey{}, err
	}
	for _, nodeID := range in.NodeIDs {
		if err := s.bindTx(ctx, q, key, nodeID); err != nil {
			return db.AccessKey{}, err
		}
	}
	return key, nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (db.AccessKey, error) {
	if err := in.validate(true); err != nil {
		return db.AccessKey{}, err
	}
	var key db.AccessKey
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		var err error
		key, err = s.createTx(ctx, q, in)
		return err
	})
	if err != nil {
		return db.AccessKey{}, err
	}
	return key, nil
}

func (s *Service) CreateBatch(ctx context.Context, in CreateInput, prefix string, count int) ([]db.AccessKey, error) {
	if count < 1 || count > 100 {
		return nil, ValidationError{"count": "1..100"}
	}
	if strings.TrimSpace(prefix) == "" {
		return nil, ValidationError{"prefix": "required"}
	}
	var out []db.AccessKey
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		out = make([]db.AccessKey, 0, count)
		for _, nodeID := range in.NodeIDs {
			node, err := q.GetNode(ctx, nodeID)
			if err != nil {
				return ValidationError{"node_ids": "unknown node " + nodeID.String()}
			}
			used, err := q.CountNodeProfiles(ctx, nodeID)
			if err != nil {
				return err
			}
			if used+int64(count) > int64(node.MaxProfiles) {
				return fmt.Errorf("%w: %s has %d/%d profiles, only %d free but %d requested",
					ErrCapacity, node.Hostname, used, node.MaxProfiles, int64(node.MaxProfiles)-used, count)
			}
		}
		for i := 1; i <= count; i++ {
			item := in
			item.Label = fmt.Sprintf("%s-%d", prefix, i)
			if err := item.validate(true); err != nil {
				return err
			}
			k, err := s.createTx(ctx, q, item)
			if err != nil {
				return err
			}
			out = append(out, k)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
		return q.SetKeyStatus(ctx, db.SetKeyStatusParams{ID: keyID, Column2: db.KeyStatusRevoked})
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
		old, err := q.GetKey(ctx, keyID)
		if err != nil {
			return ErrNotFound
		}
		if old.Status == db.KeyStatusRevoked {
			return ValidationError{"status": "key is revoked"}
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

// UpdateInput is the full desired state of an editable key.
type UpdateInput struct {
	Label, OwnerLabel, Note string
	ExpiresAt               *time.Time
	CarrierMode             domain.CarrierMode
	Limits                  domain.ProfileLimits
	TelemtLimits            domain.TelemtLimits
}

func (s *Service) Update(ctx context.Context, keyID uuid.UUID, in UpdateInput) (db.AccessKey, error) {
	if strings.TrimSpace(in.Label) == "" {
		return db.AccessKey{}, ValidationError{"label": "required"}
	}
	if !in.CarrierMode.Valid() {
		return db.AccessKey{}, ValidationError{"carrier_mode": "invalid"}
	}
	if err := in.Limits.Validate(); err != nil {
		return db.AccessKey{}, ValidationError{"limits": err.Error()}
	}
	if err := in.TelemtLimits.Validate(); err != nil {
		return db.AccessKey{}, ValidationError{"telemt_limits": err.Error()}
	}
	carrier := in.CarrierMode
	raw, _ := json.Marshal(in.Limits)
	telemtRaw, _ := json.Marshal(in.TelemtLimits)
	var key db.AccessKey
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		old, err := q.GetKey(ctx, keyID)
		if err != nil {
			return ErrNotFound
		}
		key, err = q.UpdateKey(ctx, db.UpdateKeyParams{
			ID: keyID, Label: in.Label, OwnerLabel: in.OwnerLabel, Note: in.Note, ExpiresAt: in.ExpiresAt,
			CarrierMode: string(carrier), Limits: raw, TelemtLimits: telemtRaw,
		})
		if err != nil {
			return err
		}
		if old.CarrierMode != string(carrier) || string(old.Limits) != string(raw) || string(old.TelemtLimits) != string(telemtRaw) {
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

// Extend moves a key's expiry forward.
func (s *Service) Extend(ctx context.Context, keyID uuid.UUID, expiresAt time.Time) error {
	if !expiresAt.After(time.Now()) {
		return ValidationError{"expires_at": "must be in the future"}
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		k, err := q.GetKey(ctx, keyID)
		if err != nil {
			return ErrNotFound
		}
		if k.Status == db.KeyStatusRevoked {
			return ValidationError{"status": "key is revoked"}
		}
		exp := expiresAt
		return q.SetKeyExpiry(ctx, db.SetKeyExpiryParams{ID: keyID, ExpiresAt: &exp})
	})
}

func (s *Service) Secret(ctx context.Context, key db.AccessKey) (string, error) {
	return s.box.DecryptString(key.SecretEnc)
}

// Link kinds.
const (
	LinkWeb = "web"
	LinkTLS = "tls"
)

// KindLink is one link form of one kind, for one node.
type KindLink struct {
	Kind string `json:"kind"`
	TMe  string `json:"tme"`
	Tg   string `json:"tg"`
}

// NodeLinks groups every link a key has on one node.
type NodeLinks struct {
	NodeID   uuid.UUID  `json:"node_id"`
	NodeName string     `json:"node_name"`
	Hostname string     `json:"hostname"`
	Engine   string     `json:"engine"`
	Links    []KindLink `json:"links"`
}

// Link is the flattened form of NodeLinks: one entry per node and kind.
type Link struct {
	NodeID   uuid.UUID `json:"node_id"`
	NodeName string    `json:"node_name"`
	Hostname string    `json:"hostname"`
	Kind     string    `json:"kind"`
	TMe      string    `json:"tme"`
	Tg       string    `json:"tg"`
}

// NodeLinks builds every link for the key, grouped by node.
func (s *Service) NodeLinks(ctx context.Context, keyID uuid.UUID) ([]NodeLinks, error) {
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
	out := make([]NodeLinks, 0, len(bindings))
	for _, b := range bindings {
		item := NodeLinks{
			NodeID: b.NodeID, NodeName: b.NodeName, Hostname: b.Hostname, Engine: string(b.Engine),
			Links: []KindLink{{Kind: LinkWeb, TMe: qrlink.TMe(b.Hostname, secret), Tg: qrlink.Tg(b.Hostname, secret)}},
		}
		if b.Engine == db.NodeEngineTelemt && b.TlsDomain != "" {
			fake := qrlink.FakeTLSSecret(secret, b.TlsDomain)
			port := int(b.ClassicPort)
			item.Links = append(item.Links, KindLink{
				Kind: LinkTLS, TMe: qrlink.TMeProxy(b.Hostname, port, fake), Tg: qrlink.TgProxy(b.Hostname, port, fake),
			})
		}
		out = append(out, item)
	}
	return out, nil
}

// Links is NodeLinks flattened, in node order with the web link of each node first.
func (s *Service) Links(ctx context.Context, keyID uuid.UUID) ([]Link, error) {
	grouped, err := s.NodeLinks(ctx, keyID)
	if err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(grouped))
	for _, g := range grouped {
		for _, l := range g.Links {
			out = append(out, Link{NodeID: g.NodeID, NodeName: g.NodeName, Hostname: g.Hostname, Kind: l.Kind, TMe: l.TMe, Tg: l.Tg})
		}
	}
	return out, nil
}
