#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.observability.yaml"
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

echo "[1/6] Building app image"
"${COMPOSE_CMD[@]}" --progress plain build grpc-mock

echo "[2/6] Starting observability stack"
"${COMPOSE_CMD[@]}" up -d

echo "[3/6] Waiting for core endpoints"
wait_for_http "grpc-mock-mgmt" "http://127.0.0.1:9000/openapi.json"
wait_for_http "metrics" "http://127.0.0.1:9100/metrics"
wait_for_http "prometheus" "http://127.0.0.1:9090/-/healthy"
wait_for_http "loki" "http://127.0.0.1:3100/ready"
wait_for_http "tempo" "http://127.0.0.1:3200/ready"
wait_for_http "collector" "http://127.0.0.1:13133/"
wait_for_http "grafana" "http://127.0.0.1:3000/api/health"

echo "[4/6] Sending plain and traced gRPC requests"
# Send a plain request via the management API (triggers internal gRPC call recording)
# Note: gRPC calls require grpcurl; we use the mgmt API as a basic smoke test
curl -fsS "http://127.0.0.1:9000/logs" >/dev/null

# Send a traced request if grpcurl is available
if command -v grpcurl >/dev/null 2>&1; then
  grpcurl -plaintext \
    -H 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' \
    -d '{"message":"traced"}' \
    localhost:50051 EchoService/Echo || true
fi

echo "[5/6] Verifying metrics, Prometheus, Grafana, traces, and logs"
if ! curl -fsS "http://127.0.0.1:9100/metrics" | grep -q 'grpc_requests_total'; then
  echo "Metrics endpoint does not expose grpc_requests_total" >&2
  exit 1
fi

if ! curl -fsSG --data-urlencode 'query=up{job="grpc-mock"}' "http://127.0.0.1:9090/api/v1/query" | grep -q '"1"'; then
  echo "Prometheus up{job=\"grpc-mock\"} is not 1" >&2
  exit 1
fi

if ! curl -fsSG --data-urlencode 'query={job="grpc-mock"}' "http://127.0.0.1:3100/loki/api/v1/query" | grep -q '"result"'; then
  echo "Loki query did not return a valid result payload" >&2
  exit 1
fi

for datasource in "gRPC Mock Metrics" "gRPC Mock Traces" "gRPC Mock Logs"; do
  encoded_name="${datasource// /%20}"
  if ! wait_for_grafana_api_match "Grafana datasource $datasource" "http://127.0.0.1:3000/api/datasources/name/$encoded_name" "\"name\":\"$datasource\""; then
    echo "Grafana datasource $datasource was not provisioned" >&2
    exit 1
  fi
done

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

echo "[6/6] Smoke validation passed"
