package agent

import (
	"context"
	"os"
	"path/filepath"
)

// telemt 3.5.9 removes its conntrack chain at every start and fails on iptables-nft, which reports
// the missing chain in words telemt does not recognise (telemt#932); an empty chain lets it pass.
const telemtConntrackDropin = "[Service]\n" +
	"ExecStartPre=-+/bin/sh -c 'command -v iptables >/dev/null 2>&1 && iptables -w -t raw -N TELEMT_NOTRACK 2>/dev/null; " +
	"command -v ip6tables >/dev/null 2>&1 && ip6tables -w -t raw -N TELEMT_NOTRACK 2>/dev/null; exit 0'\n"

func telemtConntrackDropinPath(unitPath string) string {
	return filepath.Join(unitPath+".d", "10-tgwp-conntrack.conf")
}

// writeTelemtConntrackDropin reports whether it had to write the drop-in.
func writeTelemtConntrackDropin(unitPath string) (bool, error) {
	path := telemtConntrackDropinPath(unitPath)
	if b, err := os.ReadFile(path); err == nil && string(b) == telemtConntrackDropin {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, writeAtomic(path, []byte(telemtConntrackDropin), 0o644)
}

// ensureTelemtConntrackFix gives a node installed before the drop-in existed the same start, and
// lets the telemt already running finish its cleanup on its next retry.
func (h *Handler) ensureTelemtConntrackFix(ctx context.Context, unitPath string) error {
	if h.cfg.Engine != EngineTelemt {
		return nil
	}
	if _, err := os.Stat(unitPath); err != nil {
		return nil
	}
	changed, err := writeTelemtConntrackDropin(unitPath)
	if err != nil || !changed {
		return err
	}
	if _, err := h.exec.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	for _, bin := range []string{"iptables", "ip6tables"} {
		_, _ = h.exec.Run(ctx, bin, "-w", "-t", "raw", "-N", "TELEMT_NOTRACK")
	}
	return nil
}
