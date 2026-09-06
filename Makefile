GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

# VERSION overrides internal/version.Version at link time (release builds pass
# the git tag). Empty keeps the version written in source.
VERSION ?=
LDFLAGS := -s -w $(if $(VERSION),-X tgwebproxy/internal/version.Version=$(patsubst v%,%,$(VERSION)),)

.PHONY: tools test lint fmt sqlc proto web run build agent-linux e2e e2e-telemt

tools:
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
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
