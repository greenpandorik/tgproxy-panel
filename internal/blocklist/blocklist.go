// Package blocklist checks the addresses and networks a server refuses connections from.
package blocklist

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// MaxEntries is how many addresses and networks one server may block.
const MaxEntries = 2000

// MaxNote is the longest note an entry may carry.
const MaxNote = 200

// Entry is one blocked address or network as the panel keeps it.
type Entry struct {
	Prefix  string    `json:"prefix"`
	Note    string    `json:"note,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

var (
	errTooWide = errors.New("too wide")
	errSpecial = errors.New("not a public address")
)

// Parse reads an address or a network in CIDR form and returns the network it covers.
func Parse(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	var p netip.Prefix
	if strings.Contains(s, "/") {
		parsed, err := netip.ParsePrefix(s)
		if err != nil {
			return p, errors.New("not an address or network")
		}
		p = parsed
	} else {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return p, errors.New("not an address or network")
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	if p.Addr().Zone() != "" {
		return p, errors.New("not an address or network")
	}
	if p.Addr().Is4In6() && p.Bits() >= 96 {
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	p = p.Masked()
	a := p.Addr()
	switch {
	case a.Is4() && p.Bits() < 8, a.Is6() && p.Bits() < 16:
		return p, errTooWide
	case a.IsLoopback(), a.IsUnspecified(), a.IsMulticast(), a.IsLinkLocalUnicast():
		return p, errSpecial
	}
	return p, nil
}

// Format is how an entry is stored and shown: a bare address for a single host.
func Format(p netip.Prefix) string {
	if p.IsSingleIP() {
		return p.Addr().String()
	}
	return p.String()
}

// Normalize checks every entry and returns them in stored form; problems are keyed by index.
func Normalize(prefixes []string) ([]string, map[int]string) {
	problems := map[int]string{}
	if len(prefixes) > MaxEntries {
		problems[-1] = fmt.Sprintf("at most %d entries", MaxEntries)
		return nil, problems
	}
	parsed := make([]netip.Prefix, len(prefixes))
	for i, s := range prefixes {
		p, err := Parse(s)
		if err != nil {
			problems[i] = err.Error()
			continue
		}
		parsed[i] = p
	}
	for i := range parsed {
		if _, bad := problems[i]; bad {
			continue
		}
		for j := range i {
			if _, bad := problems[j]; bad {
				continue
			}
			a, b := parsed[j], parsed[i]
			switch {
			case a == b:
				problems[i] = fmt.Sprintf("same as line %d", j+1)
			case a.Overlaps(b) && a.Bits() <= b.Bits():
				problems[i] = fmt.Sprintf("already covered by %s", Format(a))
			case a.Overlaps(b):
				problems[i] = fmt.Sprintf("covers %s on line %d", Format(a), j+1)
			}
			if _, bad := problems[i]; bad {
				break
			}
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}
	out := make([]string, len(parsed))
	for i, p := range parsed {
		out[i] = Format(p)
	}
	return out, nil
}
