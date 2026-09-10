package telemt

import (
	"sort"
	"strconv"
	"strings"
)

// telemt_web_* metric families the panel reads.
const (
	MetricCarrierSelections = "telemt_web_carrier_selections_total"
	MetricCarrierFailures   = "telemt_web_carrier_reported_failures_total"
	MetricLearningOutcomes  = "telemt_web_carrier_learning_outcomes_total"
	MetricLearningEntries   = "telemt_web_carrier_learning_entries"
	MetricRejections        = "telemt_web_rejections_total"
	MetricSessionClosures   = "telemt_web_session_closures_total"
	MetricBridgeRecovery    = "telemt_web_bridge_recovery_events_total"
	MetricTLSFrontDomains   = "telemt_tls_front_profile_domains"
	MetricHandshakeFailures = "telemt_handshake_failures_by_class_total"
)

// LabelCounters is a metric family split by one label. Present is false when the family never
// appeared in the exposition, which is "not available" and not a zero reading.
type LabelCounters struct {
	Present bool
	Values  map[string]float64
}

// Get returns one label's value and whether that label was reported.
func (f LabelCounters) Get(label string) (float64, bool) {
	v, ok := f.Values[label]
	return v, ok
}

func (f LabelCounters) Total() float64 {
	var sum float64
	for _, v := range f.Values {
		sum += v
	}
	return sum
}

// Labels returns the reported label values in sorted order.
func (f LabelCounters) Labels() []string { return sortedKeys(f.Values) }

// CarrierCounters is a metric family split by carrier and one further label.
type CarrierCounters struct {
	Present bool
	Values  map[string]map[string]float64
}

// Carrier returns one carrier's slice of the family, absent when the family itself is absent.
func (f CarrierCounters) Carrier(name string) LabelCounters {
	if !f.Present {
		return LabelCounters{}
	}
	return LabelCounters{Present: true, Values: f.Values[name]}
}

// Carriers returns the reported carriers in sorted order.
func (f CarrierCounters) Carriers() []string { return sortedKeys(f.Values) }

// CarrierTotals sums each carrier's samples.
func (f CarrierCounters) CarrierTotals() map[string]float64 {
	if !f.Present {
		return nil
	}
	out := make(map[string]float64, len(f.Values))
	for carrier, byLabel := range f.Values {
		var sum float64
		for _, v := range byLabel {
			sum += v
		}
		out[carrier] = sum
	}
	return out
}

func (f CarrierCounters) Total() float64 {
	var sum float64
	for _, byLabel := range f.Values {
		for _, v := range byLabel {
			sum += v
		}
	}
	return sum
}

// CarrierFailureKey is the phase/reason pair of telemt_web_carrier_reported_failures_total.
type CarrierFailureKey struct {
	Phase  string
	Reason string
}

// CarrierFailures is the failure family, split by carrier and then by phase and reason.
type CarrierFailures struct {
	Present bool
	Values  map[string]map[CarrierFailureKey]float64
}

// CarrierTotals sums each carrier's failures.
func (f CarrierFailures) CarrierTotals() map[string]float64 {
	if !f.Present {
		return nil
	}
	out := make(map[string]float64, len(f.Values))
	for carrier, byKey := range f.Values {
		var sum float64
		for _, v := range byKey {
			sum += v
		}
		out[carrier] = sum
	}
	return out
}

func (f CarrierFailures) Total() float64 {
	var sum float64
	for _, byKey := range f.Values {
		for _, v := range byKey {
			sum += v
		}
	}
	return sum
}

// WebMetrics is the telemt_web_* part of the Prometheus exposition.
type WebMetrics struct {
	CarrierSelections CarrierCounters
	CarrierFailures   CarrierFailures
	LearningOutcomes  CarrierCounters
	LearningEntries   LabelCounters
	Rejections        LabelCounters
	SessionClosures   CarrierCounters
	BridgeRecovery    LabelCounters
	TLSFrontDomains   LabelCounters
	HandshakeFailures LabelCounters
}

// ParseWebMetrics reads the telemt_web_* families out of Prometheus exposition text.
func ParseWebMetrics(text string) WebMetrics {
	var m WebMetrics
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if name, ok := metadataFamily(line); ok {
				m.markPresent(name)
			}
			continue
		}
		name, labels, value, ok := parseSample(line)
		if !ok {
			continue
		}
		m.add(name, labels, value)
	}
	return m
}

func (m *WebMetrics) markPresent(family string) {
	switch family {
	case MetricCarrierSelections:
		m.CarrierSelections.Present = true
	case MetricCarrierFailures:
		m.CarrierFailures.Present = true
	case MetricLearningOutcomes:
		m.LearningOutcomes.Present = true
	case MetricLearningEntries:
		m.LearningEntries.Present = true
	case MetricRejections:
		m.Rejections.Present = true
	case MetricSessionClosures:
		m.SessionClosures.Present = true
	case MetricBridgeRecovery:
		m.BridgeRecovery.Present = true
	case MetricTLSFrontDomains:
		m.TLSFrontDomains.Present = true
	case MetricHandshakeFailures:
		m.HandshakeFailures.Present = true
	}
}

func (m *WebMetrics) add(family string, labels map[string]string, value float64) {
	m.markPresent(family)
	switch family {
	case MetricCarrierSelections:
		setCarrier(&m.CarrierSelections, labels["carrier"], labels["disposition"], value)
	case MetricLearningOutcomes:
		setCarrier(&m.LearningOutcomes, labels["carrier"], labels["outcome"], value)
	case MetricSessionClosures:
		setCarrier(&m.SessionClosures, labels["carrier"], labels["reason"], value)
	case MetricCarrierFailures:
		if m.CarrierFailures.Values == nil {
			m.CarrierFailures.Values = map[string]map[CarrierFailureKey]float64{}
		}
		carrier := labels["carrier"]
		if m.CarrierFailures.Values[carrier] == nil {
			m.CarrierFailures.Values[carrier] = map[CarrierFailureKey]float64{}
		}
		m.CarrierFailures.Values[carrier][CarrierFailureKey{Phase: labels["phase"], Reason: labels["reason"]}] = value
	case MetricLearningEntries:
		setLabel(&m.LearningEntries, labels["kind"], value)
	case MetricRejections:
		setLabel(&m.Rejections, labels["reason"], value)
	case MetricBridgeRecovery:
		setLabel(&m.BridgeRecovery, labels["event"], value)
	case MetricTLSFrontDomains:
		setLabel(&m.TLSFrontDomains, labels["status"], value)
	case MetricHandshakeFailures:
		setLabel(&m.HandshakeFailures, labels["class"], value)
	}
}

func setLabel(f *LabelCounters, label string, value float64) {
	if f.Values == nil {
		f.Values = map[string]float64{}
	}
	f.Values[label] = value
}

func setCarrier(f *CarrierCounters, carrier, label string, value float64) {
	if f.Values == nil {
		f.Values = map[string]map[string]float64{}
	}
	if f.Values[carrier] == nil {
		f.Values[carrier] = map[string]float64{}
	}
	f.Values[carrier][label] = value
}

// metadataFamily returns the family a "# HELP name ..." or "# TYPE name ..." line describes.
func metadataFamily(line string) (string, bool) {
	fields := strings.Fields(strings.TrimPrefix(line, "#"))
	if len(fields) < 2 {
		return "", false
	}
	if fields[0] != "HELP" && fields[0] != "TYPE" {
		return "", false
	}
	return fields[1], true
}

func parseSample(line string) (name string, labels map[string]string, value float64, ok bool) {
	var rest string
	if i := strings.IndexByte(line, '{'); i >= 0 && i < indexSpace(line) {
		name = line[:i]
		labels, rest, ok = parseLabels(line[i+1:])
		if !ok {
			return "", nil, 0, false
		}
	} else {
		cut := indexSpace(line)
		if cut < 0 {
			return "", nil, 0, false
		}
		name, rest = line[:cut], line[cut:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", nil, 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", nil, 0, false
	}
	return name, labels, v, true
}

// parseLabels reads `k="v",k2="v2"}` and returns the pairs plus whatever follows the brace.
func parseLabels(s string) (map[string]string, string, bool) {
	labels := map[string]string{}
	for {
		s = strings.TrimLeft(s, " ,")
		if rest, found := strings.CutPrefix(s, "}"); found {
			return labels, rest, true
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			return nil, "", false
		}
		key := strings.TrimSpace(s[:eq])
		s = strings.TrimLeft(s[eq+1:], " ")
		if !strings.HasPrefix(s, `"`) {
			return nil, "", false
		}
		var value strings.Builder
		i := 1
		for i < len(s) && s[i] != '"' {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				switch s[i] {
				case 'n':
					value.WriteByte('\n')
				default:
					value.WriteByte(s[i])
				}
				i++
				continue
			}
			value.WriteByte(s[i])
			i++
		}
		if i >= len(s) {
			return nil, "", false
		}
		labels[key] = value.String()
		s = s[i+1:]
	}
}

func indexSpace(s string) int {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return i
	}
	return len(s)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
