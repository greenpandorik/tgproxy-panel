# Contributing

**English** · [Русский](CONTRIBUTING.ru.md)

This page explains how to build the panel on your computer, run the checks and send a change.
Ask questions and share ideas in [Discussions](https://github.com/greenpandorik/tgproxy-panel/discussions).
Report bugs in Issues. Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md).

## What you need

- Go 1.26
- Node.js 22
- PostgreSQL 16
- Docker, only for the end-to-end tests
- `make` and `openssl`

## Build and run

Create a database user and two databases, one for the panel and one for the tests:

```bash
createuser -s tgwp && psql -d postgres -c "ALTER USER tgwp PASSWORD 'tgwp'"
createdb -O tgwp tgwp && createdb -O tgwp tgwp_test
```

Then build and start the panel:

```bash
cp .env.example .env     # fill in MASTER_KEY and SESSION_SECRET: openssl rand -base64 32
make web                 # builds the UI into web/dist
set -a; . ./.env; set +a # the panel reads settings only from the environment
go run ./cmd/panel admin create root 'change-me-now-1'
make run                 # panel on http://localhost:8080
```

Run `make web` before any Go build or test: the Go binary embeds `web/dist`, and a fresh clone
does not have it.

With the default `NODE_DRIVER=gateway`, the panel also needs `METRICS_TOKEN`
(`openssl rand -hex 32`). For UI work you can set `NODE_DRIVER=mock` instead: the panel then
uses an in-memory stand-in for servers and allows an empty `METRICS_TOKEN`.

For the UI with hot reload, run `cd web && npm run dev`. The dev server forwards `/api`,
`/healthz` and `/metrics` to the panel on `:8080`.

Other ways to run the panel, with Docker or on a real domain, are in the
[setup guide](docs/setup.en.md).

## Run the checks

CI runs these commands on every pull request. Run them before you push. Install the Go tools
once with `make tools`: it puts sqlc, gofumpt, golangci-lint and the protoc plugins into
`$(go env GOPATH)/bin`.

Go:

```bash
export TEST_DATABASE_URL='postgres://tgwp:tgwp@localhost:5432/tgwp_test?sslmode=disable'
go test -p 1 -race ./...
gofumpt -l ./cmd ./internal ./deploy     # prints nothing when the formatting is right
golangci-lint run ./...
```

Without `TEST_DATABASE_URL` the database tests are silently skipped, so set it. `-p 1` is
required: all packages share one test database, and each test empties its tables.
`make test` and `make lint` are shortcuts, but `make test` runs without `-race`.

Web:

```bash
cd web
npm ci
npm run typecheck
npm run lint
npm run test -- --run
npm run i18n:check
npm run build
```

## End-to-end tests

These need Docker and take a few minutes. Each one cleans up after itself.

| Command | What it checks |
|---|---|
| `make e2e` | The whole flow on the tproxy engine: panel, Postgres and a test server running the real `tproxy-server` in containers. Needs port 8080 free. |
| `make e2e-telemt` | The same flow on the telemt engine, with the real telemt release. Needs internet access to Telegram's data centres. On an ARM computer the container runs under emulation and is slower. |
| `make test-install` | `install.sh` from start to finish inside a `docker:27-dind` container, plus shellcheck. |
| `make test-node-preflight` | The pre-flight of the server install script, without network access. |
| `make test-agent-upgrade` | `tgwp-agent upgrade` against a stub panel, including a download with a wrong checksum. |

## Where things live

- SQL queries are in `internal/store/queries`. After changing them, run `make sqlc`; it
  regenerates `internal/store/db`, which you don't edit by hand.
- Migrations are goose files in `internal/store/migrations`. The panel applies them on start.
- The agent protocol is `proto/agent/v1/agent.proto`. After changing it, run `make proto`
  (needs `protoc`).
- UI strings are in `web/src/i18n/ru.json` and `en.json`. Add every new string to both files;
  `npm run i18n:check` fails otherwise.
- `internal/api/rbac_walk_test.go` walks every route and checks that it requires a session, a
  CSRF token and the right role. A route that is public on purpose, a change a viewer may make,
  or a route the agent calls with its own token goes into the matching list: `publicReads`,
  `publicMutations`, `allowedForViewer` or `nodeTokenReads`.
- Logs use `slog`. Never log secrets, tokens or passwords.

## Sending a change

1. Fork the repository and create a branch from `main`.
2. Make the change and run the checks above.
3. Open a pull request against `main`. Say what you changed and how you tested it. For a UI
   change, add a screenshot.

CI runs three jobs: Go (build, formatting, tests with `-race` on PostgreSQL 16, lint), Web
(types, lint, tests, translations, build) and the Docker image build. The project has one
maintainer, who reads every pull request. A change is merged once CI passes and the maintainer
approves it. Write commit messages in plain words that say what changed.

By sending a change you agree that it is published under [AGPL-3.0](LICENSE), the project's
license.

## Releases

Releases are made by the maintainer:

1. Set the version in `internal/version/version.go`.
2. Push the tag `vX.Y.Z`. The release workflow checks that the tag matches the version in the
   source, builds the panel and agent binaries for linux/amd64 and linux/arm64 with a
   `SHA256SUMS` file, publishes the `ghcr.io` image and attaches the binaries to the GitHub
   release.
