#!/usr/bin/env bash
# CSCAN local dev: one-shot launcher for the full local stack on macOS/Linux.
# Dependency volumes are kept by default; set CLEAN_DEV_VOLUMES=1 to remove them on exit.

set -u

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$PROJECT_ROOT"

export CSCAN_DEV="${CSCAN_DEV:-1}"
if [ -z "${CSCAN_WORKER_KEY:-}" ]; then
    CSCAN_WORKER_KEY="$(od -An -N24 -tx1 /dev/urandom | tr -d '[:space:]')"
fi
if [ -z "$CSCAN_WORKER_KEY" ]; then
    printf '[dev] Failed to generate CSCAN_WORKER_KEY; set it explicitly and retry.\n' >&2
    exit 1
fi
export CSCAN_WORKER_KEY
export CSCAN_MONGO_URI="${CSCAN_MONGO_URI:-mongodb://127.0.0.1:27017}"
CLEAN_DEV_VOLUMES="${CLEAN_DEV_VOLUMES:-0}"

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        printf '[dev] Required command not found: %s\n' "$1" >&2
        exit 1
    fi
}

require_command docker
require_command go
require_command npm

if docker compose version >/dev/null 2>&1; then
    COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE=(docker-compose)
else
    printf '[dev] Docker Compose is required (docker compose or docker-compose).\n' >&2
    exit 1
fi

WEB_DIR="$PROJECT_ROOT/web"

ensure_web_dependencies() {
    if [ ! -x "$WEB_DIR/node_modules/.bin/vite" ]; then
        printf '[dev] Web dependencies are missing or incomplete; installing locked dev dependencies...\n'
        (
            cd "$WEB_DIR" || exit 1
            if [ -f package-lock.json ]; then
                npm ci --include=dev
            else
                npm install
            fi
        ) || {
            printf '[dev] Failed to install web dependencies.\n' >&2
            return 1
        }
    fi

    if [ ! -x "$WEB_DIR/node_modules/.bin/vite" ]; then
        printf '[dev] Vite is unavailable after installing web dependencies.\n' >&2
        return 1
    fi
}

configure_docker_host() {
    local docker_context
    local docker_host

    if [ -n "${DOCKER_HOST:-}" ]; then
        return
    fi

    docker_context="$(docker context show 2>/dev/null || true)"
    docker_host=""
    if [ -n "$docker_context" ]; then
        docker_host="$(docker context inspect "$docker_context" --format '{{ .Endpoints.docker.Host }}' 2>/dev/null || true)"
    fi

    case "$docker_host" in
        unix://*)
            export DOCKER_HOST="$docker_host"
            printf '[dev] Using Docker context endpoint: %s\n' "$DOCKER_HOST"
            ;;
    esac
}

if ! ensure_web_dependencies; then
    exit 1
fi
configure_docker_host

LOG_DIR="$PROJECT_ROOT/log"
mkdir -p "$LOG_DIR"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
API_LOG="$LOG_DIR/api-$TIMESTAMP.log"
WORKER_LOG="$LOG_DIR/worker-$TIMESTAMP.log"
WEB_LOG="$LOG_DIR/web-$TIMESTAMP.log"

api_pid=""
worker_pid=""
web_pid=""
dependencies_started=0

terminate_process_tree() {
    local pid="$1"
    local child

    for child in $(pgrep -P "$pid" 2>/dev/null || true); do
        terminate_process_tree "$child"
    done

    kill -TERM "$pid" 2>/dev/null || true
}

force_kill_process_tree() {
    local pid="$1"
    local child

    for child in $(pgrep -P "$pid" 2>/dev/null || true); do
        force_kill_process_tree "$child"
    done

    kill -KILL "$pid" 2>/dev/null || true
}

cleanup() {
    local exit_status=$?
    local pid

    trap - EXIT INT TERM
    printf '\n[dev] Stopping all local services...\n'

    for pid in "$api_pid" "$worker_pid" "$web_pid"; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            terminate_process_tree "$pid"
        fi
    done

    sleep 1

    for pid in "$api_pid" "$worker_pid" "$web_pid"; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            force_kill_process_tree "$pid"
        fi
    done

    for pid in "$api_pid" "$worker_pid" "$web_pid"; do
        if [ -n "$pid" ]; then
            wait "$pid" 2>/dev/null || true
        fi
    done

    if [ "$dependencies_started" -eq 1 ]; then
        if [ "$CLEAN_DEV_VOLUMES" = "1" ]; then
            printf '[dev] Stopping dependency stack and removing volumes...\n'
            "${COMPOSE[@]}" -f docker-compose.dev.yaml down -v 2>/dev/null || true
        else
            printf '[dev] Stopping dependency stack (volumes are preserved; set CLEAN_DEV_VOLUMES=1 to remove them)...\n'
            "${COMPOSE[@]}" -f docker-compose.dev.yaml down 2>/dev/null || true
        fi
    fi

    printf '[dev] All stopped\n'
    exit "$exit_status"
}

trap cleanup EXIT INT TERM

printf '[dev] Starting dependency stack (MongoDB + Redis)...\n'
if ! "${COMPOSE[@]}" -f docker-compose.dev.yaml up -d; then
    printf '[dev] Failed to start dependency stack\n' >&2
    exit 1
fi
dependencies_started=1

(
    exec go run api/cscan.go -f api/etc/cscan.yaml
) >"$API_LOG" 2>&1 &
api_pid=$!

(
    exec go run worker/main.go -s http://localhost:8888
) >"$WORKER_LOG" 2>&1 &
worker_pid=$!

(
    cd "$PROJECT_ROOT/web" || exit 1
    if [ ! -d node_modules ]; then
        npm install
    fi
    exec npm run dev
) >"$WEB_LOG" 2>&1 &
web_pid=$!

printf '[dev] Local dev stack started (Deps / API / Worker / Web)\n'
printf '[dev] Logs:\n'
printf '  API   : %s\n' "$API_LOG"
printf '  Worker: %s\n' "$WORKER_LOG"
printf '  Web   : %s\n' "$WEB_LOG"
printf '[dev] Open http://localhost:7777\n'
printf '[dev] Press Ctrl+C to stop all (volumes are preserved by default; set CLEAN_DEV_VOLUMES=1 to remove them).\n'

wait "$api_pid" "$worker_pid" "$web_pid"
