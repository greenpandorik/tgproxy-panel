// api-spec exports the management API contract used by the public documentation.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"tgwebproxy/internal/api"
)

func main() {
	check := flag.Bool("check", false, "fail when the committed API contract is stale")
	output := flag.String("output", "docs/api/openapi.json", "output contract path")
	flag.Parse()
	if err := run(*output, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(output string, check bool) error {
	data, err := json.MarshalIndent(api.ManagementOpenAPI(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if check {
		current, err := os.ReadFile(output)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, current) {
			return fmt.Errorf("%s is stale; run go run ./cmd/api-spec", output)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}
