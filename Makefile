GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

# VERSION overrides internal/version.Version at link time (release builds pass
# the git tag). Empty keeps the version written in source.
VERSION ?=
LDFLAGS := -s -w $(if $(VERSION),-X tgwebproxy/internal/version.Version=$(patsubst v%,%,$(VERSION)),)

.PHONY: tools test lint fmt sqlc proto web run build agent-linux e2e e2e-telemt test-install test-node-preflight

tools:
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install mvdan.cc/gofumpt@v0.11.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

fmt:
	gofumpt -l -w ./cmd ./internal ./proto 2>/dev/null || gofumpt -l -w ./cmd ./internal

lint:
	golangci-lint run ./...

test:
	go test -p 1 ./...

sqlc:
	sqlc generate

proto:
	protoc -I proto --go_out=. --go_opt=module=tgwebproxy --go-grpc_out=. --go-grpc_opt=module=tgwebproxy proto/agent/v1/agent.proto

web:
	cd web && npm ci && npm run build

run:
	go run ./cmd/panel serve

# build produces the panel binary for this machine, with the SPA already built
# into web/dist (make web first).
build:
	CGO_ENABLED=0 go build -trimpath -ldflags='$(LDFLAGS)' -o panel ./cmd/panel

agent-linux:
	mkdir -p data/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='$(LDFLAGS)' -o data/agent/tgwp-agent-linux-amd64 ./cmd/agent
	shasum -a 256 data/agent/tgwp-agent-linux-amd64 | awk '{print $$1}' > data/agent/tgwp-agent-linux-amd64.sha256

# e2e brings up postgres+panel (with the override file exposing :8080),
# creates an admin, runs the fakenode container against a fresh install
# token, and asserts the whole flow against http://localhost:8080. Tears the
# stack down afterwards either way.
e2e:
	./deploy/run-e2e.sh

# e2e-telemt is the same flow for the telemt engine: the fakenode-telemt container runs
# the real telemt binary from the pinned release and the agent drives it over telemt's
# control API. The first run downloads telemt inside the image build.
e2e-telemt:
	./deploy/run-e2e-telemt.sh

# test-install runs install.sh end to end inside a docker:27-dind container, against the
# panel image built from this checkout: a pre-flight that must refuse a domain that does
# not resolve, fresh install in --local mode, health and login checks, an --update pass,
# --uninstall --purge, then a --skip-preflight run in domain mode. Also runs shellcheck.
test-install:
	./deploy/test-install.sh

# test-node-preflight runs the rendered node install script's pre-flight inside ubuntu:24.04
# (no network, no tty, a stub panel on loopback): a hostname that does not resolve must stop
# the script before apt-get with the "nothing was installed" message; TGWP_SKIP_PREFLIGHT=1
# and TGWP_DRY_RUN=1 must get past it; a resolving hostname and the [r]/[c] menu must pass.
test-node-preflight:
	./deploy/test-node-preflight.sh
