package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
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
		r.Get("/stats", s.handleKeyStats)
		r.With(RequireRole(writers...)).Patch("/", s.handlePatchKey)
		r.With(RequireRole(writers...)).Delete("/", s.handleDeleteKey)
		r.With(RequireRole(writers...)).Post("/revoke", s.handleRevokeKey)
		r.With(RequireRole(writers...)).Post("/rotate", s.handleRotateKey)
		r.With(RequireRole(writers...)).Get("/links", s.handleKeyLinks)
		r.With(RequireRole(writers...)).Get("/qr", s.handleKeyQR)
		r.With(RequireRole(writers...)).Post("/bindings", s.handleBindKey)
		r.With(RequireRole(writers...)).Delete("/bindings/{node}", s.handleUnbindKey)
		r.With(RequireRole(writers...)).Post("/subscription", s.handleCreateSubscription)
		r.With(RequireRole(writers...)).Delete("/subscription", s.handleRevokeSubscription)
	})
}

type keyJSON struct {
	ID                 uuid.UUID         `json:"id"`
	Label              string            `json:"label"`
	Type               string            `json:"type"`
	OwnerLabel         string            `json:"owner_label"`
	Status             string            `json:"status"`
	CarrierMode        string            `json:"carrier_mode"`
	Limits             json.RawMessage   `json:"limits"`
	TelemtLimits       json.RawMessage   `json:"telemt_limits"`
	ExpiresAt          *time.Time        `json:"expires_at"`
	RevokedAt          *time.Time        `json:"revoked_at"`
	Note               string            `json:"note"`
	CreatedAt          time.Time         `json:"created_at"`
	Nodes              []keyNodeJSON     `json:"nodes"`
	Secret             string            `json:"secret,omitempty"`
	Links              []keys.Link       `json:"links,omitempty"`
	ClientSupport      map[string]string `json:"client_support"`
	SubscriptionActive bool              `json:"subscription_active"`
	// Traffic30d is the octets the key moved across all its telemt nodes in the last 30 days.
	Traffic30d int64 `json:"traffic_30d"`
}

func telemtLimitsJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

type keyNodeJSON struct {
	NodeID   uuid.UUID `json:"node_id"`
	NodeName string    `json:"node_name"`
	Hostname string    `json:"hostname"`
	Sync     string    `json:"profile_sync"`
}

func (s *Server) keyJSON(r *http.Request, k db.AccessKey, withSecret bool) keyJSON {
	bindings, _ := s.store.Q.ListKeyBindings(r.Context(), k.ID)
	nodes := make([]keyNodeJSON, 0, len(bindings))
	for _, b := range bindings {
		nodes = append(nodes, keyNodeJSON{NodeID: b.NodeID, NodeName: b.NodeName, Hostname: b.Hostname, Sync: string(b.SyncState)})
	}
	return s.keyJSONWith(r, k, withSecret, nodes, s.hasActiveSubscription(r.Context(), k.ID), s.trafficByKey(r.Context(), []db.AccessKey{k})[k.ID])
}

func (s *Server) keyJSONWith(r *http.Request, k db.AccessKey, withSecret bool, nodes []keyNodeJSON, subscriptionActive bool, traffic30d int64) keyJSON {
	if nodes == nil {
		nodes = []keyNodeJSON{}
	}
	out := keyJSON{
		ID: k.ID, Label: k.Label, Type: string(k.Type), OwnerLabel: k.OwnerLabel, Status: string(k.Status), CarrierMode: k.CarrierMode,
		Limits: k.Limits, TelemtLimits: telemtLimitsJSON(k.TelemtLimits), ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt, Note: k.Note, CreatedAt: k.CreatedAt, Nodes: nodes, ClientSupport: clientSupport,
		SubscriptionActive: subscriptionActive, Traffic30d: traffic30d,
	}
	if withSecret && k.Status != db.KeyStatusRevoked {
		out.Secret, _ = s.keys.Secret(r.Context(), k)
		out.Links, _ = s.keys.Links(r.Context(), k.ID)
	}
	return out
}

// bindingsByKey loads the bindings of a whole page of keys in one query.
func (s *Server) bindingsByKey(ctx context.Context, ks []db.AccessKey) map[uuid.UUID][]keyNodeJSON {
	out := make(map[uuid.UUID][]keyNodeJSON, len(ks))
	if len(ks) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(ks))
	for _, k := range ks {
		ids = append(ids, k.ID)
	}
	rows, err := s.store.Q.ListBindingsForKeys(ctx, ids)
	if err != nil {
		s.log.Error("list key bindings", "err", err)
		return out
	}
	for _, b := range rows {
		out[b.AccessKeyID] = append(out[b.AccessKeyID], keyNodeJSON{NodeID: b.NodeID, NodeName: b.NodeName, Hostname: b.Hostname, Sync: string(b.SyncState)})
	}
	return out
}

func (s *Server) activeSubscriptionsByKey(ctx context.Context, ks []db.AccessKey) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ks))
	if len(ks) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(ks))
	for _, k := range ks {
		ids = append(ids, k.ID)
	}
	rows, err := s.store.Q.ListActiveSubscriptionAccessKeyIDs(ctx, ids)
	if err != nil {
		s.log.Error("list active subscriptions", "err", err)
		return out
	}
	for _, id := range rows {
		out[id] = true
	}
	return out
}

// trafficByKey loads the 30-day traffic of a whole page of keys in one query, mirroring bindingsByKey.
func (s *Server) trafficByKey(ctx context.Context, ks []db.AccessKey) map[uuid.UUID]int64 {
	out := make(map[uuid.UUID]int64, len(ks))
	if len(ks) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(ks))
	for _, k := range ks {
		ids = append(ids, k.ID)
	}
	rows, err := s.store.Q.KeyTrafficLast30d(ctx, db.KeyTrafficLast30dParams{KeyIds: ids, Since: time.Now().Add(-keyStatsRetention)})
	if err != nil {
		s.log.Error("key traffic", "err", err)
		return out
	}
	for _, r := range rows {
		out[r.AccessKeyID] = r.Traffic
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
	Label        string               `json:"label"`
	Type         string               `json:"type"`
	OwnerLabel   string               `json:"owner_label"`
	Note         string               `json:"note"`
	CarrierMode  string               `json:"carrier_mode"`
	Limits       domain.ProfileLimits `json:"limits"`
	TelemtLimits domain.TelemtLimits  `json:"telemt_limits"`
	ExpiresAt    *time.Time           `json:"expires_at"`
	NodeIDs      []uuid.UUID          `json:"node_ids"`
	Prefix       string               `json:"prefix"`
	Count        int                  `json:"count"`
}

func (in keyInput) toCreate(by uuid.UUID) keys.CreateInput {
	cm := in.CarrierMode
	if cm == "" {
		cm = "https"
	}
	return keys.CreateInput{
		Label: in.Label, OwnerLabel: in.OwnerLabel, Note: in.Note, Type: domain.KeyType(in.Type),
		CarrierMode: domain.CarrierMode(cm), Limits: in.Limits, TelemtLimits: in.TelemtLimits,
		ExpiresAt: in.ExpiresAt, NodeIDs: in.NodeIDs, CreatedBy: by,
	}
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
	byKey := s.bindingsByKey(r.Context(), ks)
	subByKey := s.activeSubscriptionsByKey(r.Context(), ks)
	trafficByKey := s.trafficByKey(r.Context(), ks)
	items := make([]keyJSON, 0, len(ks))
	for _, k := range ks {
		items = append(items, s.keyJSONWith(r, k, true, byKey[k.ID], subByKey[k.ID], trafficByKey[k.ID]))
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
	byKey := s.bindingsByKey(r.Context(), rows)
	subByKey := s.activeSubscriptionsByKey(r.Context(), rows)
	trafficByKey := s.trafficByKey(r.Context(), rows)
	items := make([]keyJSON, 0, len(rows))
	for _, k := range rows {
		items = append(items, s.keyJSONWith(r, k, false, byKey[k.ID], subByKey[k.ID], trafficByKey[k.ID]))
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
		Label        *string               `json:"label"`
		OwnerLabel   *string               `json:"owner_label"`
		Note         *string               `json:"note"`
		CarrierMode  *string               `json:"carrier_mode"`
		Limits       *domain.ProfileLimits `json:"limits"`
		TelemtLimits *domain.TelemtLimits  `json:"telemt_limits"`
		ExpiresAt    *time.Time            `json:"expires_at"`
		ClearExpiry  bool                  `json:"clear_expiry"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, err.Error())
		return
	}
	label, owner, note, cm := k.Label, k.OwnerLabel, k.Note, k.CarrierMode
	var limits domain.ProfileLimits
	_ = json.Unmarshal(k.Limits, &limits)
	var telemtLimits domain.TelemtLimits
	_ = json.Unmarshal(telemtLimitsJSON(k.TelemtLimits), &telemtLimits)
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
	if in.TelemtLimits != nil {
		telemtLimits = *in.TelemtLimits
	}
	if in.ExpiresAt != nil {
		exp = in.ExpiresAt
	}
	if in.ClearExpiry {
		exp = nil
	}
	updated, err := s.keys.Update(r.Context(), k.ID, keys.UpdateInput{
		Label: label, OwnerLabel: owner, Note: note, ExpiresAt: exp,
		CarrierMode: domain.CarrierMode(cm), Limits: limits, TelemtLimits: telemtLimits,
	})
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
	links, err := s.keys.NodeLinks(r.Context(), k.ID)
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
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = keys.LinkWeb
	}
	if kind != keys.LinkWeb && kind != keys.LinkTLS {
		badRequest(w, "kind must be web or tls")
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
		if l.NodeID == nodeID && l.Kind == kind {
			png, err := qrlink.PNG(l.TMe, size)
			if err != nil {
				internal(w)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			filename := k.Label + "-" + l.Hostname + "-" + l.Kind + ".png"
			disposition := mime.FormatMediaType("inline", map[string]string{"filename": filename})
			if disposition == "" {
				disposition = "inline"
			}
			w.Header().Set("Content-Disposition", disposition)
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
			err = s.keys.Extend(r.Context(), id, *in.ExpiresAt)
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

const keyStatsRetention = 30 * 24 * time.Hour

// maxKeyStatsPoints bounds one node's series in a /keys/{id}/stats response.
const maxKeyStatsPoints = 1000

// keyStatsSnapshotStep is the stats worker's key-snapshot cadence.
const keyStatsSnapshotStep = 60

func keyStatsStepSeconds(from, to time.Time) int64 {
	span := int64(to.Sub(from) / time.Second)
	if span <= 0 {
		return keyStatsSnapshotStep
	}
	step := (span + maxKeyStatsPoints - 1) / maxKeyStatsPoints
	if step < keyStatsSnapshotStep {
		return keyStatsSnapshotStep
	}
	return step
}

type keyStatsPointJSON struct {
	T           time.Time `json:"t"`
	Connections int32     `json:"connections"`
	TotalOctets int64     `json:"total_octets"`
}

type keyStatsNodeJSON struct {
	NodeID   uuid.UUID           `json:"node_id"`
	NodeName string              `json:"node_name"`
	Points   []keyStatsPointJSON `json:"points"`
}

func (s *Server) handleKeyStats(w http.ResponseWriter, r *http.Request) {
	k, ok := s.loadKey(w, r)
	if !ok {
		return
	}
	from, to, ok := parseFromTo(w, r)
	if !ok {
		return
	}
	rows, err := s.store.Q.ListKeyStatsSnapshotsBucketed(r.Context(), db.ListKeyStatsSnapshotsBucketedParams{
		AccessKeyID: k.ID, FromAt: from, ToAt: to, Step: keyStatsStepSeconds(from, to),
	})
	if err != nil {
		s.log.Error("key stats", "err", err)
		internal(w)
		return
	}
	nodes := []keyStatsNodeJSON{}
	var connectionsNow int32
	var octetsDelta int64
	byNode := map[uuid.UUID]int{}
	prev := map[uuid.UUID]int64{}
	for _, row := range rows {
		idx, seen := byNode[row.NodeID]
		if !seen {
			idx = len(nodes)
			byNode[row.NodeID] = idx
			nodes = append(nodes, keyStatsNodeJSON{NodeID: row.NodeID, NodeName: row.NodeName})
		} else if d := row.TotalOctets - prev[row.NodeID]; d > 0 {
			octetsDelta += d
		}
		prev[row.NodeID] = row.TotalOctets
		nodes[idx].Points = append(nodes[idx].Points, keyStatsPointJSON{T: row.TakenAt, Connections: row.Connections, TotalOctets: row.TotalOctets})
	}
	for i := range nodes {
		pts := nodes[i].Points
		connectionsNow += pts[len(pts)-1].Connections
		nodes[i].Points = capSamples(pts, maxKeyStatsPoints)
	}
	writeJSON(w, 200, map[string]any{
		"nodes":  nodes,
		"totals": map[string]any{"connections_now": connectionsNow, "octets_delta": octetsDelta},
	})
}
