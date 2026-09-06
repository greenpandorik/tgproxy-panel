package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tgwebproxy/internal/store/db"
)

// nonEmpty returns a pointer to s for use as a nullable sqlc.narg param, or
// nil when s is empty - the query treats a nil param as "no filter".
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// likeEscaper escapes the characters that are special to a Postgres LIKE
// pattern (%, _ and the escape character itself, \) so a caller-supplied
// prefix is matched literally. ListAuditFiltered/CountAuditFiltered build
// their pattern as `action LIKE $1 || '%' ESCAPE '\'`, so an ?action=key.%
// filter matches only actions literally starting with "key.%" instead of
// "key." followed by anything.
var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// escapeLikePrefix escapes s for safe use as a LIKE prefix under ESCAPE '\'.
func escapeLikePrefix(s string) string {
	return likeEscaper.Replace(s)
}

// parseFilterTime parses an RFC3339 query param into a nullable timestamp.
// An empty string means "no filter" (nil, no error); a malformed value is
// reported so the handler can return 400.
func parseFilterTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// maxAuditPage caps the requested page number. maxAuditPage * the 200-row per_page ceiling
// stays well inside int32, which is what the generated OFFSET parameter is; the audit log
// would need 20M rows before the cap could hide anything, and nothing pages that far by hand.
const maxAuditPage = 100000

func (s *Server) mountAudit(r chi.Router) {
	r.Get("/audit", s.handleListAudit)
}

type auditJSON struct {
	ID         int64           `json:"id"`
	Username   string          `json:"username,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Meta       json.RawMessage `json:"meta"`
	IP         string          `json:"ip"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > maxAuditPage {
		page = maxAuditPage
	}
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per < 1 || per > 200 {
		per = 50
	}
	// Computed in int64 and clamped above: (page-1)*per used to be an int32 conversion, so
	// page beyond ~10.7M wrapped to a negative OFFSET, which Postgres rejects and the handler
	// surfaced as a 500.
	offset := int64(page-1) * int64(per)

	from, err := parseFilterTime(q.Get("from"))
	if err != nil {
		badRequest(w, "invalid from")
		return
	}
	to, err := parseFilterTime(q.Get("to"))
	if err != nil {
		badRequest(w, "invalid to")
		return
	}
	filter := db.ListAuditFilteredParams{
		Limit:    int32(per),
		Offset:   int32(offset),
		Action:   nonEmpty(escapeLikePrefix(q.Get("action"))),
		Username: nonEmpty(q.Get("user")),
		FromAt:   from,
		ToAt:     to,
	}

	rows, err := s.store.Q.ListAuditFiltered(r.Context(), filter)
	if err != nil {
		internal(w)
		return
	}
	total, err := s.store.Q.CountAuditFiltered(r.Context(), db.CountAuditFilteredParams{
		Action:   filter.Action,
		Username: filter.Username,
		FromAt:   filter.FromAt,
		ToAt:     filter.ToAt,
	})
	if err != nil {
		internal(w)
		return
	}
	items := make([]auditJSON, 0, len(rows))
	for _, a := range rows {
		item := auditJSON{ID: a.ID, Action: a.Action, TargetType: a.TargetType, TargetID: a.TargetID, Meta: a.Meta, IP: a.Ip, CreatedAt: a.CreatedAt}
		if a.Username != nil {
			item.Username = *a.Username
		}
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "per_page": per})
}
