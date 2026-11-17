# Perspectiv

A storytelling forum anchored to a place and a date: everyone who was at the
same place on the same day writes their side, and the stories link up.

![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/Go-1.27-00ADD8.svg)
![TypeScript](https://img.shields.io/badge/TypeScript-7-3178C6.svg)

## What it does

You pick a place (a state, a city, a mall, a single store) and a day, and you
write what happened from where you stood. Other writers who were there add
their own perspective to the same day. When a writer recognises themselves as
a character in someone else's story ("the guy in the yellow raincoat"), they
claim it, and once the author accepts, the two stories join into a lore. The
lore the day's writers favour most becomes that day's canon.

The product is a full-stack Go and React app. The part worth reading is the
platform work around it: a Helm chart that runs migrations before any pod
serves, a nightly verified backup, and a script that proves the whole
production path on a throwaway k3d cluster.

## Key features

- A Helm chart that migrates first. Each install or upgrade runs a migration
  Job, and the api and worker pods wait on it in an init container, so no pod
  serves against an old schema.
- Nightly backups that check themselves. A CronJob writes a `pg_dump` archive,
  verifies it with `pg_restore --list` and keeps the newest 14. The backup
  script has its own integration test against PostgreSQL 18.
- An end-to-end deploy test on k3d. `deploy/k3s/e2e.sh` builds the image,
  installs ingress-nginx, cert-manager and the chart, then checks migration
  ordering, TLS with HSTS, that `/metrics` is not reachable at the edge, a
  backup run and a Helm upgrade.
- Two-factor sign-in with RFC 6238 TOTP, written against the RFC's own test
  vectors. It has replay protection and ten single-use recovery codes, and the
  secrets are encrypted at rest with `age` under rotatable keys.
- Security state kept in PostgreSQL. Lockouts, rate limits and session
  revocation survive restarts and are shared across replicas. Refresh tokens
  live in `HttpOnly; Secure` cookies, and responses carry `nosniff` and a
  strict referrer policy.
- Background jobs in the same database. River runs the story expiry timers,
  the periodic canon ranking and email delivery, and each job is enqueued in
  the same transaction as the write that caused it.
- Tests against real backends. Integration tests run against PostgreSQL 18 in
  testcontainers, and `make check` enforces per-package coverage floors.

## Quick start

You need Go 1.27, Node 24 with pnpm, and Docker.

```sh
git clone https://github.com/gshepptech/perspectiv.git
cd perspectiv
make dev-up
```

`make dev-up` starts PostgreSQL 18 and Mailpit in Docker, applies migrations,
runs the server under Air and the web app under Vite, all in the background.
Open <http://localhost:5173>. Sign-up email lands in Mailpit at
<http://localhost:8025>.

To have places and stories to read, load the sample place data and the seed
stories:

```sh
set -a; . ./.env.dev; [ -f .env.dev.local ] && . ./.env.dev.local; set +a
go run ./cmd/placectl import \
  --divisions web/e2e/fixtures/divisions.ndjson \
  --places web/e2e/fixtures/places.ndjson
make seed
```

`make dev-status` shows what is running, `make dev-logs` tails the logs and
`make dev-down` stops everything (the database volume is kept).

## How it works

```text
                    ingress-nginx (TLS, HSTS, HTTP -> HTTPS)
                                 |
                 +---------------+----------------+
                 |                                |
        Deployment: api                  Deployment: worker
        ROLE=api                         ROLE=worker
        SPA + Connect-RPC on /api        River jobs, canon ranking
                 |                                |
                 +------------+-------------------+
                              |
            init container waits on Job: migrate-<revision>
                              |
                 StatefulSet: PostgreSQL 18  <-- CronJob: nightly pg_dump
```

- `cmd/perspectiv` is one binary with three roles: it runs as `api`,
  `worker` or `all` depending on `ROLE`. The api serves the React app
  (embedded in the binary) and the Connect-RPC API; the worker runs River
  jobs. In development one process does both.
- Each domain under `internal/` (`auth`, `place`, `arc`, `perspective`,
  `lore`, `vote`, `react`, `notify`, `moderation`, `user`) splits into
  handler, service and store. Queries are written in SQL and compiled with
  sqlc; the API is defined in `proto/` and generated with buf for both Go and
  TypeScript.
- Every RPC passes through recovery, request id and tracing, structured
  logging, authentication, authorization from a static route-to-role map,
  rate limiting and validation before its handler.
- Logs are zerolog JSON, traces go out over OpenTelemetry OTLP, and
  Prometheus scrapes `/metrics`. `make dev-grafana` starts Grafana,
  Prometheus, Tempo and Loki locally.
- `cmd/placectl` imports open US place data into PostgreSQL and folds
  duplicate stores and malls. Search runs on trigram and prefix indexes;
  nothing calls an outside map service.

Design decisions are recorded as ADRs in [`docs/adr`](docs/adr). The product
rules are in [`docs/product-model.md`](docs/product-model.md) and the visual
design system in [`docs/design-system.md`](docs/design-system.md), with a
page per surface under [`docs/design`](docs/design).

## Deploying

The chart lives in [`deploy/chart`](deploy/chart) and the production runbook
in [`deploy/k3s/README.md`](deploy/k3s/README.md): VM setup, ingress and
certificates, image build, secrets, install, upgrade, backups and restore,
and resetting a member's two-factor sign-in. The chart refuses to render
without its required values (image, host, secret name, mail sender, ACME
email), so a half-configured install fails before it starts.

To run the full deploy path locally (needs Docker, k3d 5.9+, Helm and
kubectl):

```sh
deploy/k3s/e2e.sh                 # create a cluster, check, delete it
KEEP_CLUSTER=1 deploy/k3s/e2e.sh  # leave the cluster up afterwards
```

## Configuration

The server reads its settings from the environment. Development defaults are
in [`.env.dev`](.env.dev), which holds no secrets; the first `make dev-up`
copies it to the gitignored `.env.dev.local`, which is the file to edit. The
main settings:

| Variable | Purpose |
|---|---|
| `ROLE` | `api`, `worker` or `all` |
| `DATABASE_URL` | PostgreSQL connection string |
| `PUBLIC_BASE_URL` | Base URL used in email links |
| `JWT_SECRET` | Signing key for access tokens |
| `ENCRYPTION_KEYS` | `age` keys for two-factor secrets, newest first (`perspectiv keygen` makes one) |
| `MAIL_PROVIDER` | `smtp` (Mailpit in development) or `resend` |
| `SIGNUP_MODE` | `open`, or `invite` to require an invite link or beta code |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Trace collector; empty turns tracing off |

In the cluster, the chart builds the non-secret settings from `values.yaml`
and reads the secrets from an existing Kubernetes Secret.

## Development

```sh
make help              # list every target
make check             # lint, vet, fmt, unit tests, integration tests, coverage floors
make test              # unit tier: Go (race, no Docker) and web tests
make test-integration  # integration tier: PostgreSQL 18 through testcontainers
make e2e               # Playwright against the running dev loop
make generate          # regenerate sqlc and buf code
make new-migration NAME=add_something
```

`make tools` installs the pinned Go tools (golangci-lint, gotestsum, Air,
goose, sqlc, buf) into `bin/tools/`; the targets above also install a missing
tool on first use. `shellcheck` and `pnpm` come from your package manager.

## License

[MIT](LICENSE)
