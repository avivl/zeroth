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
- **plan**: draft, cross-exam, approve, apply. Consequential mutation happens only on apply. A plan is a typed set of resource rows (operation, target, payload, lease, precondition, idempotency key, postcondition) plus a canonical hash, expiry, cost ceiling, and the scope and credential classes it was drafted under (Z1-052). A revised plan is a new hash and needs a new approval. The builder turns harness proposed effects into those rows; an unexpressible effect stops the run rather than falling back to direct action. Cross-exam (Z1-019) is automatic after every draft. A reviewer on a different model reads the issue, the plan, and the diffs in a fresh context. It does not see the producer's transcript. The verdict is pass, fail, or pass_with_notes, with notes stored on the plan and signed on the audit chain. Dual review requires both models to pass. Same-model second pass is rejected. Optional block-on-fail returns a failed plan to the agent with the notes attached instead of escalating to the human. A plan cannot be approved until that exam has completed and the status is `pending_approval`. Pass rate per agent is queryable. Apply rechecks every precondition against the live workspace overlay and fails closed on drift (the plan is marked stale, nothing is written). An approval authorizes exactly one plan hash (`HashOf`). Rows run in order under the leases they name; each is idempotent by key. Create payloads are new file contents. Modify payloads are patches against the live overlay, never a silent full-file overwrite. Apply rechecks each row's postcondition hash against the bytes that landed and fails closed on mismatch before commit or push. Then the daemon commits a `zeroth/{issue}-{slug}` branch, pushes, and opens a GitHub pull request. The tracker completion comment carries that PR URL. A mid-apply failure leaves applied rows applied and records the boundary on `AppliedThrough`. Recovery drafts a new plan from observed postconditions rather than replaying blindly.
- **lease** — runtime mint/renew/expire for policy leases.
- **signer / audit**: attributable, append-only trail. Signatures are secp256k1 Schnorr, Nostr-compatible ([ADR-Z-0007](../adr/Z-0007-secp256k1-schnorr.md)). Each agent has a keypair at creation. Records chain through `prev_hash`. `zeroth verify <run-id>` checks the chain offline. Rotation appends a new pubkey; historical signatures stay verifiable.
- **secretscan** — a gate on apply and on sandbox export, not a linter the operator can skip.
- **memory**: a notebook of atomic facts with dates, provenance, and
  version history. Humans write directly. Agents only propose; a fact
  enters the notebook on human accept, and that rule is not configurable
  (Z1-022). Sandbox spawn compiles the relevant slice into `AGENTS.md`
  (and companion harness files) inside the overlay before the harness
  starts. Apply of a `memory_proposal` row calls `Notebook.Propose`; it
  does not write a fact. The compiled file is a build artifact, excluded
  from checkpoints and commits, never the source of truth (Z1-118).

## Ports

Each port is an interface in `internal/<name>` with one implementation in a subpackage and a `conformance_test.go` that the implementation must pass. The daemon depends on the interface.

| Port | Interface | Stage-1 implementation |
| --- | --- | --- |
| sandbox | `Driver` | `docker` |
| harness | `Driver` | `claudecode` |
| tracker | `Provider` | `linear` |
| store | `Store` | `sqlite` |

The store port covers sessions, events, plans, approvals, memory entries and proposals, audit records, leases, the checkpoint index, and agents. SQLite is one file in WAL mode. The path is configurable (`zerothd --db-path` / `ZEROTH_DB_PATH`). Schema changes go through Up and Down migrations; a migration without a Down is not done. The event log is append-only and is the source of truth for a session. `internal/store/conformance_test.go` is the contract a later Postgres driver must pass unchanged except for adding its table row (ADR-Z-0004, NFR-4).

The sandbox port is `Driver`: Spawn, Exec, ExportTar, ImportTar, AllowEgress, Kill, and Stop. Spawn starts with egress denied. A checkpoint is a workspace tar (content hash of paths, modes, and bytes, not tar mtimes), not a frozen process. Kill drops in-flight PIDs; the overlay remains until Stop. Credentials (Z1-113) are injected per Exec via env or a tmpfs (`/run/zeroth`), never into `/workspace`. ExportTar strips a hard exclusion list (CLI OAuth stores, `.git-credentials`,
shell history, token caches, and compiled memory artifacts regenerated at
hydration) and secret-scans the remaining tree, failing closed on a finding. The same tar hydrates any number of independent sandboxes (branch). Docker also exposes the overlay's host directory so the harness (a host subprocess) and the plan builder observe the same tree. `zerothd` copies the operator's local git checkout into that overlay at spawn. `internal/sandbox/conformance_test.go` is the contract a later backend must pass unchanged except for adding its table row (Z1-080, NFR-4). Docker is the stage-1 implementation ([ADR-Z-0005](../adr/Z-0005-docker-sandbox.md)).

The harness port is `Driver`: Start, Stream, Steer, Checkpoint, and Stop. Start launches a supervised agent subprocess against a workspace directory (the sandbox overlay). Stream yields tokens, tool calls, and structured proposed effects. Steer injects operator guidance mid-run (and restarts the subprocess if it has already exited). Checkpoint captures vendor session state so a run can resume. Stop kills the subprocess; an unexpected death is an `exited` event, not a hang. The agent proposes effects; it does not apply them (G4 prompt, `--permission-mode plan`, `--tools ""`). Anthropic auth is API-key only and the key never lands in the workspace ([ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md)). `zerothd` drives one harness turn per run: Stream effects become a draft, then automatic cross-exam. Draft observation hashes the overlay files the effects name. A modify or destroy whose target is missing fails with a workspace-observe error that includes the path and overlay directory, rather than an opaque plan-builder rejection. A turn that produces no plan fails the run instead of marking it completed. `internal/harness/conformance_test.go` is the contract a later adapter must pass unchanged except for adding its table row (Z1-006, NFR-4). Claude Code is the stage-1 implementation ([ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md)). A real `claude` subprocess (not the fake CLI) streamed tokens into the session event log on 2026-08-21: [LIVE_VERIFICATION.md](../harness/LIVE_VERIFICATION.md) (Linear 42-36).

The tracker port is `Provider`: GetIssue, Comment, SetState, Assignments, LinkArtifact, plus Name and Capabilities. Cycles and milestones are optional flags, not assumed methods. Assigning an issue to the Zeroth agent identity starts a headless run (Z1-038). Delegating that issue via Linear's native agent-delegation (a human stays the classic assignee) is the same trigger. Un-assigning or clearing the delegate cancels that run and must Kill then Stop the sandbox, not only mark a row. Polling is the stage-1 default so no inbound network path is required (Z1-082); a webhook secret is opt-in (`zerothd --linear-webhook-secret`). Linear GraphQL auth is a personal API key by default (raw `Authorization` value). OAuth application actor tokens use `zerothd --linear-auth-style oauth` / `ZEROTH_LINEAR_AUTH_STYLE=oauth` so the header is `Bearer <token>`. The style is an explicit flag, because guessing token format would show up as a 401 logged at error on every poll. On completion the driver comments cost, transcript, PR, and audit so someone reading only the tracker can reconstruct the run. Plan comments collapse diffs in `<details>`. `internal/tracker/conformance_test.go` is the contract a later GitHub Issues provider must pass unchanged except for adding its table row (Z1-071, ADR-Z-0006). Linear is the stage-1 implementation.

Stage 1 is local and single-player, so the store is SQLite and the daemon binds locally (`127.0.0.1:8420` by default, overridable with `ZEROTH_ADDR` or `zerothd --addr`). There is no remote control plane.

## Cross-cutting

- **Logging:** Zap via `internal/logging`. No package-level global. `internal/policy` does not log; callers pass facts in and may log the decision.
- **CLI and config:** Cobra command trees for `cmd/zeroth` and `cmd/zerothd`. Viper loads `zerothd` startup config (flags > `ZEROTH_*` env > yaml file > defaults).
- **Resilience:** Failsafe-go via `internal/resilience`. See [resilience.md](resilience.md). Drivers that talk to docker, a subprocess, or a tracker API reuse that pattern.

## Surfaces

- `cmd/zerothd` — long-running process (Cobra root, Viper config, Zap logs). Binds the OpenAPI HTTP surface (`internal/server`) on `127.0.0.1:8420` by default.
- `cmd/zeroth` — CLI and headless entry point (Cobra: `version`, `run`, `attach`, `bg`, `runs`, and `verify`). `verify` walks the signed audit chain against the SQLite file with the daemon stopped. Same OpenAPI contract as the web UI. `attach` is replay-then-live-tail over the run-events WebSocket; Ctrl-C detaches without stopping the run. Warm attach p50 versus the BA-6 G1 baseline (5.403ms) is recorded in [ATTACH_LATENCY.md](../cli/ATTACH_LATENCY.md) (Linear 42-38).
- `pkg/api` — OpenAPI spec (`openapi.yaml`); Go stubs and the TypeScript client are generated from it. Stage 1 surface: runs (steer, background, foreground, stop, events WebSocket, on-demand checkpoints), plans (list, approve, request-changes, branch, apply), agents (including read-only leases and cross-exam stats), approvals, memory (including proposals), audit verify, and checkpoint restore (forks a new run). Apply is `POST /plans/{id}/apply` after approve. Cross-exam is automatic and appears on the plan resource, not as an operator endpoint. Reviewer model (or dual) and block-on-fail are agent config. `GET /agents/{id}/cross-exam-stats` is the pass-rate query. A flow that cannot be expressed here does not ship on the web UI or the CLI.
- `web/` — Vite + React. Seven operator views (runs, run detail, agents, agent configuration, approvals, project memory, audit) call the generated TypeScript client. The visual language follows [Beautiful UI](https://www.beautifului.dev/) primitives (MIT). Live run events use a thin WebSocket wrapper around the generated `RunEvent` type and reconnect after a drop.

## Trust boundary

The harness is untrusted relative to the kernel. The sandbox is the isolation boundary for agent work. Policy decides what a session may do; the harness does not. Audit records are signed so a successful agent cannot quietly rewrite history.

Sandbox egress is deny by default. With no lease, the docker driver uses `--network none`. Per-destination allow is an HTTP/HTTPS CONNECT proxy whose allowlist is derived from active leases. A destination that is not listed is denied. The proxy is the enforcement point for leased egress: clients that ignore `HTTP_PROXY` are out of scope for stage 1. Closing that gap is a later network-namespace filter, not a second allowlist. The BA-6 spike measured this in [RESULTS.md](../spike/RESULTS.md) (Linear 42-8).

## Out of scope here

Deployment topology, multi-tenant isolation, and multiplayer session sharing. Those belong to stage 2 and are not designed in this document.
