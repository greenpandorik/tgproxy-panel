# Contributing

## Running the checks

Go 1.26, Node 22 and PostgreSQL 16 are needed locally. Create the `tgwp` and `tgwp_test` databases and copy `.env.example` to `.env`.

```bash
make tools                                   # gofumpt, golangci-lint, sqlc, protoc plugins into $(go env GOPATH)/bin
TEST_DATABASE_URL='postgres://tgwp:tgwp@localhost:5432/tgwp_test?sslmode=disable' go test -p 1 -race ./...
gofumpt -l ./cmd ./internal ./deploy         # prints nothing when formatted
golangci-lint run ./...
cd web && npm ci && npm run typecheck && npm run lint && npm run test -- --run && npm run i18n:check && npm run build
```

`-p 1` is required: the Go packages share one test database.

The end-to-end smoke tests need Docker and take a few minutes:

```bash
make e2e          # tproxy engine, real tproxy-server in a container
make e2e-telemt   # telemt engine, real telemt release binary (needs egress to Telegram DCs)
```

## Conventions

- Backend: `sqlc` for queries (`make sqlc` after editing `internal/store/queries`), `goose` migrations embedded under `internal/store/migrations`, `slog` for logs. Secrets are never logged.
- Every HTTP route is covered by the RBAC walk test (`internal/api/rbac_walk_test.go`); a new route needs an entry there.
- Frontend strings live in `web/src/i18n/{ru,en}.json`; `npm run i18n:check` fails on missing keys in either language.
- Node engines (`tproxy`, `telemt`) are switched inside the agent and the install script renderer; the panel API stays engine-agnostic except where the README says otherwise.
- Commit messages describe the change in plain words. Pull requests should say what was tested and how.

## Releasing

1. Set the version in `internal/version/version.go`.
2. Tag `vX.Y.Z` and push the tag. The release workflow checks that the tag matches the source version, builds the binaries and the `ghcr.io` image, and attaches them to the GitHub release.
