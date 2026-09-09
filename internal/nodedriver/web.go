package nodedriver

// WebCounterSample is one sample of a telemt_web_* family. Carrier, Label and Phase are the
// family's own labels and stay empty for a family that does not carry them.
type WebCounterSample struct {
	Carrier string
	Label   string
	Phase   string
	Value   float64
}

// WebCounterFamily is one metric family. A nil family is one the node did not report, which is
// "not available" and never a zero reading.
type WebCounterFamily struct {
	Samples []WebCounterSample
}

// Total sums the family; ok is false when the family is absent.
func (f *WebCounterFamily) Total() (value float64, ok bool) {
	if f == nil {
		return 0, false
	}
	for _, s := range f.Samples {
		value += s.Value
	}
	return value, true
}

// Get returns the sample with exactly this carrier and label; ok is false when the family is
// absent or carries no such sample.
func (f *WebCounterFamily) Get(carrier, label string) (value float64, ok bool) {
	if f == nil {
		return 0, false
	}
	for _, s := range f.Samples {
		if s.Carrier == carrier && s.Label == label {
			return s.Value, true
		}
	}
	return 0, false
}

type WebLearningState struct {
	Enabled          bool
	PolicyGeneration uint64
	Epoch            uint64
	Entries          int
	Capacity         int
	LifetimeSecs     int64
	HealthSecs       int64
	AgeMs            int64
}

type WebDrainState struct {
	OperationID         string
	State               string
	Outcome             string
	TimeoutSecs         int
	StartedEpochMillis  int64
	DeadlineEpochMillis int64
	RemainingSessions   uint64
	RemainingStreams    uint64
	RemainingWebsockets uint64
	ForceCloseSignalled bool
}

type WebLifecycleState struct {
	State                     string
	Epoch                     uint64
	AgeMs                     int64
	AdmissionOpen             bool
	EffectiveNewWorkAdmission bool
	Drain                     *WebDrainState
}

// WebRuntimeState is the node's WEB runtime as the agent read it. Learning and Lifecycle are
// nil when telemt's status carried no such section.
type WebRuntimeState struct {
	RuntimeInstance    string
	CarrierNegotiation bool
	Learning           *WebLearningState
	Lifecycle          *WebLifecycleState
}

// WebTelemetry is one heartbeat's view of the WEB transport. Every field is a pointer so that
// a family telemt did not expose stays absent all the way into the snapshot row.
type WebTelemetry struct {
	CarrierSelections *WebCounterFamily
	CarrierFailures   *WebCounterFamily
	LearningOutcomes  *WebCounterFamily
	LearningEntries   *WebCounterFamily
	Rejections        *WebCounterFamily
	SessionClosures   *WebCounterFamily
	BridgeRecovery    *WebCounterFamily
	Runtime           *WebRuntimeState
}

// TelemtCapabilities maps a capability name to what the node answered. A nil value is a
// capability the probe could not settle, which marshals as an explicit null so that "we could
// not ask" stays distinct from "the node does not support it".
type TelemtCapabilities map[string]*bool

// Determined returns the capability's value and whether it was settled at all.
func (c TelemtCapabilities) Determined(name string) (value, ok bool) {
	v, present := c[name]
	if !present || v == nil {
		return false, false
	}
	return *v, true
}
