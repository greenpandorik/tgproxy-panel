package api

import (
	"net/http"
	"sync"
	"time"

	"tgwebproxy/internal/updates"
	"tgwebproxy/internal/version"
)

// publicStatusTTL caps how often an anonymous caller can make the panel count
// nodes. The login screen polls this while nobody is logged in, and the
// numbers move on the heartbeat timescale anyway, so a 10s window is both
// fresh enough and a hard ceiling on the query rate a scraper can produce.
const publicStatusTTL = 10 * time.Second

// relayCommitShort is how many characters of the pinned MTProxy commit the
// public status shows - the same short form git and the node UI use.
const relayCommitShort = 7

// publicStatus is the whole anonymous view of the deployment.
//
// Everything in it is either already public (the version the SPA was built
// from, the relay commit that is baked into the published install script) or a
// bare count. Deliberately absent: node names, hostnames, IPs, key counts,
// alert text - anything that would let a stranger fingerprint or target a
// specific node. Adding a field here means re-reading that sentence.
type publicStatus struct {
	Version     string `json:"version"`
	NodesTotal  int    `json:"nodes_total"`
	NodesOnline int    `json:"nodes_online"`
	RelayCommit string `json:"relay_commit"`
}

type publicStatusCache struct {
	mu       sync.Mutex
	body     publicStatus
	cachedAt time.Time
}

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	s.publicStatus.mu.Lock()
	defer s.publicStatus.mu.Unlock()

	if time.Since(s.publicStatus.cachedAt) >= publicStatusTTL {
		rows, err := s.store.Q.CountNodesByStatus(r.Context())
		if err != nil {
			internal(w)
			return
		}
		var total, online int
		for _, c := range rows {
			total += int(c.N)
			if string(c.Status) == "online" {
				online = int(c.N)
			}
		}
		commit := s.cfg.TProxyCommit
		if len(commit) > relayCommitShort {
			commit = commit[:relayCommitShort]
		}
		s.publicStatus.body = publicStatus{
			Version:     version.Version,
			NodesTotal:  total,
			NodesOnline: online,
			RelayCommit: commit,
		}
		s.publicStatus.cachedAt = time.Now()
	}

	w.Header().Set("Cache-Control", "public, max-age=10")
	writeJSON(w, http.StatusOK, s.publicStatus.body)
}

// handleUpdateStatus reports whether a newer panel release exists, from the
// updates.Checker's cache (refreshed at most hourly). With UPDATE_CHECK=false
// the checker is nil and the answer says so, with the version and repo link
// still filled in so the UI can render a plain chip.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	if s.updates == nil {
		writeJSON(w, http.StatusOK, updates.Disabled(s.cfg.GitHubRepo, version.Version))
		return
	}
	writeJSON(w, http.StatusOK, s.updates.Status(r.Context()))
}
