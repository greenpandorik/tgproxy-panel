package api

import (
	"net/http"
	"sync"
	"time"

	"tgwebproxy/internal/updates"
	"tgwebproxy/internal/version"
)

// publicStatusTTL caps how often an anonymous caller can make the panel count nodes.
const publicStatusTTL = 10 * time.Second

// relayCommitShort is how many characters of the pinned MTProxy commit the public status shows.
const relayCommitShort = 7

// publicStatus is the whole anonymous view of the deployment.
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

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	if s.updates == nil {
		writeJSON(w, http.StatusOK, updates.Disabled(s.cfg.GitHubRepo, version.Version))
		return
	}
	writeJSON(w, http.StatusOK, s.updates.Status(r.Context()))
}
