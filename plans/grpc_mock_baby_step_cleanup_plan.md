# grpc-mock Baby-Step Cleanup Plan

**Date:** 2026-06-05
**Source proposal:** `../../COMPARISON_REPORT_CURRENT.md`
**Scope:** Finish the remaining `grpc-mock` gaps called out by the current comparison report.

## Operating Workflow

Use this loop for every step below:

1. Implement one small, logical unit of work only.
2. Run the step's DoD Verification in this order:
   1. **Lint**
   2. **Test**
   3. **Extra Check**
3. If verification fails:
   1. Fix locally.
   2. Re-run the full DoD Verification for that same step.
   3. Do **not** commit until all checks pass.
4. If verification passes:
   1. Commit immediately with the step's proposed commit message.
   2. Move to the next step without pausing.

Do not batch unrelated changes into one step. If a step reveals a prerequisite refactor, commit the prerequisite as its own verified baby step first.

## Baseline Preflight

Run once before starting implementation:

```bash
git status --short
go test ./...
bash -n scripts/*.sh
docker compose -f docker-compose.yaml config >/tmp/grpc-mock-compose.yaml
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
```

Expected result: clean or intentionally understood Git state, tests pass, scripts parse, and compose files parse.

No commit is required for preflight.

## Default DoD Commands

Use these defaults unless a step overrides or extends them.

### Lint

```bash
gofmt -w <touched-go-files>
go vet ./...
```

For shell or compose/documentation-only steps, use the relevant lint checks listed in the step.

### Test

```bash
go test ./...
```

### Commit

After the step-specific extra check passes:

```bash
git status --short
git add <touched-files>
git commit -m "<step commit message>"
```

Before committing, inspect `git status --short` and ensure only files from the current step are staged.

---

## Step 1 — Add request-id CLI flags

**Goal:** Close the env-only gap for request-id configuration by adding CLI parity for:

- `--request-id-headers`
- `--request-id-response-header`

**Implementation notes:**

- Register both flags in `cmd/grpc-mock/main.go` beside the existing observability/config flags.
- Apply flag overrides only when the flag was explicitly changed, preserving the existing env → flags → positional precedence model.
- Keep the existing env vars as-is:
  - `REQUEST_ID_HEADERS`
  - `REQUEST_ID_RESPONSE_HEADER`

**DoD Verification:**

Lint:

```bash
gofmt -w cmd/grpc-mock/main.go
go vet ./...
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
go run ./cmd/grpc-mock --help | grep -E -- '--request-id-headers|--request-id-response-header'
```

**Commit message:**

```text
feat(grpc-mock): add request id configuration flags
```

---

## Step 2 — Bound gRPC error and panic metric kinds

**Goal:** Replace unbounded raw-message-derived `kind` labels with bounded values.

**Implementation notes:**

- Update `pkg/metrics/metrics.go` so error kinds are derived from `status.Code(err).String()`.
- Use a small bounded panic kind such as `panic`, `runtime_error`, or another fixed enum; do not use panic text as a label.
- Preserve existing metric names in this step. Gauge renaming is handled separately.
- Add or update focused tests if metrics helper functions are currently testable or can be made testable with minimal churn.

**DoD Verification:**

Lint:

```bash
gofmt -w pkg/metrics/metrics.go pkg/metrics/*_test.go
go vet ./...
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
go test ./pkg/metrics -run 'Test.*Kind|Test.*Metrics' -count=1
```

If no focused tests exist yet, add them in this step before committing.

**Commit message:**

```text
fix(metrics): bound grpc error and panic label values
```

---

## Step 3 — Rename the in-flight metric to the unified contract name

**Goal:** Expose `grpc_requests_in_flight` instead of `grpc_in_flight`.

**Implementation notes:**

- Rename the metric in `pkg/metrics/metrics.go` to `grpc_requests_in_flight`.
- Decide deliberately whether to keep the current `method` label or simplify to a single gauge. Prefer the unified contract unless dashboards/scripts rely on per-method labels.
- Update all references in docs, dashboards, tests, and smoke scripts.
- Search before editing:

```bash
grep -R "grpc_in_flight\|grpc_requests_in_flight" -n . --exclude-dir=.git --exclude-dir=bin
```

**DoD Verification:**

Lint:

```bash
gofmt -w pkg/metrics/metrics.go pkg/metrics/*_test.go
go vet ./...
bash -n scripts/*.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
grep -R "grpc_in_flight" -n . --exclude-dir=.git --exclude-dir=bin && exit 1 || true
grep -R "grpc_requests_in_flight" -n pkg deploy scripts README.md AGENT.md
```

**Commit message:**

```text
fix(metrics): rename grpc in-flight gauge
```

---

## Step 4 — Wire full management reset semantics

**Goal:** Make `POST /reset` clear all mutable test-control state, not only recorded logs.

**Implementation notes:**

- Inspect `pkg/mgmt` reset options and the `openapi-mock` reset wiring for parity.
- In `cmd/grpc-mock/main.go`, pass the reset hook/option that clears context-values and any other mutable state owned by the management server.
- Keep recorder clearing behavior intact.

**DoD Verification:**

Lint:

```bash
gofmt -w cmd/grpc-mock/main.go pkg/mgmt/*.go pkg/mgmt/*_test.go
go vet ./...
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
go test ./pkg/mgmt -run 'Test.*Reset|Test.*Context' -count=1
```

If `pkg/mgmt` does not already cover reset + context-values together, add the missing test in this step.

**Commit message:**

```text
fix(mgmt): reset grpc context values with recorder state
```

---

## Step 5 — Make observability smoke send a deterministic correlated request

**Goal:** Ensure the smoke path always creates a known request id and trace context that later checks can query.

**Implementation notes:**

- Update `scripts/validate-observability-stack.sh` to generate a deterministic per-run request id, for example `smoke-$(date +%s)-$$`.
- Send that request id through gRPC metadata.
- Send a valid W3C `traceparent` value with a known trace id.
- Treat `grpcurl` as a required dependency for this smoke test, or provide an explicit fallback that still proves a request reached the mock. Do not silently skip the correlated request.
- Print the request id and trace id in the smoke output for debugging.

**DoD Verification:**

Lint:

```bash
bash -n scripts/validate-observability-stack.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
```

If Docker is available in the environment, also run:

```bash
make compose-smoke
```

**Commit message:**

```text
test(observability): send correlated grpc smoke request
```

---

## Step 6 — Assert the correlated request is queryable in Loki

**Goal:** Strengthen the smoke test from generic Loki payload checks to proof that the known smoke request reached log collection.

**Implementation notes:**

- Query Loki for the exact smoke request id generated in Step 5.
- Fail if Loki returns no matching log line within the script's retry window.
- Keep the retry loop bounded and diagnostics useful.

**DoD Verification:**

Lint:

```bash
bash -n scripts/validate-observability-stack.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
```

If Docker is available:

```bash
make compose-smoke
```

**Commit message:**

```text
test(observability): assert grpc request logs in loki
```

---

## Step 7 — Assert trace export for the correlated smoke request

**Goal:** Prove the smoke request exports trace data, not just logs and generic collector metrics.

**Implementation notes:**

- Prefer an exact check for the known trace id in Tempo if the stack exposes a reliable query endpoint.
- If exact Tempo trace lookup is not reliable in the local stack, assert `otelcol_receiver_accepted_spans > 0` after the correlated request and document the limitation in script comments.
- Keep the request id and trace id printed on failure.

**DoD Verification:**

Lint:

```bash
bash -n scripts/validate-observability-stack.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
```

If Docker is available:

```bash
make compose-smoke
```

**Commit message:**

```text
test(observability): verify grpc trace export in smoke test
```

---

## Step 8 — Remove the legacy Grafana compose file

**Goal:** Remove the stale legacy compose entrypoint `docker-compose-grafana.yaml`.

**Implementation notes:**

- Delete `docker-compose-grafana.yaml`.
- Remove any references to it from docs or scripts.
- Do not touch the active files:
  - `docker-compose.yaml`
  - `docker-compose.observability.yaml`
  - `deploy/prometheus.yaml`
  - `deploy/grafana/`

**DoD Verification:**

Lint:

```bash
bash -n scripts/*.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.yaml config >/tmp/grpc-mock-compose.yaml
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
grep -R "docker-compose-grafana.yaml" -n . --exclude-dir=.git && exit 1 || true
```

**Commit message:**

```text
chore: remove legacy grafana compose file
```

---

## Step 9 — Remove stale root Prometheus config

**Goal:** Remove the obsolete root `prometheus.yaml`; the active observability stack uses `deploy/prometheus.yaml`.

**Implementation notes:**

- Delete root-level `prometheus.yaml`.
- Remove or update references to root `prometheus.yaml`.
- Ensure `docker-compose.observability.yaml` still points to `deploy/prometheus.yaml`.

**DoD Verification:**

Lint:

```bash
bash -n scripts/*.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
test ! -e prometheus.yaml
grep -R "./prometheus.yaml\|prometheus.yaml" -n . --exclude-dir=.git --exclude-dir=deploy && exit 1 || true
```

If this grep finds legitimate references to `deploy/prometheus.yaml`, update the command or inspect manually before committing.

**Commit message:**

```text
chore: remove stale root prometheus config
```

---

## Step 10 — Remove the duplicate top-level Grafana directory

**Goal:** Remove the stale top-level `grafana/` tree in favor of `deploy/grafana/`.

**Implementation notes:**

- Delete top-level `grafana/`.
- Remove or update references to the deleted path.
- Ensure active compose and docs reference `deploy/grafana/`.

**DoD Verification:**

Lint:

```bash
bash -n scripts/*.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
test ! -e grafana
grep -R "grafana/" -n README.md AGENT.md scripts deploy docker-compose*.yaml --exclude-dir=.git
```

Inspect grep output and confirm only valid `deploy/grafana/` references remain.

**Commit message:**

```text
chore: remove duplicate grafana assets
```

---

## Step 11 — Remove the orphan `openapi-mock.md` planning document

**Goal:** Remove the obsolete planning document that describes old `grpc-mock` structure and stale legacy artifacts.

**Implementation notes:**

- Delete `openapi-mock.md` from the `grpc-mock` root.
- Remove references if any exist.
- Do not change the sibling repository `../openapi-mock/`.

**DoD Verification:**

Lint:

```bash
bash -n scripts/*.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
test ! -e openapi-mock.md
grep -R "openapi-mock.md" -n . --exclude-dir=.git && exit 1 || true
```

**Commit message:**

```text
chore: remove obsolete openapi planning document
```

---

## Step 12 — Final repository audit

**Goal:** Confirm the finished sequence did not leave stale references or broken validation gates.

**Implementation notes:**

- Run the full audit after all prior commits.
- Fix any failures as separate baby-step commits using the same workflow.

**DoD Verification:**

Lint:

```bash
gofmt -w $(git ls-files '*.go')
go vet ./...
bash -n scripts/*.sh
```

Test:

```bash
go test ./...
```

Extra Check:

```bash
docker compose -f docker-compose.yaml config >/tmp/grpc-mock-compose.yaml
docker compose -f docker-compose.observability.yaml config >/tmp/grpc-mock-observability-compose.yaml
git grep -n "docker-compose-grafana.yaml\|grpc_in_flight\|openapi-mock.md" -- . ':!plans/grpc_mock_baby_step_cleanup_plan.md' && exit 1 || true
git status --short
```

If Docker is available and the environment is suitable for a longer run:

```bash
make compose-smoke
```

**Commit message if fixes are needed:**

```text
chore: finish grpc mock cleanup audit
```

If no fixes are needed, no commit is required.

## Completion Criteria

The plan is complete when:

- Request-id configuration has env and CLI parity.
- Error and panic metric labels use bounded values.
- The in-flight gauge uses `grpc_requests_in_flight`.
- `POST /reset` clears recorder state and context-values.
- Observability smoke proves a known request id reaches Loki.
- Observability smoke proves trace export for the correlated request, preferably by trace id.
- Legacy observability artifacts are removed:
  - `docker-compose-grafana.yaml`
  - root `prometheus.yaml`
  - top-level `grafana/`
- Obsolete `openapi-mock.md` is removed.
- `go test ./...`, shell syntax checks, and active compose config checks pass.
