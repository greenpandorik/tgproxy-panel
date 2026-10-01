package subscription

import (
	"net"
	"strings"
)

// MissesPerHour is how many links that do not exist one visitor may try in an hour before
// every page is refused to them for an hour.
const MissesPerHour = 20

// PlausibleToken reports whether s can be a subscription token or a short address at all, so
// anything else is refused before it costs a lookup, a cache entry or a log line.
func PlausibleToken(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !tokenRune(r) {
			return false
		}
	}
	return true
}

func tokenRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return r == '-' || r == '_'
	}
}

// VisitorKey groups addresses one visitor can switch between: an IPv6 /64 counts as one.
func VisitorKey(ip string) string {
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return ip
	}
	if addr.To4() != nil {
		return addr.String()
	}
	return addr.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
