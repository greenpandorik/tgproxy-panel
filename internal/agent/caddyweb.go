package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
)

const DefaultCaddyfilePath = "/etc/caddy/Caddyfile"

var installerCaddyfile = regexp.MustCompile(`\A\{\n\temail ([^\s]+)\n\tservers \{\n\t\ttrusted_proxies static private_ranges\n\t\}\n\}\n\n([A-Za-z0-9.-]+) \{\n\tencode zstd gzip\n\treverse_proxy 127\.0\.0\.1:18080 \{\n\t\theader_up X-Forwarded-For \{remote_host\}\n\t\}\n\}\n\z`)

func nodeCaddyfile(email, host string) string {
	return fmt.Sprintf("{\n\temail %s\n\tservers {\n\t\ttrusted_proxies static private_ranges\n\t\tprotocols h1 h2\n\t}\n}\n\n"+
		"%s {\n\treverse_proxy 127.0.0.1:18080 {\n\t\theader_up X-Forwarded-For {remote_host}\n\t\tflush_interval -1\n\t\tstream_close_delay 5m\n\t}\n}\n", email, host)
}

// tunedCaddyfile returns the current node Caddyfile for one an older installer wrote, and false for anything else.
func tunedCaddyfile(cur string) (string, bool) {
	m := installerCaddyfile.FindStringSubmatch(cur)
	if m == nil {
		return "", false
	}
	return nodeCaddyfile(m[1], m[2]), true
}

// ensureCaddyWebTuning moves a node's untouched installer Caddyfile to the WEB settings telemt documents.
func (h *Handler) ensureCaddyWebTuning(ctx context.Context, path string) error {
	if h.cfg.Engine != EngineTelemt {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, ok := tunedCaddyfile(string(old))
	if !ok {
		return nil
	}
	candidate := path + ".tgwp-new"
	if err := writeAtomic(candidate, []byte(next), info.Mode().Perm()); err != nil {
		return err
	}
	defer func() { _ = os.Remove(candidate) }()
	if out, err := h.exec.Run(ctx, "caddy", "validate", "--config", candidate, "--adapter", "caddyfile"); err != nil {
		return fmt.Errorf("caddy validate: %v: %s", err, out)
	}
	if err := writeAtomicKeepOwner(path, []byte(next), info.Mode().Perm()); err != nil {
		return err
	}
	if out, err := h.exec.Run(ctx, "systemctl", "reload", "caddy"); err != nil {
		_ = writeAtomicKeepOwner(path, old, info.Mode().Perm())
		_, _ = h.exec.Run(ctx, "systemctl", "reload", "caddy")
		return fmt.Errorf("caddy reload: %v: %s", err, out)
	}
	return nil
}
