# 0003. Deploy v1 on self-managed k3s on one VM with PostgreSQL in-cluster

- Status: accepted
- Date: 2026-10-16
- Deciders: project owner (interview answers A-058, A-059, A-063)

## Context and Problem Statement

The v1 definition of done includes a production deployment on Kubernetes
(A-058). The global guide says production does not depend on k3s or k3d, and
names managed Kubernetes as the usual target. Where does v1 run?

## Decision Drivers

- Cost: v1 serves a single launch venue at first; spend scales later.
- Simplicity: one operator, no CI in v1 (A-062), deploys by `helm upgrade`.
- The global guide's position that production does not depend on k3s.
- Data safety: PostgreSQL holds every piece of user and security state.

## Considered Options

1. Managed Kubernetes on one cloud with that cloud's managed PostgreSQL 18.
2. Self-managed k3s on one VM with PostgreSQL 18 in-cluster on a persistent volume.

## Decision Outcome

Chosen option: 2, k3s on one VM with PostgreSQL in-cluster (A-059, "Start simple
for now with number 2"). Ingress is ingress-nginx with cert-manager and Let's
Encrypt (A-063). This deviates from the global guide's rule that production
does not depend on k3s, which is why this ADR exists.

- The Helm chart under `deploy/chart` stays portable: nothing in it is
  k3s-specific beyond the storage class value, so a later move to managed
  Kubernetes is a values change plus a data migration.
- The API (`ROLE=api`) and worker (`ROLE=worker`) run as separate Deployments;
  migrations run as a Job before either starts.
- A cloud load balancer and a Cloudflare front are post-v1 work (A-063).

### Consequences

- Good: lowest running cost and the fewest moving parts for v1.
- Good: the same chart and images run on a managed cluster later.
- Bad: one VM is a single point of failure for the whole service.
- Bad: PostgreSQL backups, restores, upgrades and disk monitoring are ours;
  no managed point-in-time recovery.
- Bad: k3s upgrades and OS patching are ours.

### Support Obligation

The deploy owner documents backup and restore in `deploy/k3s/README.md`, runs a
restore drill before launch, and revisits this ADR before the user count or the
uptime target outgrows one VM. Moving to managed Kubernetes needs a new ADR
that supersedes this one.

## Pros and Cons of the Options

### 1. Managed Kubernetes and managed PostgreSQL

- Good: managed backups, failover and upgrades; matches the global guide.
- Bad: higher fixed cost before there is traffic to justify it; provider choice needed now.

### 2. k3s on one VM, PostgreSQL in-cluster

- Good: cheapest; one machine to reason about.
- Bad: single point of failure; database operations are on us.
