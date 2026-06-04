#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose-grafana.yaml"
COMPOSE_CMD=(docker compose)
COMPOSE_ENV_FILE="$ROOT_DIR/.env"

if [[ ! -f "$COMPOSE_ENV_FILE" && -f "$ROOT_DIR/deploy/.env" ]]; then
  COMPOSE_ENV_FILE="$ROOT_DIR/deploy/.env"
fi

if [[ -f "$COMPOSE_ENV_FILE" ]]; then
  COMPOSE_CMD+=(--env-file "$COMPOSE_ENV_FILE")
fi

COMPOSE_CMD+=(-f "$COMPOSE_FILE")

cleanup() {
  "${COMPOSE_CMD[@]}" down >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_for_http() {
  local name="$1"
  local url="$2"
  local retries="${3:-60}"
  local i
  for ((i = 1; i <= retries; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "Timed out waiting for $name at $url" >&2
  return 1
}

wait_for_grafana_api_match() {
  local name="$1"
  local url="$2"
  local pattern="$3"
  local retries="${4:-30}"
  local i
  for ((i = 1; i <= retries; i++)); do
    if curl -fsS -u admin:admin "$url" | grep -q "$pattern"; then
      return 0
    fi
    sleep 2
  done
  echo "Timed out waiting for $name at $url" >&2
  return 1
}

echo "[1/5] Building app image"
"${COMPOSE_CMD[@]}" --progress plain build grpc-mock

echo "[2/5] Starting observability stack"
"${COMPOSE_CMD[@]}" up -d

echo "[3/5] Waiting for core endpoints"
wait_for_http "grpc-mock-mgmt" "http://127.0.0.1:9000/openapi.json"
wait_for_http "metrics" "http://127.0.0.1:9100/metrics"
wait_for_http "prometheus" "http://127.0.0.1:9090/-/healthy"
wait_for_http "grafana" "http://127.0.0.1:3000/api/health"

echo "[4/5] Verifying metrics and Prometheus"
if ! curl -fsS "http://127.0.0.1:9100/metrics" | grep -q 'grpc_requests_total'; then
  echo "Metrics endpoint does not expose grpc_requests_total" >&2
  exit 1
fi

if ! curl -fsSG --data-urlencode 'query=up{job="grpc-mock"}' "http://127.0.0.1:9090/api/v1/query" | grep -q '"1"'; then
  echo "Prometheus up{job=\"grpc-mock\"} is not 1" >&2
  exit 1
fi

echo "[5/5] Verifying Grafana datasource and dashboards"
if ! wait_for_grafana_api_match "Grafana datasource" "http://127.0.0.1:3000/api/datasources/name/gRPC%20Mock%20Metrics" '"name":"gRPC Mock Metrics"'; then
  echo "Grafana Prometheus datasource was not provisioned" >&2
  exit 1
fi

for dashboard_uid in \
  grpc-mock-methods-overview \
  grpc-mock-method-details \
  grpc-mock-resources-overview
do
  if ! wait_for_grafana_api_match "Grafana dashboard $dashboard_uid" "http://127.0.0.1:3000/api/dashboards/uid/$dashboard_uid" "\"uid\":\"$dashboard_uid\""; then
    echo "Grafana dashboard $dashboard_uid was not provisioned" >&2
    exit 1
  fi
done

echo "Observability stack validation passed"
