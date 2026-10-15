package nodesvc

import (
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/telemt"
)

// MinAgentForWebCapacity is the first agent release that applies a WEB capacity preset.
const MinAgentForWebCapacity = "2.17.4"

// MinAgentForLargeWebCapacity is the first agent release that knows the 2048 preset.
const MinAgentForLargeWebCapacity = "2.17.5"

var (
	minAgentForWebCapacity      = [3]int{2, 17, 4}
	minAgentForLargeWebCapacity = [3]int{2, 17, 5}
	minTelemtForWebCapacity     = [3]int{3, 5, 14}
)

// WebCapacityUnsupported explains why a node cannot take a WEB capacity preset, or returns "".
func WebCapacityUnsupported(n db.Node) string {
	switch {
	case n.Engine != db.NodeEngineTelemt:
		return "WEB capacity applies to telemt nodes only"
	case !telemt.VersionAtLeast(n.AgentVersion, minAgentForWebCapacity):
		return "WEB capacity needs agent " + MinAgentForWebCapacity + " or newer; run tgwp-agent upgrade on the node first"
	case !telemt.VersionAtLeast(n.TelemtVersion, minTelemtForWebCapacity):
		return "WEB capacity needs Telemt 3.5.14 or newer; update Telemt on the node first"
	}
	return ""
}

func WebCapacitySupported(n db.Node) bool { return WebCapacityUnsupported(n) == "" }

// WebCapacityPresetUnsupported explains why a node cannot take this particular preset, or returns "".
func WebCapacityPresetUnsupported(n db.Node, capacity int) string {
	if msg := WebCapacityUnsupported(n); msg != "" {
		return msg
	}
	if capacity > 1024 && !telemt.VersionAtLeast(n.AgentVersion, minAgentForLargeWebCapacity) {
		return "WEB capacity above 1024 needs agent " + MinAgentForLargeWebCapacity + " or newer; run tgwp-agent upgrade on the node first"
	}
	return ""
}

// WebCapacityOf is the node's chosen preset, 0 when the panel has no opinion.
func WebCapacityOf(n db.Node) int {
	if !n.TelemtWebCapacity.Valid {
		return 0
	}
	return int(n.TelemtWebCapacity.Int32)
}
