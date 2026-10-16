# Production runbook: k3s on one VM

Perspectiv v1 runs on self-managed k3s on one VM, with PostgreSQL 18 in the
cluster on a persistent volume, ingress-nginx in front, and a cert-manager
certificate from Let's Encrypt ([ADR 0003](../../docs/adr/0003-k3s-single-vm.md)).
One Helm chart, `deploy/chart`, installs all of the application side:

| Object | What it does |
|---|---|
| `Job/<release>-migrate-<revision>` | Runs `perspectiv migrate` once per install or upgrade, after Postgres answers |
| `Deployment/<release>-api` | `ROLE=api`; serves the SPA and `/api`; mounts the avatars volume |
| `Deployment/<release>-worker` | `ROLE=worker`; River workers and the rank job; answers `/healthz` and `/metrics` |
| `StatefulSet/<release>-postgres` | `postgres:18.6-alpine3.24` on a `local-path` volume, kept on uninstall |
| `CronJob/<release>-postgres-backup` | `pg_dump` custom-format archive daily at 03:15 UTC, newest 14 kept |
| `Ingress/<release>` | ingress-nginx, TLS from cert-manager, HTTP redirected to HTTPS |
| `Issuer/<release>-letsencrypt` | ACME HTTP-01 issuer for Let's Encrypt production |

The api and worker share one image. Each of their pods starts with an init
container that runs `kubectl wait` on this revision's migration Job, so neither
serves until the schema is current. The chart is not k3s-specific beyond the
`local-path` storage class.

Commands below run from the repository root on a workstation whose `KUBECONFIG`
points at the VM's cluster, unless a step says it runs on the VM.

## Versions

Checked against their registries on 2026-10-16; re-verify before a bump.

| Component | Version |
|---|---|
| k3s | `v1.36.5+k3s1` (stable channel) |
| ingress-nginx chart | `4.15.1` (controller 1.15.1) |
| cert-manager chart | `v1.21.2` |
| PostgreSQL image | `postgres:18.6-alpine3.24` |
| kubectl image (migration wait) | `registry.k8s.io/kubectl:v1.36.5` |
| Helm | 3.18 or later; chart `apiVersion: v2` also installs with Helm 4 |

The Kubernetes project retired ingress-nginx in March 2026, so the 4.15 line
receives no further fixes. v1 stays on it as the spec decided; moving to
another controller is a values change to `ingress.className` plus the HSTS and
redirect settings in `ingress-nginx-values.yaml`, and needs its own ADR.

## 1. Prepare the VM (on the VM)

Open TCP 80 and 443 to the internet and 6443 to your workstation only. Point
the DNS `A` record for the hostname at the VM before step 4; the HTTP-01
challenge needs it.

```sh
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.36.5+k3s1 sh -s - server --disable=traefik
sudo cat /etc/rancher/k3s/k3s.yaml   # copy to the workstation; replace 127.0.0.1 with the VM address
```

Traefik is disabled because ingress-nginx owns ports 80 and 443. k3s keeps its
ServiceLB, which binds the ingress-nginx `LoadBalancer` Service on the VM's
address, and its `local-path` provisioner, which backs every volume under
`/var/lib/rancher/k3s/storage` on the VM disk.

## 2. Ingress and certificates

```sh
helm upgrade --install ingress-nginx ingress-nginx \
  --repo https://kubernetes.github.io/ingress-nginx --version 4.15.1 \
  --namespace ingress-nginx --create-namespace \
  -f deploy/k3s/ingress-nginx-values.yaml --wait

helm upgrade --install cert-manager cert-manager \
  --repo https://charts.jetstack.io --version v1.21.2 \
  --namespace cert-manager --create-namespace --set crds.enabled=true --wait
```

`ingress-nginx-values.yaml` turns on HSTS for every HTTPS response
(`max-age=31536000; includeSubDomains`, no preload) and redirects HTTP to
HTTPS. The chart sets `COOKIE_SECURE=true`, so the refresh cookie is only sent
over TLS.

## 3. Image

Build on any machine with Docker; the build stamps the version that `/about`
reports.

```sh
VERSION="$(git describe --tags --always --dirty)"
docker build \
  --build-arg GO_VERSION="$(sed -n 's/^toolchain go//p' go.mod)" \
  --build-arg VERSION="$VERSION" \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t perspectiv:"$VERSION" .
```

The build fails if `GO_VERSION` differs from the `toolchain` line in `go.mod`.
Either push the image to a registry the VM can pull from and set
`image.repository` to it, or copy it straight into k3s's containerd:

```sh
docker save perspectiv:"$VERSION" | ssh <vm> sudo k3s ctr images import -
```

## 4. Install

Create the namespace and the Secret once. The chart never holds secret
values; it reads them from this Secret. The Postgres password is hex so the
connection URL needs no escaping.

```sh
kubectl create namespace perspectiv
kubectl -n perspectiv create secret generic perspectiv-secrets \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=POSTGRES_PASSWORD="$(openssl rand -hex 24)" \
  --from-literal=RESEND_API_KEY="<resend api key>"
```

Postgres sets its password from this Secret only when the data volume is first
initialised. Changing `POSTGRES_PASSWORD` later needs an `ALTER ROLE
perspectiv PASSWORD '...'` in the database first.

```sh
helm upgrade --install perspectiv deploy/chart --namespace perspectiv \
  --set image.repository=perspectiv --set image.tag="$VERSION" \
  --set host=perspectiv.example.com \
  --set secrets.existingSecret=perspectiv-secrets \
  --set tls.acme.email=ops@perspectiv.example.com \
  --set "app.mail.from=Perspectiv <no-reply@perspectiv.example.com>" \
  --wait --timeout 10m
```

The chart refuses to render without `image.repository`, `host`,
`secrets.existingSecret`, `app.mail.from` and, while it creates the Issuer,
`tls.acme.email`. For a first dry run against Let's Encrypt staging add
`--set tls.acme.server=https://acme-staging-v02.api.letsencrypt.org/directory`.

## 5. Verify

```sh
kubectl -n perspectiv get job,deploy,sts,cronjob,certificate
curl -fsSI https://perspectiv.example.com/healthz   # 200 with Strict-Transport-Security
curl -fsS  https://perspectiv.example.com/about     # version, commit, build date
curl -sI   http://perspectiv.example.com/           # 308 to https
kubectl -n perspectiv port-forward deploy/perspectiv-api 8080:8080 &
curl -fsS localhost:8080/metrics | head             # Prometheus text
```

`/metrics` answers inside the cluster only: at the public edge the Ingress
routes it to a Service with no endpoints, so it returns 503. The api and
worker pods carry `prometheus.io/scrape` annotations for an in-cluster
Prometheus.

## 6. Upgrade

Build and import a new image, then run the same `helm upgrade --install` with
the new tag. Each revision creates `Job/perspectiv-migrate-<revision>`; the new
pods wait for it while the old ones keep serving, and Helm removes the previous
revision's Job. A failed migration leaves the new pods in `Init` and
`helm upgrade --wait` fails; read it with
`kubectl -n perspectiv logs job/perspectiv-migrate-<revision>`, fix forward,
and upgrade again. `helm rollback` restores the old image but not the old
schema, which is why migrations are written to keep the previous release
working.

A PostgreSQL major upgrade is not a chart change: dump with the backup below,
install the new major on a fresh volume, restore.

## 7. Backups and restore

The `perspectiv-postgres-backup` CronJob runs every day at 03:15 UTC. It
writes `perspectiv-<UTC timestamp>.dump` (pg_dump custom format) to the
`perspectiv-postgres-backups` volume, verifies it with `pg_restore --list`,
and keeps the newest 14. A failed run leaves no archive and prunes nothing.
Change the schedule with `postgres.backup.schedule` and the retention with
`postgres.backup.keep`.

That volume sits on the same VM disk as the database, so it protects against a
bad migration or a deleted row, not against losing the VM. Copy archives off
the VM; on the VM they live under
`/var/lib/rancher/k3s/storage/*_perspectiv_perspectiv-postgres-backups/`:

```sh
# on a schedule from another machine
rsync -a <vm>:/var/lib/rancher/k3s/storage/'*_perspectiv_perspectiv-postgres-backups/' ./perspectiv-backups/
```

Take a backup now (before an upgrade, or to test):

```sh
kubectl -n perspectiv create job --from=cronjob/perspectiv-postgres-backup backup-$(date -u +%Y%m%d%H%M)
kubectl -n perspectiv logs -f job/backup-<stamp>
```

Or stream one straight to the workstation without the backup volume:

```sh
kubectl -n perspectiv exec statefulset/perspectiv-postgres -- \
  pg_dump -U perspectiv -d perspectiv --format=custom > perspectiv-$(date -u +%Y%m%dT%H%M%SZ).dump
```

Restore an archive (stop writers first so nothing races the restore):

```sh
kubectl -n perspectiv scale deploy/perspectiv-api deploy/perspectiv-worker --replicas=0
kubectl -n perspectiv exec -i statefulset/perspectiv-postgres -- \
  pg_restore -U perspectiv -d perspectiv --clean --if-exists --no-owner < perspectiv-<stamp>.dump
kubectl -n perspectiv scale deploy/perspectiv-api deploy/perspectiv-worker --replicas=1
```

Run this restore against a scratch namespace before launch and after any
change to the schema tooling; ADR 0003 makes the restore drill a launch
condition.

## 8. Uninstall

`helm uninstall perspectiv -n perspectiv` removes the workloads but keeps the
Postgres data volume, the backups volume and the avatars volume. Delete those
PersistentVolumeClaims by hand only when the data is meant to go.

## End-to-end check

`deploy/k3s/e2e.sh` runs this whole path on a throwaway k3d cluster: it
builds the image, installs ingress-nginx and cert-manager with the versions
above, installs the chart, and checks that the migration Job finished before
the api and worker started, that `/healthz` and `/about` answer through the
ingress hostname over TLS with HSTS, that `/metrics` answers in-cluster and
not at the edge, that a backup runs, and that an upgrade migrates again.
Let's Encrypt cannot reach a local cluster, so the certificate comes from a
throwaway cert-manager CA issuer. It needs k3d 5.9 or later. Set
`KEEP_CLUSTER=1` to leave the cluster up.
