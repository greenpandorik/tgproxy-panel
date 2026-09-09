package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const mtproxyDropin = `[Service]
ExecStart=
ExecStart=/opt/MTProxy/objs/bin/mtproto-proxy -u mtproxy -p 8888 -H 2398 $MTPROXY_SECRETS --aes-pwd /etc/mtproxy/proxy-secret /etc/mtproxy/proxy-multi.conf -M ${MTPROXY_WORKERS} -C ${MTPROXY_MAX_CONNECTIONS}
`

func InitNode(configPath, envPath, dropinPath string, maxProfiles int) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	limits, _ := cfg["limits"].(map[string]any)
	if limits == nil {
		limits = map[string]any{}
	}
	limits["max_profiles"] = maxProfiles
	cfg["limits"] = limits
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(configPath, append(out, '\n'), 0o640); err != nil {
		return err
	}

	env, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	secret := ""
	for _, line := range strings.Split(string(env), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "MTPROXY_SECRET="); ok {
			secret = v
		}
	}
	if err := writeAtomic(envPath, RenderMTProxyEnv(env, []string{secret}), 0o640); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dropinPath), 0o755); err != nil {
		return err
	}
	return writeAtomic(dropinPath, []byte(mtproxyDropin), 0o644)
}
