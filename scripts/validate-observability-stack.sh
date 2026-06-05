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

echo "[4/6] Sending correlated gRPC request"

# Generate deterministic per-run identifiers for correlation
SMOKE_REQ_ID="smoke-$(date +%s)-$$"
SMOKE_TRACE_ID="$(printf '%032x' $((RANDOM * RANDOM * RANDOM + RANDOM)))"
SMOKE_TRACEPARENT="00-${SMOKE_TRACE_ID}-$(printf '%016x' $RANDOM)-01"

echo "  request_id: ${SMOKE_REQ_ID}"
echo "  trace_id:   ${SMOKE_TRACE_ID}"

# grpcurl is required for the correlated smoke request
if ! command -v grpcurl >/dev/null 2>&1; then
  echo "ERROR: grpcurl is required for the observability smoke test but was not found." >&2
  echo "Install grpcurl: https://github.com/fullstorydev/grpcurl#installation" >&2
  exit 1
fi

grpcurl -plaintext \
  -H "x-request-id: ${SMOKE_REQ_ID}" \
  -H "traceparent: ${SMOKE_TRACEPARENT}" \
  -d '{"message":"smoke"}' \
  localhost:50051 EchoService/Echo || true

echo "[5/6] Verifying metrics, Prometheus, Grafana, traces, and logs"
if ! curl -fsS "http://127.0.0.1:9100/metrics" | grep -q 'grpc_requests_total'; then
  echo "Metrics endpoint does not expose grpc_requests_total" >&2
  exit 1
fi

if ! curl -fsSG --data-urlencode 'query=up{job="grpc-mock"}' "http://127.0.0.1:9090/api/v1/query" | grep -q '"1"'; then
  echo "Prometheus up{job=\"grpc-mock\"} is not 1" >&2
  exit 1
fi

# Query Loki for the exact smoke request id to prove the correlated request reached log collection
echo "  Querying Loki for request_id=${SMOKE_REQ_ID} ..."
LOKI_REQ_FOUND=false
for _i in $(seq 1 15); do
  sleep 2
  LOKI_RESP="$(curl -fsSG --data-urlencode "query={job=\"grpc-mock\"} |= \"${SMOKE_REQ_ID}\"" "http://127.0.0.1:3100/loki/api/v1/query" 2>/dev/null || echo "")"
  if echo "$LOKI_RESP" | grep -q "${SMOKE_REQ_ID}"; then
    LOKI_REQ_FOUND=true
    break
  fi
done
if [[ "$LOKI_REQ_FOUND" != "true" ]]; then
  echo "ERROR: Loki did not return log lines containing request_id=${SMOKE_REQ_ID}" >&2
  exit 1
fi
echo "  Found smoke request_id in Loki"

# Verify trace export: try exact trace id lookup in Tempo first, fall back to collector metrics
echo "  Querying Tempo for trace_id=${SMOKE_TRACE_ID} ..."
TRACE_FOUND=false
for _i in $(seq 1 15); do
  sleep 2
  TEMPO_RESP="$(curl -fsS "http://127.0.0.1:3200/api/traces/${SMOKE_TRACE_ID}" 2>/dev/null || echo "")"
  if echo "$TEMPO_RESP" | grep -q "${SMOKE_TRACE_ID}"; then
    TRACE_FOUND=true
    break
  fi
done
if [[ "$TRACE_FOUND" == "true" ]]; then
  echo "  Found smoke trace_id in Tempo"
else
  # Fallback: verify the OTel collector received spans
  echo "  Exact trace lookup not available; verifying collector accepted spans ..."
  COLLECTOR_METRICS="$(curl -fsS "http://127.0.0.1:13133/metrics" 2>/dev/null || echo "")"
  if echo "$COLLECTOR_METRICS" | grep -q 'otelcol_receiver_accepted_spans'; then
    ACCEPTED=$(echo "$COLLECTOR_METRICS" | grep 'otelcol_receiver_accepted_spans' | grep -v '#' | awk '{sum+=$2} END {print sum+0}')
    if [[ "$ACCEPTED" -gt 0 ]]; then
      echo "  Collector accepted ${ACCEPTED} spans (trace_id=${SMOKE_TRACE_ID} not directly confirmed)"
    else
      echo "ERROR: Collector accepted 0 spans (trace_id=${SMOKE_TRACE_ID})" >&2
      exit 1
    fi
  else
    echo "ERROR: Could not verify trace export (trace_id=${SMOKE_TRACE_ID})" >&2
    exit 1
  fi
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
