# gRPC-Mock Convergence — Iterative Baby-Step Plan

Derived from `../COMPARISON_REPORT.md` (workspace root). This document converts the report's gap findings into an
ordered sequence of **small, independently shippable steps** for the `grpc-mock` repo, each driven by the same
workflow loop. The end state brings `grpc-mock` to parity with `openapi-mock` on the stated product goals
(G2 trace-ID, G3 traces, G5 management surface, G6 structured logs, G7 full observability stack), plus the
lower-severity polish items.

All paths below are relative to `grpc-mock/` unless noted.

## Workflow Loop (applies to every step)

1. **Implement** one small, logical unit of work (a single step below).
2. **Run DoD Verification — in order:**
    1. **Lint:** `gofmt -l .` (must print nothing) **and** `go vet ./...`
    2. **Test:** `go test ./...`
    3. **Extra Check:** the step-specific command listed under each step.
3. **Decide:**
    - ✅ **All pass →** `git commit` immediately with the step's commit message, then move to the next step
      without pausing.
    - ❌ **Any fail →** fix locally and re-run the full verification from the top. **Do not commit** until Lint,
      Test, and Extra Check all pass.

### DoD Verification reference commands

```bash
# Lint
gofmt -l .            # expect: no output
go vet ./...

# Test
go test ./...

# Extra Check (varies per step — see each step)
```

> Note: no `golangci-lint`/CI exists in the repo today. `gofmt -l .` + `go vet ./...` is the baseline lint gate.
> If a step introduces CI or a linter, later steps may upgrade this gate.

### Priority order

The phases follow the report's severity ranking: 🔴 High first (P1–P4), then 🟡 Medium (P5–P8), then 🟢 Low (P9–P12).
Within the high-severity band the order is deliberate: **structured logging is built first** because it unblocks Loki
shipping, then **trace propagation/collection**, then the **compose stack** that ships both. Each step is a separate
commit; phases are split where a smaller commit is safer.

| Band      | Phases   | Report gaps addressed                                            |
|:----------|:---------|:-----------------------------------------------------------------|
| 🔴 High   | P1 – P4  | G6 structured logs, G2/G3 trace propagation+collection, G7 stack |
| 🟡 Medium | P5 – P8  | G2 request-id forwarding, G5 mgmt surface, streaming, `/clear`   |
| 🟢 Low    | P9 – P12 | updater tests, recorder typing, `go tool` wire, `.gitignore`     |

---

## Phase 1 — 🔴 Structured logging foundation (G6)

Goal: replace stdlib `log` with a structured, level-aware, context-scoped logger so logs are parseable and
Loki-shippable. This is the prerequisite for P3 and P4. Build the package first, then wire it into the interceptor,
then into stubs, then config.

### Step 1.1 — Add `pkg/observability` logger + context helpers

- Create `pkg/observability/logging.go` mirroring openapi-mock: a `zerolog`-based `NewLogger(LogConfig)` with
  `Format` (json/console), `Output` (stdout/file), `File`, `Level`.
- Create `pkg/observability/context.go` with `RequestMetadata{RequestID, TraceID, Method}`, plus
  `With*/Get*` helpers and `Logger(ctx, fallback)` / `WithLogger(ctx, logger)`.
- Add `github.com/rs/zerolog` to `go.mod`; run `go mod tidy`.
- Add `pkg/observability/logging_test.go` + `context_test.go` covering level parsing, file output, and
  context round-trips.
- **Extra Check:** `go test ./pkg/observability/...`
- **Commit:** `feat(observability): add zerolog structured logger and request-metadata context`

### Step 1.2 — Route the recording interceptor through the contextual logger

- `cmd/grpc-mock/main.go`: build a base `zerolog.Logger` in `runServer` and pass it into `recordingInterceptor`.
- In `recordingInterceptor`, derive a per-request logger (`request_id`, `method` fields), store it in context via
  `observability.WithLogger`, and replace the `log.Printf` start/success/error/panic lines with structured logger
  calls (`Info`/`Error` with `duration_ms`, `status`).
- Keep behaviour gated by `EnableLogging`.
- **Extra Check:** `go run ./cmd/grpc-mock run --logging 2>&1 | head` emits JSON lines with `request_id`
  (smoke a single call via grpcurl if available, otherwise verify startup log shape).
- **Commit:** `feat(server): emit structured request logs from the recording interceptor`

### Step 1.3 — Migrate stub logging to the context logger

- `internal/stubs/echo/echo_server.go` (and `internal/stubs/complex/*`): replace `log.Printf` +
  `ctxkeys.RequestID` lookups with `observability.Logger(ctx, zerolog.Nop())`.
- Update the `upd-stubs` generator template (`cmd/upd-stubs/stubs.go`) so newly generated stubs use the
  contextual logger pattern instead of stdlib `log`.
- **Extra Check:** `make stub && git diff --exit-code internal/stubs` (regeneration is stable) **and**
  `go test ./...`.
- **Commit:** `refactor(stubs): use contextual zerolog logger in generated and hand-written stubs`

### Step 1.4 — Add logging config (env + flags) to the CLI

- `cmd/grpc-mock/main.go` `Config`: add `LOG_FORMAT` (json), `LOG_OUTPUT` (stdout), `LOG_FILE`, `LOG_LEVEL` (info)
  env fields and matching `--log-format/--log-output/--log-file/--log-level` flags (flags > env > positional rules
  unchanged).
- Update `README.md` env/flags tables.
- **Extra Check:** `go run ./cmd/grpc-mock run --log-format console --log-level debug 2>&1 | head` honours the
  flags.
- **Commit:** `feat(cli): configurable log format/output/level via env and flags`

---

## Phase 2 — 🟡→prereq: request-id forwarding from gRPC metadata (G2)

Goal: stop discarding inbound correlation IDs. Done now (not later) because P3 builds the trace/metadata plumbing on
top of it. (This is the 🟡 G2 request-id item, pulled forward as a dependency.)

### Step 2.1 — Resolve incoming request-id from metadata, echo it back

- Add `pkg/observability/request_id.go` with `ResolveRequestID(md, allowedKeys)` + `GenerateRequestID()`
  (mirror openapi-mock semantics; default keys `x-request-id`, `x-correlation-id`).
- `recordingInterceptor`: read `metadata.FromIncomingContext`, resolve the request-id (fallback to generated),
  store in `RequestMetadata`, and send it back via `grpc.SetHeader` as `x-request-id`.
- Add `REQUEST_ID_HEADERS` / `REQUEST_ID_RESPONSE_HEADER` env + flags.
- Add a unit test exercising resolve-vs-generate.
- **Extra Check:** `go test ./pkg/observability/...` **and** a grpcurl call passing `-H 'x-request-id: abc'`
  shows `abc` in `/logs` and in response metadata.
- **Commit:** `feat(server): forward inbound request-id from gRPC metadata and echo in response`

---

## Phase 3 — 🔴 Trace ID forwarding + OpenTelemetry collection (G2, G3)

Goal: extract W3C trace context from inbound metadata, create server spans, and export them. Build the tracing
package, then wire the interceptor, then expose config.

### Step 3.1 — Add `pkg/observability/tracing.go`

- Mirror openapi-mock: `SetupTracing(ctx, TraceConfig)` registering the W3C `TraceContext`+`Baggage` propagator and
  building a `file` or `otlp-http` exporter with ratio/always sampler; return a shutdown func.
- Add OTel SDK deps (`go.opentelemetry.io/otel`, `.../sdk`, `.../exporters/otlp/otlptrace/otlptracehttp`,
  `.../exporters/stdout/stdouttrace`, `.../propagation`, `semconv`); `go mod tidy`.
- Add `tracing_test.go` covering the disabled/no-op path and exporter selection error handling.
- **Extra Check:** `go test ./pkg/observability/...`
- **Commit:** `feat(observability): add OpenTelemetry tracing setup (file/otlp-http exporters)`

### Step 3.2 — Start spans in the interceptor and enrich logs/metadata with trace-id

- `recordingInterceptor`: extract context from inbound metadata via the propagator, start a server span named by the
  full method, set `rpc.method`/`rpc.status_code` attributes, record errors/panics on the span, and put the
  resulting `trace_id` into `RequestMetadata` + the request logger fields.
- Call `SetupTracing` in `runServer`; defer shutdown.
- **Extra Check:** with `TRACE_ENABLED=true TRACE_EXPORTER=file` a call appends a span containing the inbound
  `trace_id` to the trace file.
- **Commit:** `feat(server): create server spans and propagate trace-id through context and logs`

### Step 3.3 — Add tracing config (env + flags) and README

- `Config`: add `TRACE_ENABLED` (false), `TRACE_EXPORTER` (none), `TRACE_ENDPOINT`, `TRACE_FILE`,
  `TRACE_SAMPLING_RATIO` (1.0) + matching flags.
- Document tracing in `README.md` (new "Трассировка" section) and the env/flag tables.
- **Extra Check:** `grep -n "TRACE_ENABLED" README.md` present; `go run ./cmd/grpc-mock run --help` lists trace
  flags.
- **Commit:** `feat(cli): tracing configuration via env and flags; document tracing`

---

## Phase 4 — 🔴 Full observability compose stack (G7)

Goal: ship metrics + logs + traces to Grafana/Loki/Tempo, matching openapi-mock. Split deploy configs, compose file,
Make targets, and smoke validation.

### Step 4.1 — Add flat deploy configs

- Create `deploy/` with `prometheus.yaml` (move/align the existing root `prometheus.yaml`), `otel.yaml`
  (OTLP→Tempo), `tempo.yaml`, `promtail.yaml` (tail the app log file → Loki), and `deploy/grafana/`
  (Dockerfile + provisioning datasources for Prometheus, Loki, Tempo; reuse existing dashboards).
- **Extra Check:** `find deploy -type f` lists the five configs + grafana provisioning; YAML parses
  (`python3 -c "import yaml,glob; [yaml.safe_load(open(f)) for f in glob.glob('deploy/**/*.yaml', recursive=True)]"`).
- **Commit:** `chore(deploy): add flat prometheus/otel/tempo/promtail/grafana configs`

### Step 4.2 — Replace the Grafana-only compose with a full observability stack

- Rename/replace `docker-compose-grafana.yaml` → `docker-compose.observability.yaml` adding `loki`, `promtail`,
  `tempo`, `otel-collector`; add healthchecks + `condition: service_healthy` ordering.
- Set app env: `LOG_OUTPUT=file` (shared `app_logs` volume), `TRACE_ENABLED=true`, `TRACE_EXPORTER=otlp-http`,
  `TRACE_ENDPOINT=otel-collector:4318`.
- **Extra Check:** `docker compose -f docker-compose.observability.yaml config` validates without error.
- **Commit:** `feat(compose): full observability stack (Prometheus+Loki+Tempo+OTel+Grafana)`

### Step 4.3 — Wire Make targets + smoke validation to the new stack

- `Makefile`: point `compose-up/-logs/-down/-smoke` at `docker-compose.observability.yaml` (use the two-step
  `--progress plain build` then `up -d` pattern from openapi-mock).
- Extend `scripts/validate-observability-stack.sh` to assert Loki readiness, Tempo readiness, and that a traced
  request surfaces in Loki + Tempo.
- Update README observability section.
- **Extra Check:** `bash -n scripts/validate-observability-stack.sh` parses; `make -n compose-up` references the
  observability file.
- **Commit:** `build(compose): target observability stack from make + extend smoke validation`

---

## Phase 5 — 🟡 Management surface parity (G5)

Goal: bring the mgmt server up to the openapi-mock contract. Split context-values store, its routes, then docs
discovery.

### Step 5.1 — Add an in-memory context-values store

- Create `pkg/mm/mm.go` (port from openapi-mock): per-request-id `map[string]any` store with
  `Get/GetAll/Replace/ReplaceAll/Merge/MergeAll/Delete/DeleteKeys/Clear` + `DecodeObject/DecodeStore`.
- Add `pkg/mm/mm_test.go`.
- **Extra Check:** `go test ./pkg/mm/...`
- **Commit:** `feat(mm): add per-request context-values store`

### Step 5.2 — Expose context-values routes + inject into request context

- `pkg/mgmt/server.go`: add `Options`-style constructor holding the store + reset; add the eight
  `/context-values[...]` routes (GET/PUT/PATCH/DELETE, collection + by-id) mirroring openapi-mock.
- `recordingInterceptor`: read the store by resolved request-id and merge values into the request context so stubs
  can consume pre-seeded values.
- Extend `pkg/mgmt/server_test.go`.
- **Extra Check:** `go test ./pkg/mgmt/...`
- **Commit:** `feat(mgmt): context-values endpoints and request-scoped injection`

### Step 5.3 — Per-service docs discovery endpoints

- Add `/docs` (index of registered gRPC services) and `/docs/{service}` (rendered proto/service summary or
  reflection-backed listing) to the mgmt server; keep `/doc` + `/openapi.json` for the mgmt API itself.
- Update README mgmt endpoint tables.
- **Extra Check:** `go test ./pkg/mgmt/...`; `curl :9000/docs` lists services.
- **Commit:** `feat(mgmt): service docs discovery endpoints`

---

## Phase 6 — 🟡 Streaming interceptor coverage

Goal: record/meter/log streaming RPCs, not just unary.

### Step 6.1 — Add a stream interceptor sharing the unary pipeline

- Factor the request-id/trace/log/record/metric logic into a shared helper; add
  `grpc.StreamInterceptor` wrapping `ServerStream` to capture method, duration, error/panic, and a record entry.
- Register it alongside the unary interceptor in `runServer`.
- Add a streaming proto fixture + interceptor test (`httptest`/bufconn).
- **Extra Check:** `go test ./...`; a streamed call appears in `/logs` and increments `grpc_requests_total`.
- **Commit:** `feat(server): record and meter streaming RPCs via stream interceptor`

---

## Phase 7 — 🟡 Metrics label hardening

Goal: align metric cardinality safety with openapi-mock.

### Step 7.1 — Add in-flight gauge + bounded error/panic `kind`

- `pkg/metrics/metrics.go`: add `grpc_requests_in_flight` gauge (inc/dec around handling); replace raw
  `error`/`panic` message labels with a bounded `kind` label (e.g. `error`, `panic`, `codes.X`), keeping the
  message in logs/records instead of metric labels.
- Update `pkg/metrics` tests + README metrics tables.
- **Extra Check:** `go test ./pkg/metrics/...`; `/metrics` shows the gauge and bounded labels.
- **Commit:** `feat(metrics): add in-flight gauge and bounded error/panic kind labels`

---

## Phase 8 — 🟡 Remove stale deprecated `/clear` routes

Goal: complete the `DELETE /logs` migration.

### Step 8.1 — Drop `POST/DELETE /clear`

- `pkg/mgmt/server.go`: remove the `/clear` routes + `handleClear`; ensure `DELETE /logs` and `POST /reset` cover
  all clearing semantics.
- Update README "Устаревшие эндпоинты" section + any scripts referencing `/clear`.
- **Extra Check:** `grep -rn "/clear" pkg/ scripts/ README.md` returns nothing; `go test ./pkg/mgmt/...` passes.
- **Commit:** `refactor(mgmt): remove deprecated /clear routes in favor of DELETE /logs`

---

## Phase 9 — 🟢 Recorder typing safety

Goal: store request/response as JSON instead of `any`.

### Step 9.1 — Store proto payloads as `json.RawMessage`

- `pkg/recorder/recorder.go`: change `Request`/`Response` to `json.RawMessage`; in the interceptor marshal proto
  messages with `protojson` (fallback to a string for non-proto), keeping field names stable.
- Update `pkg/recorder/recorder_test.go`.
- **Extra Check:** `go test ./pkg/recorder/...`; `/logs` payloads are valid JSON objects.
- **Commit:** `refactor(recorder): store request/response as json.RawMessage`

---

## Phase 10 — 🟢 Updater tests + UX

Goal: de-risk the stub generator.

### Step 10.1 — Split `cmd/upd-stubs` and add focused tests

- Ensure logical units (discovery, stub render, AST merge, wire) live in separate files; add table tests for
  signature migration, import-alias repair, method-append preservation, and wire generation.
- **Extra Check:** `go test ./cmd/upd-stubs/...`
- **Commit:** `test(upd-stubs): cover signature migration, alias repair, append, and wire`

### Step 10.2 — Add `--dry-run` / `--verbose` flags

- Add the flags; `--dry-run` prints intended changes without writing; `--verbose` logs per-file decisions.
- **Extra Check:** `go run ./cmd/upd-stubs --dry-run && git diff --exit-code internal/stubs`
  (no files written).
- **Commit:** `feat(upd-stubs): add --dry-run and --verbose flags`

---

## Phase 11 — 🟢 Use `go tool wire`

Goal: reproducible DI tooling.

### Step 11.1 — Pin wire via `tool` directive

- Add a `tool (...)` directive for `github.com/google/wire/cmd/wire` in `go.mod`; switch the `Makefile` `wire`
  target and the `Dockerfile` from `go run ...@latest` to `go tool wire`; `go mod tidy`.
- **Extra Check:** `make wire && git diff --exit-code internal/app/wire_gen.go` (stable regen).
- **Commit:** `build: invoke pinned wire via go tool directive`

---

## Phase 12 — 🟢 Repo hygiene

Goal: trivial cleanup noted in the report/AGENT notes.

### Step 12.1 — Ignore Python cache

- Add `scripts/__pycache__/` (and `*.pyc`) to `.gitignore`; remove the tracked cache directory if present.
- **Extra Check:** `git status --porcelain scripts/__pycache__` is empty after `git rm -r --cached` if needed.
- **Commit:** `chore: gitignore scripts/__pycache__`

---

## Completion criteria

- Every phase committed with all three DoD gates green.
- `make all && git diff --exit-code` clean (reproducible generation holds throughout).
- `go test ./...` passes on a fresh checkout.
- `make compose-smoke` validates metrics **and** logs (Loki) **and** traces (Tempo) end-to-end.
- A grpcurl call with inbound `x-request-id` + `traceparent` is visible, correlated, in `/logs`, Loki, and Tempo.

## Quick step index

| #    | Step                                | Band | Report gap | Commit gate (Extra Check)              |
|:-----|:------------------------------------|:-----|:-----------|:---------------------------------------|
| 1.1  | zerolog logger + context helpers    | 🔴   | G6         | `go test ./pkg/observability/...`      |
| 1.2  | interceptor → structured logs       | 🔴   | G6         | JSON lines with `request_id`           |
| 1.3  | stubs → context logger (+ template) | 🔴   | G6         | `make stub && git diff --exit-code`    |
| 1.4  | logging config env+flags            | 🔴   | G6         | flags honoured at runtime              |
| 2.1  | forward inbound request-id          | 🟡   | G2         | grpcurl `x-request-id` round-trips     |
| 3.1  | OTel tracing setup package          | 🔴   | G3         | `go test ./pkg/observability/...`      |
| 3.2  | spans + trace-id in logs            | 🔴   | G2/G3      | file exporter has inbound trace-id     |
| 3.3  | tracing config env+flags + docs     | 🔴   | G3         | README + `--help` list trace flags     |
| 4.1  | flat deploy configs                 | 🔴   | G7         | configs present + YAML parses          |
| 4.2  | full observability compose          | 🔴   | G7         | `compose config` validates             |
| 4.3  | make targets + smoke                | 🔴   | G7         | `make -n compose-up` + `bash -n` smoke |
| 5.1  | context-values store                | 🟡   | G5         | `go test ./pkg/mm/...`                 |
| 5.2  | context-values routes + injection   | 🟡   | G5         | `go test ./pkg/mgmt/...`               |
| 5.3  | service docs discovery              | 🟡   | G5         | `curl :9000/docs`                      |
| 6.1  | streaming interceptor               | 🟡   | —          | streamed call in `/logs` + metrics     |
| 7.1  | in-flight gauge + bounded kind      | 🟡   | G4         | `go test ./pkg/metrics/...`            |
| 8.1  | remove `/clear`                     | 🟡   | G5         | no `/clear` refs remain                |
| 9.1  | recorder `json.RawMessage`          | 🟢   | —          | `go test ./pkg/recorder/...`           |
| 10.1 | updater tests                       | 🟢   | —          | `go test ./cmd/upd-stubs/...`          |
| 10.2 | updater `--dry-run/--verbose`       | 🟢   | —          | dry-run writes nothing                 |
| 11.1 | `go tool wire`                      | 🟢   | —          | stable `wire_gen.go` regen             |
| 12.1 | gitignore pycache                   | 🟢   | —          | clean `git status` for cache           |
