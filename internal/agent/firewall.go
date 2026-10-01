package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"tgwebproxy/internal/blocklist"
	agentv1 "tgwebproxy/proto/agent/v1"
)

const firewallTable = "tgwp_block"

type firewallState struct {
	Revision int64    `json:"revision"`
	Entries  []string `json:"entries"`
}

type nftCounter struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

func (h *Handler) firewallPath() string { return filepath.Join(h.cfg.StateDir, "blocklist.json") }

func renderFirewall(entries []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s\ndelete table inet %s\n", firewallTable, firewallTable)
	if len(entries) == 0 {
		return b.String()
	}
	var v4, v6 []string
	for _, e := range entries {
		p, err := blocklist.Parse(e)
		if err != nil {
			continue
		}
		if p.Addr().Is4() {
			v4 = append(v4, p.String())
		} else {
			v6 = append(v6, p.String())
		}
	}
	fmt.Fprintf(&b, "table inet %s {\n", firewallTable)
	writeNftSet(&b, "v4", "ipv4_addr", v4)
	writeNftSet(&b, "v6", "ipv6_addr", v6)
	b.WriteString("\tchain prerouting {\n" +
		"\t\ttype filter hook prerouting priority -150; policy accept;\n" +
		"\t\tct direction reply accept\n" +
		"\t\tip saddr @v4 counter drop\n" +
		"\t\tip6 saddr @v6 counter drop\n" +
		"\t}\n}\n")
	return b.String()
}

func writeNftSet(b *strings.Builder, name, typ string, elems []string) {
	fmt.Fprintf(b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n\t\tcounter\n", name, typ)
	if len(elems) > 0 {
		fmt.Fprintf(b, "\t\telements = { %s }\n", strings.Join(elems, ", "))
	}
	b.WriteString("\t}\n")
}

func nftValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var p struct {
		Prefix *struct {
			Addr string `json:"addr"`
			Len  int    `json:"len"`
		} `json:"prefix"`
	}
	if json.Unmarshal(raw, &p) == nil && p.Prefix != nil {
		return fmt.Sprintf("%s/%d", p.Prefix.Addr, p.Prefix.Len)
	}
	return ""
}

// parseNftCounters reads `nft -j list table` output: the drop rules' totals and each set element's counter.
func parseNftCounters(out []byte) (nftCounter, map[string]nftCounter, error) {
	var doc struct {
		Nftables []struct {
			Set *struct {
				Elem []json.RawMessage `json:"elem"`
			} `json:"set"`
			Rule *struct {
				Expr []map[string]json.RawMessage `json:"expr"`
			} `json:"rule"`
		} `json:"nftables"`
	}
	var total nftCounter
	per := map[string]nftCounter{}
	if err := json.Unmarshal(out, &doc); err != nil {
		return total, per, err
	}
	for _, item := range doc.Nftables {
		if item.Set != nil {
			for _, raw := range item.Set.Elem {
				var wrapped struct {
					Elem *struct {
						Val     json.RawMessage `json:"val"`
						Counter *nftCounter     `json:"counter"`
					} `json:"elem"`
				}
				if json.Unmarshal(raw, &wrapped) != nil || wrapped.Elem == nil || wrapped.Elem.Counter == nil {
					continue
				}
				p, err := blocklist.Parse(nftValue(wrapped.Elem.Val))
				if err != nil {
					continue
				}
				per[blocklist.Format(p)] = *wrapped.Elem.Counter
			}
		}
		if item.Rule != nil {
			var c *nftCounter
			drops := false
			for _, e := range item.Rule.Expr {
				if raw, ok := e["counter"]; ok {
					var parsed nftCounter
					if json.Unmarshal(raw, &parsed) == nil {
						c = &parsed
					}
				}
				if _, ok := e["drop"]; ok {
					drops = true
				}
			}
			if drops && c != nil {
				total.Packets += c.Packets
				total.Bytes += c.Bytes
			}
		}
	}
	return total, per, nil
}

func (h *Handler) enforceFirewall(ctx context.Context, entries []string) error {
	if err := os.MkdirAll(h.cfg.StateDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(h.cfg.StateDir, "blocklist.nft")
	if err := writeAtomic(path, []byte(renderFirewall(entries)), 0o600); err != nil {
		return err
	}
	out, err := h.exec.Run(ctx, "nft", "-f", path)
	switch {
	case errors.Is(err, exec.ErrNotFound) && len(entries) == 0:
		return nil
	case errors.Is(err, exec.ErrNotFound):
		return errors.New("nftables is not installed on the server: apt-get install nftables")
	case err != nil:
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("nft refused the blocklist: %s", msg)
	}
	return nil
}

func (h *Handler) configureFirewall(ctx context.Context, req *agentv1.FirewallRequest) (*agentv1.FirewallStatus, error) {
	h.fwMu.Lock()
	defer h.fwMu.Unlock()
	if req.GetSet() {
		entries, problems := blocklist.Normalize(req.GetEntries())
		if len(problems) > 0 {
			keys := make([]int, 0, len(problems))
			for k := range problems {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			return nil, fmt.Errorf("blocklist entry %d: %s", keys[0]+1, problems[keys[0]])
		}
		if err := h.enforceFirewall(ctx, entries); err != nil {
			h.fwErr = err.Error()
			return nil, err
		}
		next := firewallState{Revision: req.GetRevision(), Entries: entries}
		raw, _ := json.Marshal(next)
		if err := writeAtomic(h.firewallPath(), raw, 0o600); err != nil {
			h.fwErr = "the blocklist works but was not saved and will not survive a reboot: " + err.Error()
			return nil, errors.New(h.fwErr)
		}
		h.fw, h.fwErr = next, ""
	}
	return h.firewallStatusLocked(ctx, true), nil
}

func (h *Handler) firewallStatus(ctx context.Context) *agentv1.FirewallStatus {
	h.fwMu.Lock()
	defer h.fwMu.Unlock()
	return h.firewallStatusLocked(ctx, false)
}

// firewallStatusLocked reads the counters, and puts the rules back if something removed the table.
func (h *Handler) firewallStatusLocked(ctx context.Context, perEntry bool) *agentv1.FirewallStatus {
	st := &agentv1.FirewallStatus{Revision: h.fw.Revision, Entries: int32(len(h.fw.Entries)), Error: h.fwErr}
	if len(h.fw.Entries) == 0 {
		return st
	}
	list := func() ([]byte, error) { return h.exec.Run(ctx, "nft", "-j", "list", "table", "inet", firewallTable) }
	out, err := list()
	if err != nil {
		if e := h.enforceFirewall(ctx, h.fw.Entries); e != nil {
			h.fwErr, st.Error = e.Error(), e.Error()
			return st
		}
		h.log.Warn("blocklist table was missing, restored it")
		if out, err = list(); err != nil {
			return st
		}
	}
	total, per, err := parseNftCounters(out)
	if err != nil {
		return st
	}
	st.DroppedPackets, st.DroppedBytes = total.Packets, total.Bytes
	if perEntry {
		for _, e := range h.fw.Entries {
			c := per[e]
			st.Counters = append(st.Counters, &agentv1.FirewallCounter{Entry: e, Packets: c.Packets, Bytes: c.Bytes})
		}
	}
	return st
}

func (h *Handler) restoreFirewall(ctx context.Context) error {
	raw, err := os.ReadFile(h.firewallPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st firewallState
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	h.fwMu.Lock()
	defer h.fwMu.Unlock()
	h.fw = st
	if len(st.Entries) == 0 {
		return nil
	}
	if err := h.enforceFirewall(ctx, st.Entries); err != nil {
		h.fwErr = err.Error()
		return err
	}
	return nil
}
