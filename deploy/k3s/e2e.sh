#!/usr/bin/env bash
# End-to-end check of the production deploy path on a throwaway k3s cluster
# (k3d): build the image, install ingress-nginx and cert-manager the way the
# runbook does, helm install the chart, and prove that the migration Job ran
# before the api and worker, that /healthz and /about answer through the
# ingress hostname over TLS with HSTS, that /metrics answers in-cluster but not
# at the edge, that a backup runs, and that a helm upgrade migrates again.
#
# Let's Encrypt cannot reach a cluster on a laptop, so the certificate comes
# from a throwaway cert-manager CA ClusterIssuer; everything else matches
# production. Needs docker, k3d 5.9 or later (older k3d never sees a k3s 1.36
# server become ready), helm, kubectl, curl and openssl, plus network access to
# the chart repositories and image registries.
#
#   deploy/k3s/e2e.sh                 # create, check, delete the cluster
#   KEEP_CLUSTER=1 deploy/k3s/e2e.sh  # leave the cluster up for inspection
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CLUSTER="${CLUSTER:-perspectiv-e2e}"
HOST="${HOST:-perspectiv.test}"
HTTP_PORT="${HTTP_PORT:-8480}"
HTTPS_PORT="${HTTPS_PORT:-8443}"
KEEP_CLUSTER="${KEEP_CLUSTER:-0}"
# Pod and Service ranges away from k3s's 10.42/10.43 defaults, which a Docker
# host running its own Kubernetes (OrbStack, Rancher Desktop) already routes.
CLUSTER_CIDR="${CLUSTER_CIDR:-10.142.0.0/16}"
SERVICE_CIDR="${SERVICE_CIDR:-10.143.0.0/16}"
CLUSTER_DNS="${CLUSTER_DNS:-10.143.0.10}"
K3S_IMAGE="rancher/k3s:v1.36.5-k3s1"
INGRESS_NGINX_VERSION="4.15.1"
CERT_MANAGER_VERSION="v1.21.2"
IMAGE_REPO="perspectiv"
IMAGE_TAG="e2e"
NS="perspectiv"
REL="perspectiv"

for tool in docker k3d helm kubectl curl openssl; do
	command -v "$tool" >/dev/null || { echo "e2e: $tool is not on PATH" >&2; exit 1; }
done

WORK="$(mktemp -d)"
export KUBECONFIG="$WORK/kubeconfig"
PF_PIDS=()

cleanup() {
	local code=$?
	for pid in "${PF_PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
	if [[ $code -ne 0 ]]; then
		echo "e2e: FAILED; cluster state follows" >&2
		kubectl get pods,jobs,ingress,certificate -A 2>/dev/null >&2 || true
		kubectl -n "$NS" logs "job/$REL-migrate-1" --all-containers 2>/dev/null | tail -40 >&2 || true
	fi
	if [[ "$KEEP_CLUSTER" == 1 ]]; then
		echo "e2e: cluster $CLUSTER kept; KUBECONFIG=$KUBECONFIG"
	else
		k3d cluster delete "$CLUSTER" >/dev/null 2>&1 || true
		rm -rf "$WORK"
	fi
	exit "$code"
}
trap cleanup EXIT INT TERM

step() { printf '\n== %s\n' "$*"; }
fail() { echo "e2e: $*" >&2; exit 1; }

step "cluster $CLUSTER ($K3S_IMAGE, Traefik disabled)"
k3d cluster delete "$CLUSTER" >/dev/null 2>&1 || true
k3d cluster create "$CLUSTER" --image "$K3S_IMAGE" \
	--k3s-arg "--disable=traefik@server:0" \
	--k3s-arg "--cluster-cidr=$CLUSTER_CIDR@server:0" \
	--k3s-arg "--service-cidr=$SERVICE_CIDR@server:0" \
	--k3s-arg "--cluster-dns=$CLUSTER_DNS@server:0" \
	-p "$HTTP_PORT:80@loadbalancer" -p "$HTTPS_PORT:443@loadbalancer" \
	--kubeconfig-update-default=false --kubeconfig-switch-context=false --wait
k3d kubeconfig get "$CLUSTER" >"$KUBECONFIG"

step "image $IMAGE_REPO:$IMAGE_TAG"
docker build -f "$ROOT/Dockerfile" \
	--build-arg GO_VERSION="$(sed -n 's/^toolchain go//p' "$ROOT/go.mod")" \
	--build-arg VERSION=e2e --build-arg COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)" \
	--build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	-t "$IMAGE_REPO:$IMAGE_TAG" "$ROOT"
k3d image import "$IMAGE_REPO:$IMAGE_TAG" -c "$CLUSTER"

step "ingress-nginx $INGRESS_NGINX_VERSION"
helm upgrade --install ingress-nginx ingress-nginx \
	--repo https://kubernetes.github.io/ingress-nginx --version "$INGRESS_NGINX_VERSION" \
	--namespace ingress-nginx --create-namespace \
	-f "$ROOT/deploy/k3s/ingress-nginx-values.yaml" --wait --timeout 5m

step "cert-manager $CERT_MANAGER_VERSION"
helm upgrade --install cert-manager cert-manager \
	--repo https://charts.jetstack.io --version "$CERT_MANAGER_VERSION" \
	--namespace cert-manager --create-namespace --set crds.enabled=true --wait --timeout 5m
# A self-signed root, then a CA issuer signing the ingress certificate with it.
kubectl apply -f - <<'YAML'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: e2e-bootstrap
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: e2e-root
  namespace: cert-manager
spec:
  isCA: true
  commonName: perspectiv e2e root
  secretName: e2e-root
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: e2e-bootstrap
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: e2e-ca
spec:
  ca:
    secretName: e2e-root
YAML
kubectl -n cert-manager wait --for=condition=Ready --timeout=2m certificate/e2e-root
kubectl wait --for=condition=Ready --timeout=1m clusterissuer/e2e-ca
kubectl -n cert-manager get secret e2e-root -o jsonpath='{.data.ca\.crt}' | openssl base64 -d -A >"$WORK/ca.crt"

step "secret and chart"
kubectl create namespace "$NS"
kubectl -n "$NS" create secret generic perspectiv-secrets \
	--from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
	--from-literal=POSTGRES_PASSWORD="$(openssl rand -hex 24)"
CHART_ARGS=(
	--namespace "$NS"
	--set image.repository="$IMAGE_REPO" --set image.tag="$IMAGE_TAG" --set image.pullPolicy=Never
	--set host="$HOST"
	--set secrets.existingSecret=perspectiv-secrets
	--set tls.issuer.create=false --set tls.issuer.kind=ClusterIssuer --set tls.issuer.name=e2e-ca
	--set app.mail.provider=smtp --set app.mail.smtpAddr=localhost:1025
	--set "app.mail.from=Perspectiv <no-reply@perspectiv.test>"
	--set postgres.size=1Gi --set postgres.backup.size=1Gi --set api.avatars.size=1Gi
)
helm install "$REL" "$ROOT/deploy/chart" "${CHART_ARGS[@]}" --wait --timeout 10m

step "migration ran before api and worker"
kubectl -n "$NS" wait --for=condition=complete --timeout=1m "job/$REL-migrate-1"
job_done="$(kubectl -n "$NS" get job "$REL-migrate-1" -o jsonpath='{.status.completionTime}')"
for role in api worker; do
	kubectl -n "$NS" rollout status "deploy/$REL-$role" --timeout=2m
	pod="$(kubectl -n "$NS" get pod -l "app.kubernetes.io/component=$role" -o jsonpath='{.items[0].metadata.name}')"
	started="$(kubectl -n "$NS" get pod "$pod" -o jsonpath='{.status.containerStatuses[?(@.name=="perspectiv")].state.running.startedAt}')"
	[[ -n "$started" && ! "$started" < "$job_done" ]] || fail "$role started at '$started', before the migration finished at $job_done"
	role_env="$(kubectl -n "$NS" get pod "$pod" -o jsonpath='{.spec.containers[0].env[?(@.name=="ROLE")].value}')"
	[[ "$role_env" == "$role" ]] || fail "$role pod has ROLE=$role_env"
done
api_img="$(kubectl -n "$NS" get deploy "$REL-api" -o jsonpath='{.spec.template.spec.containers[0].image}')"
worker_img="$(kubectl -n "$NS" get deploy "$REL-worker" -o jsonpath='{.spec.template.spec.containers[0].image}')"
[[ "$api_img" == "$worker_img" ]] || fail "api image $api_img differs from worker image $worker_img"

step "certificate"
kubectl -n "$NS" wait --for=condition=Ready --timeout=3m "certificate/$REL-tls"

CURL=(curl -sS --max-time 10 --resolve "$HOST:$HTTPS_PORT:127.0.0.1" --cacert "$WORK/ca.crt")
BASE="https://$HOST:$HTTPS_PORT"

step "/healthz and /about through the ingress over TLS"
for i in $(seq 1 30); do
	code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/healthz" || true)"
	[[ "$code" == 200 ]] && break
	[[ $i == 30 ]] && fail "/healthz answered $code"
	sleep 2
done
"${CURL[@]}" -D "$WORK/headers" -o /dev/null "$BASE/healthz"
grep -qi '^strict-transport-security: max-age=31536000' "$WORK/headers" || fail "no HSTS header on /healthz: $(cat "$WORK/headers")"
code="$("${CURL[@]}" -o "$WORK/about" -w '%{http_code}' "$BASE/about")"
[[ "$code" == 200 ]] || fail "/about answered $code"
grep -q e2e "$WORK/about" || fail "/about does not report the build version: $(cat "$WORK/about")"

step "plain HTTP redirects to HTTPS"
code="$(curl -sS -o /dev/null -w '%{http_code}' --resolve "$HOST:$HTTP_PORT:127.0.0.1" "http://$HOST:$HTTP_PORT/healthz")"
[[ "$code" == 308 || "$code" == 301 ]] || fail "plain HTTP answered $code, want a redirect"

step "/metrics in-cluster only"
code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/metrics")"
[[ "$code" == 503 ]] || fail "/metrics at the edge answered $code, want 503"
port=18080
for role in api worker; do
	kubectl -n "$NS" port-forward "deploy/$REL-$role" "$port:8080" >/dev/null 2>&1 &
	PF_PIDS+=("$!")
	for i in $(seq 1 20); do
		curl -sf -o /dev/null "http://127.0.0.1:$port/healthz" && break
		[[ $i == 20 ]] && fail "$role port-forward never answered"
		sleep 1
	done
	curl -sf "http://127.0.0.1:$port/metrics" | grep -q '^# TYPE' || fail "$role /metrics did not answer"
	curl -sf -o /dev/null "http://127.0.0.1:$port/healthz" || fail "$role /healthz did not answer"
	port=$((port + 1))
done

step "backup"
kubectl -n "$NS" create job --from="cronjob/$REL-postgres-backup" e2e-backup
kubectl -n "$NS" wait --for=condition=complete --timeout=3m job/e2e-backup
kubectl -n "$NS" logs job/e2e-backup | grep -q 'backup: wrote /backups/perspectiv-' || fail "backup job wrote no archive"

step "upgrade migrates again"
helm upgrade "$REL" "$ROOT/deploy/chart" "${CHART_ARGS[@]}" --wait --timeout 10m
kubectl -n "$NS" wait --for=condition=complete --timeout=2m "job/$REL-migrate-2"
if kubectl -n "$NS" get job "$REL-migrate-1" >/dev/null 2>&1; then fail "revision 1 migration Job still exists after upgrade"; fi
"${CURL[@]}" -fsS -o /dev/null "$BASE/healthz" || fail "/healthz failed after upgrade"

step "PASS"
