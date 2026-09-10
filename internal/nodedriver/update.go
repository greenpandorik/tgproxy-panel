package nodedriver

import (
	"errors"
	"time"

	agentv1 "tgwebproxy/proto/agent/v1"
)

// TelemtUpdateRequest is the build the panel pinned, as it is handed to one node.
type TelemtUpdateRequest struct {
	Version, SHA256, URL string
	DrainTimeoutSecs     int
	// RequireDrain refuses the update when the node's drain support is undetermined.
	RequireDrain bool
}

// UpdateStep is one step of the node's maintenance sequence.
type UpdateStep struct {
	Key               string     `json:"key"`
	State             string     `json:"state"`
	Message           string     `json:"message"`
	RemainingSessions uint64     `json:"remaining_sessions"`
	RemainingStreams  uint64     `json:"remaining_streams"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
}

// TelemtUpdate is one update as the node reports it. Outcome is the only field that says
// which state the node ended in; OK is true for a finished update and nothing else.
type TelemtUpdate struct {
	JobID       string       `json:"job_id"`
	Phase       string       `json:"phase"`
	Outcome     string       `json:"outcome"`
	OK          bool         `json:"ok"`
	FromVersion string       `json:"from_version"`
	ToVersion   string       `json:"to_version"`
	Error       string       `json:"error,omitempty"`
	Drained     bool         `json:"drained"`
	RolledBack  bool         `json:"rolled_back"`
	Steps       []UpdateStep `json:"steps"`
	StartedAt   *time.Time   `json:"started_at,omitempty"`
	FinishedAt  *time.Time   `json:"finished_at,omitempty"`
}

// Done reports whether the node has finished this update.
func (u TelemtUpdate) Done() bool { return u.Phase == "done" }

func TelemtUpdateRequestToProto(r TelemtUpdateRequest) *agentv1.UpdateTelemtRequest {
	return &agentv1.UpdateTelemtRequest{
		Version: r.Version, Sha256: r.SHA256, Url: r.URL,
		DrainTimeoutSecs: int32(r.DrainTimeoutSecs), RequireDrain: r.RequireDrain,
	}
}

func TelemtUpdateFromProto(u *agentv1.TelemtUpdate) TelemtUpdate {
	out := TelemtUpdate{
		JobID: u.GetJobId(), Phase: u.GetPhase(), Outcome: u.GetOutcome(), OK: u.GetOk(),
		FromVersion: u.GetFromVersion(), ToVersion: u.GetToVersion(), Error: u.GetError(),
		Drained: u.GetDrained(), RolledBack: u.GetRolledBack(),
		Steps:     make([]UpdateStep, 0, len(u.GetSteps())),
		StartedAt: unixMillis(u.GetStartedUnixMs()), FinishedAt: unixMillis(u.GetFinishedUnixMs()),
	}
	for _, s := range u.GetSteps() {
		out.Steps = append(out.Steps, UpdateStep{
			Key: s.GetKey(), State: s.GetState(), Message: s.GetMessage(),
			RemainingSessions: s.GetRemainingSessions(), RemainingStreams: s.GetRemainingStreams(),
			StartedAt: unixMillis(s.GetStartedUnixMs()), FinishedAt: unixMillis(s.GetFinishedUnixMs()),
		})
	}
	return out
}

func unixMillis(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

func telemtUpdateFrom(resp *agentv1.Response, err error) (TelemtUpdate, error) {
	if err != nil {
		return TelemtUpdate{}, err
	}
	u := resp.GetTelemtUpdate()
	if u == nil {
		return TelemtUpdate{}, errors.New("empty telemt update response")
	}
	return TelemtUpdateFromProto(u), nil
}
