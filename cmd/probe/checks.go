package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"tgwebproxy/internal/protocolprobe"
	"tgwebproxy/internal/reliability"
)

func authenticatedChecks(ctx context.Context, host, configPath, runner string) (protocolprobe.Result, error) {
	result := protocolprobe.Result{FakeTLS: reliability.ProbeCheck{Status: "not_run"}, WEB: reliability.ProbeCheck{Status: "not_run"}}
	if configPath != "" && runner != "" {
		return result, errors.New("--protocol-config and --client-check are mutually exclusive")
	}
	if configPath != "" {
		config, e := protocolprobe.LoadConfig(configPath)
		if e != nil {
			return result, e
		}
		return protocolprobe.Check(ctx, host, config), nil
	}
	if runner == "" {
		return result, nil
	}
	if !filepath.IsAbs(runner) {
		return result, errors.New("--client-check must be an absolute executable path")
	}
	cctx, stop := context.WithTimeout(ctx, 45*time.Second)
	defer stop()
	cmd := exec.CommandContext(cctx, runner)
	cmd.Stdin = bytes.NewBufferString(host + "\n")
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if e := cmd.Run(); e != nil {
		result.FakeTLS.Status = "failed"
		result.WEB.Status = "failed"
		return result, nil
	}
	if e := json.Unmarshal(output.Bytes(), &result); e != nil {
		return result, errors.New("client checker returned invalid JSON")
	}
	return result, nil
}
