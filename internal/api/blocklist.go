package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"tgwebproxy/internal/blocklist"
	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

type blocklistEntryJSON struct {
	Prefix  string    `json:"prefix"`
	Note    string    `json:"note"`
	AddedAt time.Time `json:"added_at"`
	Packets *uint64   `json:"packets"`
	Bytes   *uint64   `json:"bytes"`
}

type blocklistJSON struct {
	Entries        []blocklistEntryJSON `json:"entries"`
	Revision       int64                `json:"revision"`
	UpdatedAt      *time.Time           `json:"updated_at"`
	MaxEntries     int                  `json:"max_entries"`
	Supported      *bool                `json:"supported"`
	Live           bool                 `json:"live"`
	Synced         bool                 `json:"synced"`
	NodeRevision   *int64               `json:"node_revision"`
	DroppedPackets *uint64              `json:"dropped_packets"`
	DroppedBytes   *uint64              `json:"dropped_bytes"`
	NodeError      string               `json:"node_error"`
	ApplyError     string               `json:"apply_error,omitempty"`
}

func (s *Server) nodeBlocklist(ctx context.Context, n db.Node) (db.NodeBlocklist, []blocklist.Entry, error) {
	row, err := s.store.Q.GetNodeBlocklist(ctx, n.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.NodeBlocklist{NodeID: n.ID}, []blocklist.Entry{}, nil
	}
	if err != nil {
		return row, nil, err
	}
	entries := []blocklist.Entry{}
	if err := json.Unmarshal(row.Entries, &entries); err != nil {
		return row, nil, err
	}
	return row, entries, nil
}

func reportedFirewall(n db.Node) *nodedriver.FirewallStatus {
	var h nodedriver.HealthReport
	if json.Unmarshal(n.LastHealth, &h) != nil {
		return nil
	}
	return h.Firewall
}

func blocklistView(n db.Node, row db.NodeBlocklist, entries []blocklist.Entry, live *nodedriver.FirewallStatus, liveErr error) blocklistJSON {
	out := blocklistJSON{Entries: make([]blocklistEntryJSON, 0, len(entries)), Revision: row.Revision, MaxEntries: blocklist.MaxEntries}
	if row.Revision > 0 {
		out.UpdatedAt = &row.UpdatedAt
	}
	status := live
	switch {
	case live != nil:
		out.Live = true
	case errors.Is(liveErr, nodedriver.ErrUnsupportedFirewall):
	default:
		status = reportedFirewall(n)
	}
	yes, no := true, false
	switch {
	case status != nil:
		out.Supported = &yes
	case errors.Is(liveErr, nodedriver.ErrUnsupportedFirewall), liveErr == nil && live == nil:
		out.Supported = &no
	}
	counters := map[string]nodedriver.FirewallCounter{}
	if status != nil {
		out.NodeRevision = &status.Revision
		out.DroppedPackets, out.DroppedBytes = &status.DroppedPackets, &status.DroppedBytes
		out.NodeError = status.Error
		for _, c := range status.Counters {
			counters[c.Entry] = c
		}
	}
	out.Synced = row.Revision == 0 || (status != nil && status.Revision == row.Revision)
	for _, e := range entries {
		item := blocklistEntryJSON{Prefix: e.Prefix, Note: e.Note, AddedAt: e.AddedAt}
		if c, ok := counters[e.Prefix]; ok && out.Synced {
			item.Packets, item.Bytes = &c.Packets, &c.Bytes
		}
		out.Entries = append(out.Entries, item)
	}
	return out
}

func prefixesOf(entries []blocklist.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Prefix
	}
	return out
}

func (s *Server) handleGetNodeBlocklist(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	row, entries, err := s.nodeBlocklist(r.Context(), n)
	if err != nil {
		internal(w)
		return
	}
	var live *nodedriver.FirewallStatus
	var liveErr error
	if d, ok := s.driver.(nodedriver.FirewallDriver); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		st, err := d.Firewall(ctx, n.ID, false, 0, nil)
		cancel()
		if err == nil {
			live = &st
		}
		liveErr = err
	}
	writeJSON(w, 200, blocklistView(n, row, entries, live, liveErr))
}

func (s *Server) handlePutNodeBlocklist(w http.ResponseWriter, r *http.Request) {
	n, ok := s.loadNode(w, r)
	if !ok {
		return
	}
	var in struct {
		Entries []struct {
			Prefix string `json:"prefix"`
			Note   string `json:"note"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	raw := make([]string, len(in.Entries))
	for i, e := range in.Entries {
		raw[i] = e.Prefix
	}
	normalized, problems := blocklist.Normalize(raw)
	fields := map[string]string{}
	for i, msg := range problems {
		if i < 0 {
			fields["entries"] = msg
			continue
		}
		fields[fmt.Sprintf("entries.%d", i)] = msg
	}
	for i, e := range in.Entries {
		if utf8.RuneCountInString(strings.TrimSpace(e.Note)) > blocklist.MaxNote {
			fields[fmt.Sprintf("entries.%d", i)] = fmt.Sprintf("note longer than %d characters", blocklist.MaxNote)
		}
	}
	if len(fields) > 0 {
		validation(w, fields)
		return
	}
	_, old, err := s.nodeBlocklist(r.Context(), n)
	if err != nil {
		internal(w)
		return
	}
	added := map[string]time.Time{}
	for _, e := range old {
		added[e.Prefix] = e.AddedAt
	}
	now := time.Now().UTC()
	entries := make([]blocklist.Entry, len(normalized))
	for i, p := range normalized {
		at, seen := added[p]
		if !seen {
			at = now
		}
		entries[i] = blocklist.Entry{Prefix: p, Note: strings.TrimSpace(in.Entries[i].Note), AddedAt: at}
	}
	body, _ := json.Marshal(entries)
	row, err := s.store.Q.SaveNodeBlocklist(r.Context(), db.SaveNodeBlocklistParams{NodeID: n.ID, Entries: body})
	if err != nil {
		internal(w)
		return
	}
	s.Audit(r.Context(), "node.blocklist", "node", n.ID.String(), map[string]any{"entries": len(entries)})

	var live *nodedriver.FirewallStatus
	var liveErr error
	applyError := ""
	if d, ok := s.driver.(nodedriver.FirewallDriver); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		st, err := d.Firewall(ctx, n.ID, true, row.Revision, prefixesOf(entries))
		cancel()
		if err == nil {
			live = &st
		} else if !errors.Is(err, nodedriver.ErrOffline) {
			applyError = err.Error()
		}
		liveErr = err
	}
	out := blocklistView(n, row, entries, live, liveErr)
	out.ApplyError = applyError
	writeJSON(w, 200, out)
}
