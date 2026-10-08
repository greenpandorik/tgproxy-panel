// Package protocolprobe checks an authenticated proxy path using an unauthenticated
// MTProto key-exchange request. It never logs credentials or needs a Telegram account.
package protocolprobe

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"tgwebproxy/internal/reliability"
)

type Transport struct {
	Secret   string `json:"secret"`
	SNI      string `json:"sni,omitempty"`
	Port     int    `json:"port,omitempty"`
	BasePath string `json:"base_path,omitempty"`
}
type Config struct {
	FakeTLS        *Transport `json:"faketls,omitempty"`
	WEB            *Transport `json:"web,omitempty"`
	TimeoutSeconds int        `json:"timeout_seconds,omitempty"`
	DC             int        `json:"dc,omitempty"`
}
type Result struct {
	FakeTLS reliability.ProbeCheck `json:"faketls"`
	WEB     reliability.ProbeCheck `json:"web"`
}

var (
	errConfig       = errors.New("invalid private probe configuration")
	basePathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(/[A-Za-z0-9][A-Za-z0-9_-]*)*$`)
)

func LoadConfig(path string) (Config, error) {
	var c Config
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o077 != 0 {
		return c, errConfig
	}
	f, e := os.Open(path)
	if e != nil {
		return c, errConfig
	}
	defer func() { _ = f.Close() }()
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) || after.Mode().Perm()&0o077 != 0 || after.Size() > 16384 {
		return c, errConfig
	}
	decoder := json.NewDecoder(io.LimitReader(f, 16385))
	decoder.DisallowUnknownFields()
	var parsed *Config
	if decoder.Decode(&parsed) != nil || parsed == nil {
		return Config{}, errConfig
	}
	c = *parsed
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Config{}, errConfig
	}
	if c.validate() != nil {
		return Config{}, errConfig
	}
	return c, nil
}

func (c Config) validate() error {
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 30 || c.DC < 0 || c.DC > 5 {
		return errConfig
	}
	for _, v := range []struct {
		t    *Transport
		fake bool
	}{{c.FakeTLS, true}, {c.WEB, false}} {
		if v.t == nil {
			continue
		}
		t := v.t
		secret, e := hex.DecodeString(t.Secret)
		if e != nil {
			return errConfig
		}
		if v.fake {
			if len(secret) != 16 || !validHost(t.SNI) || t.BasePath != "" {
				return errConfig
			}
		} else {
			if len(secret) != 16 && (len(secret) != 17 || secret[0] != 0xdd) {
				return errConfig
			}
			if t.SNI != "" || len(t.BasePath) > 128 || (t.BasePath != "" && !basePathPattern.MatchString(t.BasePath)) {
				return errConfig
			}
		}
		if t.Port < 0 || t.Port > 65535 {
			return errConfig
		}
	}
	return nil
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || host != strings.ToLower(host) || strings.ContainsAny(host, "/:?#@ \t\r\n") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func Check(ctx context.Context, host string, c Config) Result {
	result := Result{FakeTLS: reliability.ProbeCheck{Status: "not_run"}, WEB: reliability.ProbeCheck{Status: "not_run"}}
	run := func(t *Transport, fake bool) reliability.ProbeCheck {
		if t == nil {
			return reliability.ProbeCheck{Status: "not_run"}
		}
		start := time.Now()
		check := reliability.ProbeCheck{Status: "failed"}

		if validHost(host) && c.validate() == nil {
			timeout := c.TimeoutSeconds
			if timeout == 0 {
				timeout = 15
			}
			dc := c.DC
			if dc == 0 {
				dc = 2
			}
			child, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
			defer cancel()
			if probe(child, host, *t, dc, fake) == nil {
				check.Status = "ok"
			}
		}
		check.LatencyMS = time.Since(start).Milliseconds()
		return check
	}
	result.FakeTLS = run(c.FakeTLS, true)
	result.WEB = run(c.WEB, false)
	return result
}
