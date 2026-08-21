# Architecture

Status: draft (stage 1)  
Companion docs: [PRD](../prd/zeroth.md), [plan](plan.md)

Zeroth is a local control plane. The daemon holds the kernel. Everything that talks to the outside world does so through a port.

```
                 ┌──────────────┐     ┌──────────────┐
                 │  web (Vite)  │     │ cmd/zeroth   │
                 └──────┬───────┘     └──────┬───────┘
                        │  pkg/api           │
                        └─────────┬──────────┘
                                  ▼
                           cmd/zerothd
                                  │
        ┌──────────── policy · session · plan · lease ────────────┐
        │  memory    audit ← signer    secretscan                 │
        └────────────┬──────────┬──────────┬──────────┬───────────┘
                     ▼          ▼          ▼          ▼
                 sandbox     harness    tracker     store
                  docker    claudecode   linear     sqlite
```

## Kernel

- **policy** — scopes, grants, leases. Outranks every other package.
- **session** — state machine plus event log for one human-supervised run. Lifecycle is `pending → running → awaiting-approval → applying → done | failed`. Attachment (`attached` / `background`) is orthogonal. The event log is the source of truth; status is a replay of it. Attach is replay-then-live-tail of that log, so promotion and demotion only add or remove listeners. Illegal transitions are errors. A supervisor goroutine per live session serializes mutations. A demoted session carries a completion contract (finish, comment on the issue, ping only on blockers). Persistence of the log is the store port; this package talks to a `Log`.
- **plan** — draft, cross-exam, approve, apply. Consequential mutation happens only on apply. A plan is a typed set of resource rows (operation, target, payload, lease, precondition, idempotency key, postcondition) plus a canonical hash, expiry, cost ceiling, and the scope and credential classes it was drafted under (Z1-052). A revised plan is a new hash and needs a new approval. The builder turns harness proposed effects into those rows; an unexpressible effect stops the run rather than falling back to direct action.
- **lease** — runtime mint/renew/expire for policy leases.
- **signer / audit**: attributable, append-only trail. Signatures are secp256k1 Schnorr, Nostr-compatible ([ADR-Z-0007](../adr/Z-0007-secp256k1-schnorr.md)).
- **secretscan** — a gate on apply and on sandbox export, not a linter the operator can skip.
- **memory** — session and agent memory, store-backed in stage 1.

## Ports

Each port is an interface in `internal/<name>` with one implementation in a subpackage and a `conformance_test.go` that the implementation must pass. The daemon depends on the interface.

| Port | Interface | Stage-1 implementation |
| --- | --- | --- |
| sandbox | `Driver` | `docker` |
| harness | `Driver` | `claudecode` |
| tracker | `Provider` | `linear` |
| store | `Store` | `sqlite` |

The store port covers sessions, events, plans, approvals, memory entries and proposals, audit records, leases, the checkpoint index, and agents. SQLite is one file in WAL mode. The path is configurable (`zerothd --db-path` / `ZEROTH_DB_PATH`). Schema changes go through Up and Down migrations; a migration without a Down is not done. The event log is append-only and is the source of truth for a session. `internal/store/conformance_test.go` is the contract a later Postgres driver must pass unchanged except for adding its table row (ADR-Z-0004, NFR-4).

The sandbox port is `Driver`: Spawn, Exec, ExportTar, ImportTar, AllowEgress, Kill, and Stop. Spawn starts with egress denied. A checkpoint is a workspace tar (content hash of paths, modes, and bytes, not tar mtimes), not a frozen process. Kill drops in-flight PIDs; the overlay remains until Stop. Credentials (Z1-113) are injected per Exec via env or a tmpfs (`/run/zeroth`), never into `/workspace`. ExportTar strips a hard exclusion list (CLI OAuth stores, `.git-credentials`, shell history, token caches) and secret-scans the remaining tree, failing closed on a finding. The same tar hydrates any number of independent sandboxes (branch). `internal/sandbox/conformance_test.go` is the contract a later backend must pass unchanged except for adding its table row (Z1-080, NFR-4). Docker is the stage-1 implementation ([ADR-Z-0005](../adr/Z-0005-docker-sandbox.md)).

The harness port is `Driver`: Start, Stream, Steer, Checkpoint, and Stop. Start launches a supervised agent subprocess against a workspace directory (the sandbox overlay). Stream yields tokens, tool calls, and structured proposed effects. Steer injects operator guidance mid-run (and restarts the subprocess if it has already exited). Checkpoint captures vendor session state so a run can resume. Stop kills the subprocess; an unexpected death is an `exited` event, not a hang. The agent proposes effects; it does not apply them (G4 prompt, `--permission-mode plan`, `--tools ""`). Anthropic auth is API-key only and the key never lands in the workspace ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)). `internal/harness/conformance_test.go` is the contract a later adapter must pass unchanged except for adding its table row (Z1-006, NFR-4). Claude Code is the stage-1 implementation ([ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md)). A real `claude` subprocess (not the fake CLI) streamed tokens into the session event log on 2026-08-21: [LIVE_VERIFICATION.md](../harness/LIVE_VERIFICATION.md) (Linear 42-36).

Stage 1 is local and single-player, so the store is SQLite and the daemon binds locally (`127.0.0.1:8420` by default, overridable with `ZEROTH_ADDR` or `zerothd --addr`). There is no remote control plane.

## Cross-cutting

- **Logging:** Zap via `internal/logging`. No package-level global. `internal/policy` does not log; callers pass facts in and may log the decision.
- **CLI and config:** Cobra command trees for `cmd/zeroth` and `cmd/zerothd`. Viper loads `zerothd` startup config (flags > `ZEROTH_*` env > yaml file > defaults).
- **Resilience:** Failsafe-go via `internal/resilience`. See [resilience.md](resilience.md). Drivers that talk to docker, a subprocess, or a tracker API reuse that pattern.

## Surfaces

- `cmd/zerothd` — long-running process (Cobra root, Viper config, Zap logs). Binds the OpenAPI HTTP surface (`internal/server`) on `127.0.0.1:8420` by default.
- `cmd/zeroth` — CLI and headless entry point (Cobra: `version`, `run`, `attach`, `bg`, `runs`, and a `verify` stub). Same OpenAPI contract as the web UI. `attach` is replay-then-live-tail over the run-events WebSocket; Ctrl-C detaches without stopping the run.
- `pkg/api` — OpenAPI spec (`openapi.yaml`); Go stubs and the TypeScript client are generated from it. Stage 1 surface: runs (steer, background, foreground, stop, events WebSocket, on-demand checkpoints), plans (list, approve, request-changes, branch, apply), agents (including read-only leases), approvals, memory (including proposals), audit verify, and checkpoint restore (forks a new run). Apply is `POST /plans/{id}/apply` after approve. Cross-exam is automatic and appears on the plan resource, not as an operator endpoint. A flow that cannot be expressed here does not ship on the web UI or the CLI.
- `web/` — Vite + React. Intended to use [Beautiful UI](https://www.beautifului.dev/) primitives (MIT). The generated TypeScript client is HTTP; live run events use a thin WebSocket wrapper around the generated `RunEvent` type.

## Trust boundary

The harness is untrusted relative to the kernel. The sandbox is the isolation boundary for agent work. Policy decides what a session may do; the harness does not. Audit records are signed so a successful agent cannot quietly rewrite history.

Sandbox egress is deny by default. With no lease, the docker driver uses `--network none`. Per-destination allow is an HTTP/HTTPS CONNECT proxy whose allowlist is derived from active leases. A destination that is not listed is denied. The proxy is the enforcement point for leased egress: clients that ignore `HTTP_PROXY` are out of scope for stage 1. Closing that gap is a later network-namespace filter, not a second allowlist. The BA-6 spike measured this in [RESULTS.md](../spike/RESULTS.md) (Linear 42-8).

## Out of scope here

Deployment topology, multi-tenant isolation, and multiplayer session sharing. Those belong to stage 2 and are not designed in this document.
