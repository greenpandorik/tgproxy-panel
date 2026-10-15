package nodesvc

import (
	"tgwebproxy/internal/store/db"
	"tgwebproxy/internal/telemt"
)

// MinAgentForWebCapacity is the first agent release that applies a WEB capacity preset.
const MinAgentForWebCapacity = "2.17.4"

var (
	minAgentForWebCapacity  = [3]int{2, 17, 4}
	minTelemtForWebCapacity = [3]int{3, 5, 14}
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

// WebCapacityOf is the node's chosen preset, 0 when the panel has no opinion.
func WebCapacityOf(n db.Node) int {
	if !n.TelemtWebCapacity.Valid {
		return 0
	}
	return int(n.TelemtWebCapacity.Int32)
}
