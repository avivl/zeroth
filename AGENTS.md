# Agent guide

This file is canonical for every coding agent working in this repository: Cursor, Claude Code, and later Zeroth itself. It is also the prototype of compiled project memory (requirement Z1-118). Chat, comments, and other docs do not outrank it.

Read [README.md](README.md), [docs/prd/zeroth.md](docs/prd/zeroth.md), [docs/design/architecture.md](docs/design/architecture.md), and [docs/design/plan.md](docs/design/plan.md) before changing shape.

## What Zeroth is

Zeroth is a local control plane for AI agents: they work at machine speed, and a human keeps control of consequential actions. Autonomy is earned tier by tier, plans are proposed before they are applied, and every action is signed and auditable. The name is from Asimov: a Zeroth Law sits above the rest and bounds what they are allowed to do; in this repository that law is human control.

## What stage 1 is not

Stage 1 is local and single-player. One operator, one machine, `zerothd` plus `zeroth` plus a local web UI, SQLite, Docker, Claude Code, Linear.

Do not ship, design, or smuggle in:

- A deployment story, hosted product, cloud control plane, or shared cluster.
- Multiplayer: orgs, shared sessions, RBAC across humans, remote operators. That is stage 2.
- Zeroth-as-an-agent. This repo drives a harness; it is not a model vendor.
- A second implementation of a port "while we are here." Stage 1 is one implementation per port unless the issue says otherwise.

Empty `doc.go` packages are acceptable until that package has behavior. Do not invent APIs to look busy.

## Layout

| Path | Belongs here | Does not belong here |
| --- | --- | --- |
| `cmd/zerothd` | Local daemon process | Business rules, port implementations |
| `cmd/zeroth` | CLI and headless entry point | A second copy of the kernel |
| `internal/policy` | Scopes, grants, leases. The kernel. | I/O, persistence, HTTP, agent loops. Agents do not modify this package. |
| `internal/session` | Session state machine and event log | Tracker or harness I/O |
| `internal/plan` | Draft, cross-exam, approve, apply | Applying without a plan |
| `internal/lease` | Runtime mint, renew, expire of leases | Defining what a lease *is* (that is policy) |
| `internal/signer` | Signing actions and audit records | Key-management product design |
| `internal/audit` | Append-only signed trail | Rewritable logs, chat residue as source of truth |
| `internal/secretscan` | Gate on apply for leaked secrets | A skippable linter |
| `internal/memory` | Session and agent memory (store-backed in stage 1) | Committed generated memory files |
| `internal/sandbox` | `Driver` port plus `conformance_test.go` | Concrete runtimes (those go in subpackages) |
| `internal/sandbox/docker` | Stage-1 `Driver` implementation | Changes to the port that are not reflected in conformance tests |
| `internal/harness` | `Driver` port plus `conformance_test.go` | Vendor SDKs |
| `internal/harness/claudecode` | Stage-1 harness implementation | Shortcuts that bypass plan-then-apply |
| `internal/tracker` | `Provider` port plus `conformance_test.go` | Vendor SDKs |
| `internal/tracker/linear` | Stage-1 tracker implementation | Kernel policy |
| `internal/store` | `Store` port plus `conformance_test.go` | Cloud databases in stage 1 |
| `internal/store/sqlite` | Stage-1 store implementation | Multi-tenant schema |
| `pkg/api` | `openapi.yaml` (canonical) and generated clients under `gen/` | Hand-written stubs or TS clients |
| `web/` | Vite + React UI for the local daemon | Apply paths that skip the kernel |
| `docs/adr` | `Z-NNNN-slug.md` decisions | Hidden design that never becomes an ADR |
| `docs/prd` `docs/design` `docs/spike` | Specs and time-boxed investigations | Long-lived subsystems promoted from a spike |
| `evals/` | Offline evaluations | CI unit tests |

`internal/` is not importable from other modules. That is deliberate. Policy outranks the harness. The daemon depends on port interfaces, not on Docker, Claude Code, Linear, or SQLite by name.

## The kernel rule

`internal/policy` is the kernel. Everything else is a First Law.

- **Deny by default.** Missing grant, expired lease, or unknown scope is a deny. Do not add implicit allow, "fail open," or a debug backdoor.
- **No I/O.** Policy does not read disk, network, environment, or the store. Callers pass facts in; policy returns a decision. Persistence is `internal/store`. Lease runtime is `internal/lease`.
- **Distinct named ID types.** `ScopeID`, `GrantID`, `LeaseID`, `SessionID`, and the rest are different types. Do not use `string` or `int64` where an ID is required. Do not make the types aliases of each other.
- **Human review on every change.** Agents do not modify `internal/policy`. If an issue seems to need a policy change, stop and say so. A human writes or reviews the patch.

A path that applies a consequential action without the plan lifecycle (draft, cross-exam, approve, apply) is a defect. Autonomy tiers change how much a session may do, not whether a plan exists.

## Interfaces before implementations

Ports are interfaces. Implementations live in subpackages. The daemon wires the interface, never the concrete type.

A driver or provider is **not done** without a conformance test in the **same PR**. Do not file a follow-up for tests.

### Recipe: add an implementation of an existing port

Use the existing stubs as the template (`internal/sandbox/docker`, `internal/harness/claudecode`, `internal/tracker/linear`, `internal/store/sqlite`).

1. Create `internal/<port>/<impl>/` with `doc.go` and the type.
2. Export `New()`, `Name() string` (stable, used in logs, audit, and tests), and any port methods.
3. Compile-time check: `var _ port.Driver = (*Driver)(nil)` (or `Provider` / `Store`).
4. Add a row to the table in `internal/<port>/conformance_test.go`. The table `name` field must equal `Name()`.
5. Keep the conformance file as `package <port>_test` so it only sees the exported port.
6. If you extended the interface, extend the conformance suite in this PR so every implementation is forced through the new behavior.
7. Update [docs/design/architecture.md](docs/design/architecture.md) if the stage-1 implementation list changed.

### Recipe: add a new port

1. Interface and `doc.go` in `internal/<port>/`.
2. One implementation in a subpackage.
3. `conformance_test.go` next to the interface, table-driven, covering that implementation.
4. Daemon depends on the interface.
5. Architecture, plan, and this file updated in the same PR.

Generate `pkg/api` clients from `openapi.yaml`. Do not hand-write `pkg/api/gen/`.

## Testing

```bash
go test -race ./...
go vet ./...
go build ./...
```

`task ci` is a local stand-in if [Task](https://taskfile.dev) is installed. It runs lint, `go test -race ./...`, conformance, secretscan, the web tests and build, and `go build ./...`. `task --list` shows the other targets (`up`, `lint`, `conformance`, `generate`, `web`, `secretscan`). GitHub Actions CI lives in `.github/workflows/ci.yml`. Path filtering skips Go on `web/`-only PRs and web on `internal/`-only PRs. `pkg/api/` changes run everything.

- Prefer table tests. The port `conformance_test.go` files are the pattern: a slice of cases, `t.Run`, `t.Parallel()`.
- Kernel packages (`policy`, and the plan/session/lease invariants once they have behavior) get property tests (`testing/quick` in the standard library, or an equivalent). Properties that must hold: deny by default, leases cannot outlive their window, named ID types are not interchangeable.
- Do not add a test dependency until the package under test has behavior that needs it.
- `evals/` is not `go test` and not CI.

Go 1.27. Do not add a module dependency unless the package that needs it is being implemented.

## Style

- **No em dashes** (Unicode U+2014) anywhere in docs or comments. Use a period, comma, colon, or parentheses.
- Wrap errors with context: `fmt.Errorf("sandbox docker start: %w", err)`. Do not return a bare `err` across a package boundary.
- No naked `interface{}` and no `any` used as a type-erasing bag. Prefer a concrete type, a generic, or a named interface with methods.
- Comments say *why*. Package docs live in `doc.go`.

## PR conventions

- Small, one concern. Do not bundle a kernel change with a UI tweak.
- Docs for the change land in the same PR: `doc.go`, architecture/PRD/plan if shape changed, this file if a convention changed.
- Link the Linear issue.
- Squash merge.
- ADRs are `docs/adr/Z-NNNN-slug.md`. License is MIT ([ADR-Z-0002](docs/adr/Z-0002-mit-license.md)). Repo is public ([ADR-Z-0001](docs/adr/Z-0001-public-from-day-one.md)).

## What not to do

- **Do not modify `internal/policy`.** Human review is required; agents do not patch the kernel.
- **Do not commit generated memory files.** `AGENTS.md` is committed source. Compiled or dumped memory (the Z1-118 output, session transcripts, store exports) stays out of git.
- **Do not write credentials into a workspace.** No tokens in fixtures, `.env` committed, OpenAPI examples, or docs. `.env` is gitignored; that is not permission to leave a secret on disk.
- **Do not widen an egress allowlist to make a test pass.** Use a fake, a fixture, or skip. Do not add a domain to CI, the sandbox, or the agent environment because a test wanted the network.
- **Do not apply without a plan.** No harness shortcut, UI button, or CLI flag that mutates the world outside draft, cross-exam, approve, apply.
- **Do not hand-write generated API clients.**
- **Do not add a CODEOWNERS exception for agent-authored PRs.** Human review is required on `internal/policy/`, `internal/plan/`, and `pkg/api/`.
