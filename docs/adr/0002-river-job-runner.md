# 0002. Run background jobs on River in PostgreSQL

- Status: accepted
- Date: 2026-10-16
- Deciders: project owner (interview answers A-024, A-026, A-045, A-061)

## Context and Problem Statement

Perspectiv needs three kinds of background work: a scheduled job per published
perspective that marks it SOLO seven days after publish when no claim was
accepted (A-024), a periodic job that recomputes lore rank and canon for arcs
with new activity (A-026, A-045), and email delivery with retries that never
blocks the request that caused it (A-061). The global stack names no job
runner. What runs these jobs?

## Decision Drivers

- The expiry job must be inserted in the same transaction that publishes the
  perspective, so a crash can never leave a published perspective without its timer.
- PostgreSQL is already the system of record; the deployment (ADR 0003) is one
  VM with Postgres in-cluster, and a second stateful system is a second backup story.
- Jobs must run application Go code against the domain services.
- The binary layout is one `perspectiv` binary whose ROLE selects api, worker or all (A-047).

## Considered Options

1. River (`github.com/riverqueue/river`) with the pgx v5 driver.
2. pg_cron inside PostgreSQL.
3. A Redis-backed queue (asynq or similar).
4. A hand-written `SELECT ... FOR UPDATE SKIP LOCKED` poller.

## Decision Outcome

Chosen option: 1, River v0.49.0 with `riverdriver/riverpgxv5` v0.49.0, verified
on the Go module proxy on 2026-10-13 and again on 2026-10-16.

- Producers insert jobs with `InsertTx` inside their own write transaction.
- `ROLE=api` builds an insert-only client; `ROLE=worker` and `ROLE=all` register
  workers and the periodic rank job.
- River's own migrations run after the goose migrations in one migrate step.
- Every failed attempt increments the `job_failures_total` metric.

### Consequences

- Good: transactional enqueue; no job is lost or orphaned by a rolled-back write.
- Good: no new stateful service; backups of PostgreSQL cover the queue.
- Good: retries, scheduled jobs, periodic jobs and uniqueness come with the library.
- Bad: River is pre-1.0; a minor release can change its API. Upgrades are tested
  against the integration tier before they land.
- Bad: job throughput shares the PostgreSQL primary with request traffic.

### Support Obligation

The jobs package owner tracks River releases through `make updates`, reads the
changelog before any bump, and keeps River's migrations in step with the
library version. A move off River needs a new ADR.

## Pros and Cons of the Options

### 1. River

- Good: pgx-native, transactional inserts, periodic and scheduled jobs.
- Bad: pre-1.0 API.

### 2. pg_cron

- Good: no application dependency.
- Bad: runs SQL only; it cannot call the Go lore service or send email.

### 3. Redis-backed queue

- Good: mature libraries and high throughput.
- Bad: enqueue cannot join the PostgreSQL transaction; a second stateful service to run and back up.

### 4. Hand-written poller

- Good: no dependency.
- Bad: retries, backoff, scheduling and periodic jobs all become our code to get right.
