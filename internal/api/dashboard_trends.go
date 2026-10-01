package api

import (
	"net/http"
	"time"

	"tgwebproxy/internal/store/db"
)

const (
	trendsWindow = 24 * time.Hour
	// trendsPeopleStep gives the 24-hour people line 72 points.
	trendsPeopleStep = 20 * 60
	// trendsCoverageSlack is how late the stored history may start and still cover the day before.
	trendsCoverageSlack = time.Hour
)

type peoplePointJSON struct {
	T            time.Time `json:"t"`
	PeopleOnline int32     `json:"people_online"`
}

// handleDashboardTrends is the last day in two figures: people online over it, and traffic
// moved in it next to the day before when the stored history covers that whole day.
func (s *Server) handleDashboardTrends(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	mid := now.Add(-trendsWindow)
	since := mid.Add(-trendsWindow)

	rows, err := s.store.Q.FleetPeopleSeries(r.Context(), db.FleetPeopleSeriesParams{Since: mid, Step: trendsPeopleStep})
	if err != nil {
		internal(w)
		return
	}
	people := make([]peoplePointJSON, 0, len(rows))
	for _, row := range rows {
		people = append(people, peoplePointJSON{T: row.T, PeopleOnline: row.PeopleOnline})
	}

	traffic, err := s.store.Q.FleetTraffic(r.Context(), db.FleetTrafficParams{Since: since, Mid: mid})
	if err != nil {
		internal(w)
		return
	}
	var current, previous *int64
	if traffic.CurrentSteps > 0 {
		current = &traffic.Current
		if traffic.PreviousSteps > 0 && !traffic.Earliest.After(since.Add(trendsCoverageSlack)) {
			previous = &traffic.Previous
		}
	}
	writeJSON(w, 200, map[string]any{"people": people, "traffic_24h": current, "traffic_prev_24h": previous})
}
