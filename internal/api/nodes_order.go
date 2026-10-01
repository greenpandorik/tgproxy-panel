package api

import (
	"net/http"

	"github.com/google/uuid"
)

// maxReorderIDs bounds one reorder request.
const maxReorderIDs = 1000

type reorderNodesReq struct {
	IDs []uuid.UUID `json:"ids"`
}

// handleReorderNodes sets the one server order every list in the panel follows.
func (s *Server) handleReorderNodes(w http.ResponseWriter, r *http.Request) {
	var req reorderNodesReq
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxReorderIDs {
		badRequest(w, "ids: list 1 to 1000 servers")
		return
	}
	seen := make(map[uuid.UUID]bool, len(req.IDs))
	for _, id := range req.IDs {
		if seen[id] {
			badRequest(w, "ids: each server may appear once")
			return
		}
		seen[id] = true
	}
	if _, err := s.store.Q.ReorderNodes(r.Context(), req.IDs); err != nil {
		internal(w)
		return
	}
	nodes, err := s.store.Q.ListNodes(r.Context())
	if err != nil {
		internal(w)
		return
	}
	ids := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	s.Audit(r.Context(), "node.reorder", "node", "", map[string]any{"ids": req.IDs})
	writeJSON(w, 200, map[string]any{"ids": ids})
}
