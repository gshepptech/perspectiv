#!/usr/bin/env bash
# Local dev loop for perspectiv: Postgres 18 and Mailpit in Docker, the server
# under Air, the web app under Vite. The Makefile's dev-* targets call this.
#
# usage: scripts/dev.sh <command>
#   up            start containers, apply migrations, start server and web
#   down          stop server, web and containers (the Postgres volume is kept)
#   status        show what runs and whether /healthz answers (non-zero if not)
#   logs          tail the server and web logs
#   migrate       apply pending migrations to the dev database
#   migrate-down  roll back the last goose migration
#   grafana-up    start Grafana, Prometheus, Tempo and Loki
#   grafana-down  stop the observability stack
#   e2e           run the Playwright suite against the running loop
#   env           print the effective non-secret dev settings
#   help          show this text
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEV_DIR="$ROOT/tmp/dev"
LOG_DIR="$DEV_DIR/logs"
PID_FILE="$DEV_DIR/pids"
PG_CONTAINER=perspectiv-postgres
PG_VOLUME=perspectiv-pgdata
MAILPIT_CONTAINER=perspectiv-mailpit
OBS_PROJECT=perspectiv-obs
OBS_COMPOSE="$ROOT/scripts/grafana/compose.yaml"
AIR="${AIR:-air}"
GOOSE="${GOOSE:-goose}"

# Background processes this invocation started; killed if it is interrupted.
CHILDREN=()

usage() {
	sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed -e '$d' -e 's/^# \{0,1\}//'
}

die() {
	echo "dev.sh: $*" >&2
	exit 1
}

kill_group() {
	# Each background job runs in its own process group (set -m), so the
	# group id is the job's pid and the signal reaches its children too.
	kill -TERM -- "-$1" 2>/dev/null || kill -TERM "$1" 2>/dev/null || true
}

on_signal() {
	local pid
	for pid in "${CHILDREN[@]+"${CHILDREN[@]}"}"; do
		kill_group "$pid"
	done
	exit 130
}
trap on_signal INT TERM

# load_env sources .env.dev then .env.dev.local, creating the local copy on first run.
load_env() {
	[[ -f "$ROOT/.env.dev" ]] || die ".env.dev is missing from $ROOT"
	if [[ ! -f "$ROOT/.env.dev.local" ]]; then
		cp "$ROOT/.env.dev" "$ROOT/.env.dev.local"
		echo "created .env.dev.local from .env.dev; edit .env.dev.local for local overrides" >&2
	fi
	set -a
	# shellcheck source=/dev/null
	source "$ROOT/.env.dev"
	# shellcheck source=/dev/null
	source "$ROOT/.env.dev.local"
	set +a
}

# ensure_encryption_key gives the dev server an age key for data encrypted
# at rest (two-factor secrets). No key is tracked in the repository, so when
# .env.dev.local has none, dev-up generates one with the server's own
# `perspectiv keygen` (pkg/crypto) into that gitignored file, and every later
# run reuses it.
ensure_encryption_key() {
	[[ -n "${ENCRYPTION_KEYS:-}" ]] && return 0
	local key tmp
	key="$(cd "$ROOT" && go run ./cmd/perspectiv keygen)" && [[ "$key" == AGE-SECRET-KEY-* ]] ||
		die "could not generate ENCRYPTION_KEYS with perspectiv keygen; set it in .env.dev.local"
	tmp="$(mktemp)"
	grep -v '^ENCRYPTION_KEYS=' "$ROOT/.env.dev.local" >"$tmp" || true
	printf 'ENCRYPTION_KEYS=%s\n' "$key" >>"$tmp"
	mv "$tmp" "$ROOT/.env.dev.local"
	export ENCRYPTION_KEYS="$key"
	echo "generated ENCRYPTION_KEYS into .env.dev.local" >&2
}

hint() {
	case "$1" in
	docker) echo "install Docker Desktop, OrbStack or Docker Engine 27+" ;;
	go) echo "install Go from https://go.dev/dl (go.mod selects the toolchain)" ;;
	node | npx) echo "install Node 24 LTS (nvm install 24) or Node 26" ;;
	pnpm) echo "corepack enable pnpm" ;;
	curl) echo "install curl with your package manager" ;;
	*) echo "run make tools" ;;
	esac
}

# require checks that every named command is on PATH and prints install hints.
require() {
	local missing=0 cmd
	for cmd in "$@"; do
		if ! command -v "$cmd" >/dev/null 2>&1; then
			echo "missing: $cmd ($(hint "$(basename "$cmd")"))" >&2
			missing=1
		fi
	done
	[[ $missing -eq 0 ]] || exit 1
}

port_in_use() {
	(exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

container_state() {
	local state
	state="$(docker inspect -f '{{.State.Status}}' "$1" 2>/dev/null)" || state=absent
	echo "$state"
}

# start_container starts an existing container or runs a new one with the given args.
start_container() {
	local name=$1
	shift
	case "$(container_state "$name")" in
	running) return 0 ;;
	absent) docker run -d --name "$name" "$@" >/dev/null ;;
	*) docker start "$name" >/dev/null ;;
	esac
}

check_port_free() {
	local name=$1 port=$2 var=$3
	if [[ "$(container_state "$name")" != running ]] && port_in_use "$port"; then
		die "port $port is in use by another process; set $var in .env.dev.local"
	fi
}

start_postgres() {
	check_port_free "$PG_CONTAINER" "$POSTGRES_PORT" POSTGRES_PORT
	# Postgres 18 images keep data under /var/lib/postgresql/18, so the
	# volume mounts the parent directory.
	start_container "$PG_CONTAINER" \
		-e POSTGRES_USER="$POSTGRES_USER" -e POSTGRES_PASSWORD="$POSTGRES_PASSWORD" -e POSTGRES_DB="$POSTGRES_DB" \
		-p "127.0.0.1:$POSTGRES_PORT:5432" -v "$PG_VOLUME:/var/lib/postgresql" "$POSTGRES_IMAGE"
	local i
	for i in $(seq 1 60); do
		if docker exec "$PG_CONTAINER" pg_isready -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" 2>/dev/null; then
			echo "postgres  ready on 127.0.0.1:$POSTGRES_PORT"
			return 0
		fi
		sleep 1
	done
	die "postgres did not become ready within 60s; see: docker logs $PG_CONTAINER (last try $i)"
}

start_mailpit() {
	check_port_free "$MAILPIT_CONTAINER" "$MAILPIT_SMTP_PORT" MAILPIT_SMTP_PORT
	check_port_free "$MAILPIT_CONTAINER" "$MAILPIT_HTTP_PORT" MAILPIT_HTTP_PORT
	start_container "$MAILPIT_CONTAINER" \
		-p "127.0.0.1:$MAILPIT_SMTP_PORT:1025" -p "127.0.0.1:$MAILPIT_HTTP_PORT:8025" "$MAILPIT_IMAGE"
	local i
	for i in $(seq 1 30); do
		if curl -fsS "$MAILPIT_URL/api/v1/info" >/dev/null 2>&1; then
			echo "mailpit   SMTP 127.0.0.1:$MAILPIT_SMTP_PORT, API $MAILPIT_URL"
			return 0
		fi
		sleep 1
	done
	die "mailpit API did not answer at $MAILPIT_URL within 30s (last try $i)"
}

has_server() { [[ -f "$ROOT/cmd/perspectiv/main.go" ]]; }
has_web() { [[ -f "$ROOT/web/package.json" ]]; }

run_migrations() {
	if ! has_server; then
		echo "migrate   skipped: cmd/perspectiv does not exist yet"
		return 0
	fi
	(cd "$ROOT" && go run ./cmd/perspectiv migrate)
	echo "migrate   applied"
}

rotate_logs() {
	mkdir -p "$LOG_DIR"
	local stamp f
	stamp="$(date +%Y%m%dT%H%M%S)"
	for f in server.log web.log; do
		[[ -f "$LOG_DIR/$f" ]] && mv "$LOG_DIR/$f" "$LOG_DIR/$f.$stamp"
	done
	return 0
}

# alive reports whether a recorded process (by name) is running.
alive() {
	local pid
	pid="$(pid_of "$1")"
	[[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

pid_of() {
	[[ -f "$PID_FILE" ]] || return 0
	awk -v n="$1" '$1 == n { print $2 }' "$PID_FILE"
}

start_background() {
	local name=$1 log=$2
	shift 2
	set -m
	nohup "$@" >"$log" 2>&1 &
	local pid=$!
	set +m
	CHILDREN+=("$pid")
	echo "$name $pid" >>"$PID_FILE"
	echo "$(printf '%-9s' "$name") pid $pid, log ${log#"$ROOT"/}"
}

http_port() {
	echo "${HTTP_ADDR##*:}"
}

cmd_up() {
	require docker go node npx curl "$AIR"
	load_env
	if alive server || alive web; then
		die "the dev loop is already running; make dev-down first"
	fi
	ensure_encryption_key
	mkdir -p "$DEV_DIR" "$ROOT/$AVATAR_DIR"
	rm -f "$PID_FILE"
	rotate_logs
	start_postgres
	start_mailpit
	run_migrations
	if has_server; then
		start_background server "$LOG_DIR/server.log" "$AIR" -c "$ROOT/.air.perspectiv.toml"
	else
		echo "server    skipped: cmd/perspectiv does not exist yet"
	fi
	if has_web; then
		require pnpm
		[[ -d "$ROOT/web/node_modules" ]] || pnpm --dir "$ROOT/web" install
		start_background web "$LOG_DIR/web.log" pnpm --dir "$ROOT/web" run dev
	else
		echo "web       skipped: web/package.json does not exist yet"
	fi
	CHILDREN=()
	echo "up: server http://localhost:$(http_port), web $BASE_URL, mail $MAILPIT_URL"
}

cmd_down() {
	require docker
	local name pid
	if [[ -f "$PID_FILE" ]]; then
		while read -r name pid; do
			[[ -n "$pid" ]] || continue
			kill_group "$pid"
			echo "stopped $name (pid $pid)"
		done <"$PID_FILE"
		rm -f "$PID_FILE"
	fi
	for name in "$PG_CONTAINER" "$MAILPIT_CONTAINER"; do
		if [[ "$(container_state "$name")" == running ]]; then
			docker stop "$name" >/dev/null
			echo "stopped $name"
		fi
	done
}

cmd_status() {
	require docker curl
	load_env
	local ok=0 name state code
	for name in "$PG_CONTAINER" "$MAILPIT_CONTAINER"; do
		state="$(container_state "$name")"
		printf '%-22s %s\n' "$name" "$state"
		[[ "$state" == running ]] || ok=1
	done
	for name in server web; do
		if alive "$name"; then
			printf '%-22s running (pid %s)\n' "$name" "$(pid_of "$name")"
		else
			printf '%-22s stopped\n' "$name"
			ok=1
		fi
	done
	code="$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$(http_port)/healthz" || true)"
	printf '%-22s %s\n' "/healthz" "${code:-no answer}"
	[[ "$code" == 200 ]] || ok=1
	return "$ok"
}

cmd_logs() {
	local files=()
	[[ -f "$LOG_DIR/server.log" ]] && files+=("$LOG_DIR/server.log")
	[[ -f "$LOG_DIR/web.log" ]] && files+=("$LOG_DIR/web.log")
	[[ ${#files[@]} -gt 0 ]] || die "no logs yet; run make dev-up"
	tail -n 100 -F "${files[@]}" &
	CHILDREN+=("$!")
	wait
}

cmd_migrate() {
	require docker go
	load_env
	start_postgres
	run_migrations
}

cmd_migrate_down() {
	require docker "$GOOSE"
	load_env
	start_postgres
	"$GOOSE" -dir "$ROOT/internal/db/migrations" postgres "$DATABASE_URL" down
}

cmd_grafana_up() {
	require docker
	load_env
	# Prometheus scrapes the server on the host at whatever port HTTP_ADDR
	# names, so the checked-in config is rendered with it.
	local port="${HTTP_ADDR##*:}"
	mkdir -p "$ROOT/tmp/dev"
	sed "s/host.docker.internal:8080/host.docker.internal:$port/" "$ROOT/scripts/grafana/prometheus.yml" >"$ROOT/tmp/dev/prometheus.yml"
	export PROMETHEUS_CONFIG="$ROOT/tmp/dev/prometheus.yml"
	docker compose -p "$OBS_PROJECT" -f "$OBS_COMPOSE" up -d
	echo "grafana    http://localhost:$GRAFANA_PORT"
	echo "prometheus http://localhost:$PROMETHEUS_PORT (scrapes the server's /metrics on :$port)"
	echo "tempo      OTLP gRPC localhost:$TEMPO_OTLP_GRPC_PORT; set OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:$TEMPO_OTLP_GRPC_PORT in .env.dev.local"
	echo "loki       http://localhost:$LOKI_PORT"
}

cmd_grafana_down() {
	require docker
	load_env
	docker compose -p "$OBS_PROJECT" -f "$OBS_COMPOSE" down
}

cmd_e2e() {
	require node npx pnpm curl
	load_env
	has_web || die "web/package.json does not exist yet"
	curl -fsS "$MAILPIT_URL/api/v1/info" >/dev/null || die "mailpit is not answering at $MAILPIT_URL; run make dev-up"
	(cd "$ROOT/web" && pnpm exec playwright test)
}

cmd_env() {
	load_env
	local key
	for key in ROLE HTTP_ADDR PUBLIC_BASE_URL COOKIE_SECURE MAIL_PROVIDER SMTP_ADDR MAIL_FROM \
		RANK_INTERVAL CANON_MIN_MEMBERS EXPIRY_WINDOW AVATAR_DIR MIGRATE_ON_START LOG_LEVEL \
		OTEL_EXPORTER_OTLP_ENDPOINT POSTGRES_IMAGE POSTGRES_PORT MAILPIT_IMAGE MAILPIT_SMTP_PORT \
		MAILPIT_HTTP_PORT MAILPIT_URL WEB_PORT BASE_URL; do
		printf '%s=%s\n' "$key" "${!key-}"
	done
}

main() {
	local cmd="${1:-help}"
	case "$cmd" in
	up) cmd_up ;;
	down) cmd_down ;;
	status) cmd_status ;;
	logs) cmd_logs ;;
	migrate) cmd_migrate ;;
	migrate-down) cmd_migrate_down ;;
	grafana-up) cmd_grafana_up ;;
	grafana-down) cmd_grafana_down ;;
	e2e) cmd_e2e ;;
	env) cmd_env ;;
	help | -h | --help) usage ;;
	*)
		usage >&2
		die "unknown command: $cmd"
		;;
	esac
}

main "$@"
