# AGENT notes for `grpc-mock`

## Contracts

This repo implements the contracts defined in `../CONTRACTS.md` (workspace root). See that file for the unified CLI,
Management API, Recorder JSON, Metrics, Make target, Stub Updater, and Docs layout contracts.

## Snapshot

`grpc-mock` already has a solid core architecture:

- contract-first generation from `.proto`,
- generated code isolated in `internal/genproto/`,
- hand-written logic isolated in `internal/stubs/`,
- Wire-based registration,
- management API for logs/docs,
- Prometheus metrics server,
- Docker dev environment with manual restart.

## Strengths worth preserving

1. **Simple pipeline mental model**
    - `update-proto-pkg -> proto -> stub -> wire -> build`
2. **Good local/dev feedback loop**
    - `scripts/run-dev.sh` builds and runs the server; manual restart for changes
3. **Transport-native usefulness**
    - reflection support is valuable for gRPC tooling
4. **Safe-enough stub patching behavior**
    - updater preserves existing method bodies on signature drift

## Parity status versus `openapi-mock` (now largely reached)

Most historical gaps are closed. Verified in code:

1. **Management API is at parity** — `GET /logs/{request_id}`, `POST /reset`, full `context-values` surface, and docs
   discovery (`/docs`, `/docs/{service}`) are implemented in `pkg/mgmt/server.go`.
2. **Observability stack is full** — Prometheus + Loki + Tempo + OTel Collector + Grafana via
   `docker-compose.observability.yaml`, with repo-owned `make compose-smoke`.
3. **Stub updater is modular and tested** — `cmd/upd-stubs/` is split into files and covered by
   `upd_stubs_test.go`; `--dry-run`, `--verbose`, and `--prune` flags exist (see `UPD_STUBS.md`).
4. **Structured logging** — access logs flow through interceptors + a contextual logger (`pkg/observability`).
5. **`/clear` routes removed**; `scripts/__pycache__/` is already gitignored.

## Remaining friction (open)

These are the genuinely-open items; the full cross-repo list lives in `../CONTRACTS.md` §8.

1. **`make run` is broken** — it runs `go run ./cmd/grpc-mock` without the `run` subcommand, so it prints help
   instead of starting the server. Should be `go run ./cmd/grpc-mock run` (matches `openapi-mock`).
2. **`.env.example` uses `GRPC_PORT`**, but the server reads `PORT`; `GRPC_PORT` only maps the host port in the
   observability compose. Add `PORT`/`HOST` (and a comment) to avoid confusion.
3. **No `.env` auto-wiring in the Makefile** — `openapi-mock`'s `compose-*`/`docker-dev` targets pass `--env-file`;
   `grpc-mock` does not.
4. **README flag table** omits `--request-id-headers` / `--request-id-response-header`, though both flags exist.

## Suggested role in the unified direction

Let `grpc-mock` be the reference implementation for:

- fast dev loop,
- minimal conceptual workflow,
- transport-specific gRPC ergonomics.

But let it adopt the stronger operator/test/management patterns proven in `openapi-mock`.
