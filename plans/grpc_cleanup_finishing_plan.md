# gRPC-Mock Cleanup Finishing Plan

This document captures the **remaining cleanup work** after the main convergence plan execution in `grpc-mock/`.
It intentionally focuses only on the gaps that are still partial or insufficiently verified.

The goal is to finish parity cleanly without reopening areas that are already complete.

## Remaining gaps

1. **Request-ID operator surface is incomplete**
    - `REQUEST_ID_HEADERS` / `REQUEST_ID_RESPONSE_HEADER` env vars exist.
    - Matching CLI flags are still missing.

2. **Streaming support lacks proof**
    - The stream interceptor exists.
    - Strong streaming-specific integration tests were not found.

3. **Metrics hardening is incomplete**
    - In-flight gauge name differs from the final contract.
    - Error/panic metric labels still derive from raw message fragments, which is unsafe for cardinality.

4. **Observability smoke validation is too shallow**
    - Compose wiring exists.
    - Validation does not yet prove a single correlated request flows through logs and traces.

5. **`upd-stubs` regression coverage is lighter than intended**
    - Code split is done.
    - `--dry-run` / `--verbose` are implemented.
    - Scenario-based regression tests still need strengthening.

6. **Minor cleanup remains**
    - Legacy `docker-compose-grafana.yaml` still exists.
    - Some documentation should be tightened to reflect final behavior.

---

## Workflow loop for every step

1. Implement one small unit of work.
2. Run the baseline verification gate:

```bash
gofmt -l .
go vet ./...
go test ./...
```

3. Run the step-specific extra check.
4. If all pass, commit immediately.

---

## Phase A — Finish request-id CLI parity

### Step A.1 — Add request-id CLI flags

Update `cmd/grpc-mock/main.go` to add:

- `--request-id-headers`
- `--request-id-response-header`

These must override:

- `REQUEST_ID_HEADERS`
- `REQUEST_ID_RESPONSE_HEADER`

#### DoD

- Flags appear in `go run ./cmd/grpc-mock run --help`
- Flags override env vars correctly
- Existing behavior remains backward compatible

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go run ./cmd/grpc-mock run --help | grep -E 'request-id-headers|request-id-response-header'
```

#### Commit

`feat(cli): add request-id header flags for correlation configuration`

---

### Step A.2 — Add a request-id round-trip integration test

Add a focused test that verifies:

- inbound metadata `x-request-id: abc`
- resolved request id becomes `abc`
- response metadata echoes the configured header
- recorder stores the same request id

Prefer an in-memory interceptor/server test over a manual smoke flow.

#### Suggested files

- `cmd/grpc-mock/main_test.go` or a new focused test file near the interceptor code

#### DoD

- Test fails without correct propagation
- Test passes locally and in the full suite

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./cmd/grpc-mock/... -run RequestID -v
```

#### Commit

`test(server): verify request-id metadata round-trip through interceptor and recorder`

---

## Phase B — Finish streaming coverage

### Step B.1 — Add a streaming proto fixture if needed

If current `protos/` do not expose a usable streaming RPC, add a minimal fixture service with:

- server-streaming or bidi-streaming method

If an existing streaming proto already exists, reuse it.

#### DoD

- Repo has a deterministic streaming RPC path suitable for tests

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
grep -REn 'stream ' protos internal/genproto | cat
```

#### Commit

`test(proto): add minimal streaming fixture for interceptor coverage`

---

### Step B.2 — Add stream interceptor integration tests

Add tests proving that a streamed call:

- gets a request id
- creates a recorder entry
- updates metrics
- logs success/error/panic behavior consistently
- preserves injected context values

Use `bufconn` or equivalent in-memory gRPC testing.

#### Suggested scope

- success stream
- handler error
- panic recovery
- seeded `context-values` visible in handler

#### DoD

- Stream interceptor is covered with real gRPC server calls
- Tests assert recorder state and metric counters/gauges

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./... -run Stream -v
```

#### Commit

`test(server): add end-to-end stream interceptor coverage`

---

## Phase C — Finish metrics hardening correctly

### Step C.1 — Replace message-derived error/panic labels with bounded kinds

Update `pkg/metrics/metrics.go` so labels use a strict bounded set, for example:

- `codes.InvalidArgument`
- `codes.Internal`
- `error`
- `panic`

Do not derive label values from arbitrary message text.

Prefer parsing gRPC status where possible:

- for errors: use `status.Code(err)`
- for panics: use fixed `panic`

This may require passing richer info from the interceptor to metrics helpers.

#### DoD

- No label value depends on raw free-form error text
- Logs and recorder still keep full error/panic message

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./pkg/metrics/... ./cmd/grpc-mock/... -v
```

#### Commit

`feat(metrics): use bounded error and panic kind labels`

---

### Step C.2 — Align in-flight metric name with final contract

Rename metric from:

- `grpc_in_flight`

to:

- `grpc_requests_in_flight`

Update docs and tests accordingly.

#### DoD

- Metric name matches the final contract
- `README.md` tables updated
- No stale references remain

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
grep -REn 'grpc_in_flight|grpc_requests_in_flight' . | cat
```

#### Commit

`refactor(metrics): align in-flight gauge name with observability contract`

---

### Step C.3 — Add metrics tests

Add `pkg/metrics/metrics_test.go` covering:

- request recording
- bounded error kind
- bounded panic kind
- in-flight inc/dec behavior
- exposed metric names

#### DoD

- `pkg/metrics` is covered by tests
- Tests lock in bounded-cardinality behavior

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./pkg/metrics/... -v
```

#### Commit

`test(metrics): cover bounded kind labels and in-flight gauge behavior`

---

## Phase D — Deepen observability smoke validation

### Step D.1 — Make smoke test send a real correlated gRPC request

Update `scripts/validate-observability-stack.sh` so it sends a real gRPC request with:

- `x-request-id`
- `traceparent`

Use fixed known values, for example:

- request id: `smoke-req-001`
- trace id embedded in a known `traceparent`

Prefer `grpcurl` when available; otherwise fail with a clear prerequisite message or use an existing client binary if
appropriate.

#### DoD

- Smoke test emits one deterministic correlated request

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
bash -n scripts/validate-observability-stack.sh
```

#### Commit

`test(observability): send deterministic traced and correlated gRPC smoke request`

---

### Step D.2 — Verify Loki contains the correlated request

Extend the smoke script to query Loki for:

- `request_id="smoke-req-001"` or equivalent JSON log content
- optionally `trace_id`

Avoid only checking that Loki returns any payload.

#### DoD

- Smoke fails if correlated logs are absent
- Validation is tied to the actual request sent in Step D.1

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
bash -n scripts/validate-observability-stack.sh
```

#### Commit

`test(observability): assert correlated request logs are queryable in Loki`

---

### Step D.3 — Verify Tempo contains the correlated trace

Extend the smoke script to query Tempo using the known trace id from the injected `traceparent`.

Fail if that trace is not found.

#### DoD

- Smoke fails if trace export path is broken
- Validation proves request → collector → Tempo path

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
bash scripts/validate-observability-stack.sh
```

#### Commit

`test(observability): assert traced smoke request is present in Tempo`

---

### Step D.4 — Verify recorder and response metadata correlation

Optionally extend smoke validation to assert:

- `/logs` contains the request id
- response metadata echoes the request id header

This closes the loop across app, recorder, logs, and traces.

#### DoD

- One request is visible in all intended surfaces

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
bash scripts/validate-observability-stack.sh
```

#### Commit

`test(observability): validate correlation across response metadata and recorder logs`

---

## Phase E — Strengthen `upd-stubs` regression protection

### Step E.1 — Add scenario fixtures for signature migration

Add test fixtures that simulate an existing stub file whose generated method signature changed.

Assert:

- signature updates
- existing method body is preserved
- imports remain valid

#### DoD

- Regression test reproduces real migration flow, not just helper behavior

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./cmd/upd-stubs/... -run Signature -v
```

#### Commit

`test(upd-stubs): add signature migration scenario coverage`

---

### Step E.2 — Add alias-repair scenario tests

Create fixtures with conflicting imports and confirm the generator:

- repairs aliases deterministically
- preserves compileable output

#### DoD

- Test checks generated file content, not only helper return values

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./cmd/upd-stubs/... -run Alias -v
```

#### Commit

`test(upd-stubs): add import alias repair regression coverage`

---

### Step E.3 — Add append-preservation tests

Test the case where:

- existing stub has custom methods/body
- new proto method is added
- generator appends only missing methods
- handwritten methods are preserved where expected

#### DoD

- Protects the main product promise of safe regeneration

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./cmd/upd-stubs/... -run Append -v
```

#### Commit

`test(upd-stubs): verify safe append without overwriting custom stub logic`

---

### Step E.4 — Add wire-generation scenario tests

Add tests that validate:

- service grouping
- generated wire file contents
- stable output across repeated runs

#### DoD

- `wire` generation behavior is locked by scenario tests

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go test ./cmd/upd-stubs/... -run Wire -v
```

#### Commit

`test(upd-stubs): add deterministic wire generation regression tests`

---

## Phase F — Final repo cleanup

### Step F.1 — Remove legacy `docker-compose-grafana.yaml`

The new observability stack already lives in `docker-compose.observability.yaml`.

Delete or archive the old compose file if it is no longer referenced.
Also scrub docs if they mention the old path.

#### DoD

- No operational ambiguity remains between old and new compose stacks
- No stale references remain unless explicitly marked legacy

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
grep -REn 'docker-compose-grafana.yaml' . | cat
```

#### Commit

`chore(compose): remove legacy grafana-only compose file`

---

### Step F.2 — Tighten docs to match actual final behavior

Update `README.md` and, if needed, `AGENT.md` / `UPD_STUBS.md` so they reflect:

- request-id flags
- final metric names
- bounded metric label semantics
- smoke-test prerequisites
- final observability guarantees

#### DoD

- Docs match CLI/help and actual runtime behavior
- No stale metric names or compose references remain

#### Extra Check

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
go run ./cmd/grpc-mock run --help | cat
grep -REn 'grpc_in_flight|docker-compose-grafana.yaml' README.md AGENT.md UPD_STUBS.md | cat
```

#### Commit

`docs: align observability and CLI docs with final cleanup state`

---

## Recommended execution order

1. **A.1** request-id flags
2. **A.2** request-id integration test
3. **B.1** streaming fixture if needed
4. **B.2** stream interceptor tests
5. **C.1** bounded metric kinds
6. **C.2** rename in-flight metric
7. **C.3** metrics tests
8. **D.1** deterministic correlated smoke request
9. **D.2** Loki assertion
10. **D.3** Tempo assertion
11. **D.4** recorder/metadata assertion
12. **E.1–E.4** `upd-stubs` scenario hardening
13. **F.1** remove legacy compose file
14. **F.2** docs cleanup

---

## Completion criteria

Cleanup is complete when all of the following pass:

```bash
cd /home/sr9000/Documents/dev/mocks/grpc-mock
gofmt -l .
go vet ./...
go test ./...
make -n compose-up
bash -n scripts/validate-observability-stack.sh
docker compose -f docker-compose.observability.yaml config
```

And functionally:

- `--request-id-headers` and `--request-id-response-header` exist
- streaming interceptor has real integration tests
- metrics use bounded labels only
- in-flight gauge name is finalized
- smoke validation proves one correlated request is visible in:
    - response metadata
    - recorder `/logs`
    - Loki
    - Tempo
- `upd-stubs` is protected by scenario tests
- old Grafana-only compose file is gone or explicitly retired
