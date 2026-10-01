package nodedriver

import (
	"net/netip"
	"strings"
)

// maxUserIPs is how many of one user's addresses the panel keeps from a single reading.
const maxUserIPs = 256

// SplitIPLists takes the per-user address lists ("user.<name>.ip_list") out of a node's stats.
// The counters come back in a new map, the lists keyed by user name. A nil map stays nil.
func SplitIPLists(stats map[string]string) (map[string]string, map[string][]string) {
	if stats == nil {
		return nil, nil
	}
	counters := make(map[string]string, len(stats))
	lists := map[string][]string{}
	for k, v := range stats {
		if rest, ok := strings.CutPrefix(k, "user."); ok {
			if user, ok := strings.CutSuffix(rest, ".ip_list"); ok {
				lists[user] = parseIPList(v)
				continue
			}
		}
		counters[k] = v
	}
	return counters, lists
}

func parseIPList(v string) []string {
	out := []string{}
	seen := map[netip.Addr]bool{}
	for _, part := range strings.Split(v, ",") {
		addr, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		addr = addr.Unmap().WithZone("")
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr.String())
		if len(out) == maxUserIPs {
			break
		}
	}
	return out
}
