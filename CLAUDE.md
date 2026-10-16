# Perspectiv

A place- and date-anchored collaborative storytelling forum: people who were at
the same place on the same day each write a perspective, overlapping
perspectives form lore, and lore competes for canon. `CONTEXT.md` holds the
founder's notes. This file adds to the global `~/.claude/CLAUDE.md` and wins
where the two disagree.

## Project declarations

- **Mode: pre-production.** No v1.0 has been stamped. Renames, restructures and
  hard-cutover migrations are allowed; every goose migration MUST still have a
  working `Down`.
- **Tenancy: single-tenant public app.** There is no `org_id` and the global
  tenant rules do not apply. A table, query or interceptor MUST NOT carry a
  tenant key or tenant predicate.
- **Air-gap: not air-gapped.** The product is online-only. Runtime calls to a
  third party MUST be limited to the email provider; place data is read from
  the local database only.
- **Audience: adults only (18+).** Signup MUST require the 18-or-over confirmation.
- **Regulated builds: none.** No FIPS or IL build target exists.
- **CI: none in v1.** `make check` is the gate and MUST pass locally before a commit lands.

## Design system

Design system file: `docs/design-system.md`

The design pass writes that file, and its level-one heading names the system.
Component code under `web/src/components` or `web/src/routes` MUST NOT be
written before the file exists; `make lint` runs `scripts/designcheck`, which
fails when it does. Where that file and component code disagree, the file wins.
WCAG 2.1 AA contrast and full keyboard operation apply to every reading and
writing surface.

## Module layout

- Go module path `perspectiv`; binaries `cmd/perspectiv` (ROLE `api`, `worker`
  or `all`) and `cmd/placectl`. A third binary MUST NOT be added without an ADR.
- Domains under `internal/`: `auth`, `user`, `place`, `arc`, `perspective`,
  `lore`, `vote`, `react`, `notify`, `moderation`. Shared: `internal/jobs`,
  `internal/api` (with `apiutil`, `metrics`, `logging`, `telemetry`,
  `buildinfo`), `internal/db`, `internal/testdb`, `pkg/crypto`.
- A new top-level package under `internal/` or `pkg/` MUST NOT be added
  without amending this list. A `util`, `utils`, `helper` or `helpers` package
  MUST NOT exist. A domain package MUST NOT import more than three sibling
  domains. `make lint` runs `scripts/layoutcheck`, which enforces all three.
- The place ETL lives in `internal/place/importer`.
- Only the lore service, from a River periodic job, writes canon state.
- Lockout and rate-limit counters live in PostgreSQL, never in process memory.

## Verified pins

Checked against the Go module proxy, npm and Docker Hub on 2026-10-16. These
replace the global snapshot where they differ; `go.mod` is the authority for
Go modules and `web/package.json` for npm packages.

| Component | Pin | Note |
|---|---|---|
| Go toolchain | 1.27.2 (`go 1.27`, `toolchain go1.27.2`) | a newer minor than the global 1.26 snapshot |
| PostgreSQL | 18 (`postgres:18-alpine`, dev and tests) | above the global "16+" floor |
| pgx | v5.11.0 | |
| goose | v3.29.0 | |
| sqlc | v1.31.1 | |
| Connect-RPC (Go) | v1.21.1 | |
| chi | v5.5.0 | |
| zerolog | v1.35.1 | |
| OpenTelemetry Go | v1.47.0 | |
| otelpgx | v0.13.0 | |
| prometheus/client_golang | v1.25.0 | |
| golang-jwt/jwt | v5.3.1 | |
| age | v1.3.2 | |
| River (+ riverpgxv5) | v0.49.0 | ADR 0002 |
| testcontainers-go (+ postgres) | v0.44.0 | `wait.ForSQL` takes `network.Port` since v0.43 |
| pgtestdb | v0.1.1 | |
| resend-go | v3.17.0 | |
| golangci-lint | v2.14.0 | |
| gotestsum | v1.13.0 | |
| buf | v1.74.0 | |
| Air | v1.67.4 | |
| Node | 24 LTS (24.22.0) | |
| Vite / TypeScript / vitest | 8 / 7 / 5 | ADR 0001 |
| react-router | 8 Data Mode | ADR 0001 |
| lucide-react | 1.x | ADR 0001 |
| Mailpit (dev) | `axllent/mailpit:v1.31.6` | |
| Prometheus / Tempo / Loki / Grafana (dev) | v3.15.0 / 2.7.2 / 3.6.18 / 11.6.16 | `make dev-grafana` |

A version MUST be re-verified against its registry before it is bumped.

## Dev loop

`make tools` installs the pinned Go tools into `bin/tools/` once per machine;
the targets below also install a missing tool on first use. `shellcheck` and
`pnpm` come from the system package manager.

| Command | What it does |
|---|---|
| `make dev-up` | Postgres 18 and Mailpit containers, migrations, server under Air, web under Vite, all in the background |
| `make dev-down` | Stops all of it; the `perspectiv-pgdata` volume is kept |
| `make dev-status` | Containers, processes and `/healthz`; non-zero when anything is down |
| `make dev-logs` | Tails `tmp/dev/logs/server.log` and `web.log` |
| `make dev-migrate` / `make dev-migrate-down` | Apply pending migrations / roll back the last goose migration |
| `make dev-grafana` / `make dev-grafana-down` | Grafana, Prometheus, Tempo and Loki |
| `make e2e` | Playwright against the running loop |

Settings come from `.env.dev` (tracked, no secrets) and then `.env.dev.local`
(gitignored, created from `.env.dev` on the first run). Local overrides MUST go
in `.env.dev.local`, never in `.env.dev`. Defaults: server `:8080`, web
`:5173`, Postgres `127.0.0.1:5432`, Mailpit SMTP `localhost:1025` and API
`http://localhost:8025` (`MAILPIT_URL`). Development email goes through the
SMTP mailer to Mailpit, and the E2E suite reads verification links from
Mailpit's API. When a port is taken, `dev.sh` names the variable to change.

## Gates

`make check` runs, in order and stopping at the first failure: `lint`
(golangci-lint, layoutcheck, designcheck, shellcheck, web lint), `vet`, `fmt`
(gofmt and goimports, verify only; `make fmt-fix` rewrites), `test` (unit
tier, `-race -count=1`, no Docker), `test-integration` (`-tags integration`,
real `postgres:18-alpine` through testcontainers and pgtestdb) and
`coverage-floors`.

Coverage floors are 80 percent of statements on `internal/auth`,
`internal/lore`, `internal/vote` and `internal/moderation`. `scripts/coverfloor`
measures them from Go's own cover profiles: the unit and integration profiles
merge block by block, each floor covers the package directory itself and not
its sub-packages, and a floored package with no coverage data fails. A floor
MUST NOT be lowered to make a change fit.

Integration test files MUST start with `//go:build integration`.

## Decisions

ADRs live in `docs/adr/` in MADR format.

- [0001](docs/adr/0001-pin-forward-frontend-majors.md) Pin the frontend forward to Vite 8, TypeScript 7, vitest 5, react-router 8 and lucide-react 1.
- [0002](docs/adr/0002-river-job-runner.md) Run background jobs on River in PostgreSQL.
- [0003](docs/adr/0003-k3s-single-vm.md) Deploy v1 on self-managed k3s on one VM with PostgreSQL in-cluster.
