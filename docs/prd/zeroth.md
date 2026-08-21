# Product requirements: Zeroth

Status: draft (stage 1)  
Companion docs: [architecture](../design/architecture.md), [plan](../design/plan.md)

## Why

Agents are fast, autonomous, and increasingly capable. None of that is allowed to override the governing constraint: a human stays in control of consequential actions. Zeroth exists to make that constraint real in software — not a prompt, not a policy PDF, but the kernel that every other package is subordinate to.

The name is a reminder. Asimov’s Three Laws were found incomplete; a Zeroth Law had to be added *above* them. This project is numbered the same way. Earned autonomy tiers, plan-then-apply, and signed audit trails are all in service of that one constraint.

## Who it is for (stage 1)

A single operator on their own machine who wants to run capable agents (starting with Claude Code) against real work (starting with Linear) without giving those agents unbounded authority.

There is no team, no tenant, no hosted account.

## Goals

1. **Human control of consequential actions.** Apply is a gated transition, not the default.
2. **Plan-then-apply.** Draft → cross-exam → approve → apply. The world changes only on apply.
3. **Earned autonomy.** Tiers exist so a session can be granted more scope over time; they do not skip the kernel.
4. **Signed, auditable actions.** What happened is reconstructable from the event log, not from chat residue.
5. **Local, single-player operation.** SQLite, a local daemon, a CLI, a local web UI.
6. **Public, MIT.** The repo is public from day one ([plan §6, decision 4](../design/plan.md); [ADR-Z-0002](../adr/Z-0002-mit-license.md)).

## Non-goals (stage 1)

- Deployment, hosting, HA, multi-region, or “Zeroth Cloud.”
- Multiplayer: shared sessions, orgs, RBAC across humans, remote control planes. That is stage 2.
- Being an agent. Zeroth drives a harness; it is not a model vendor.
- Supporting every tracker, sandbox, or IDE on day one. One implementation per port is enough to prove the port.

## Shape

| Piece | Role |
| --- | --- |
| `zerothd` | Local daemon: sessions, policy, plans, audit |
| `zeroth` | CLI and headless entry point |
| `web/` | Vite + React UI; [Beautiful UI](https://www.beautifului.dev/) primitives are the intended kit |
| Policy kernel | Scopes, grants, leases |
| Ports | Sandbox (Docker), harness (Claude Code), tracker (Linear), store (SQLite) |

## Decision register

This table is an index. The ADRs live in [`docs/adr/`](../adr/), one file per decision, [MADR format](../adr/README.md). Do not put the record back into this document.

| ADR | Decision | Status |
| --- | --- | --- |
| [ADR-Z-0001](../adr/Z-0001-go-typescript-isolated-kernel.md) | Core language Go, TypeScript for web; policy kernel isolated with typed IDs | Accepted |
| [ADR-Z-0002](../adr/Z-0002-mit-license.md) | License MIT | Accepted |
| [ADR-Z-0003](../adr/Z-0003-harness-driver-protocol.md) | Harness driver protocol (ACP or shim) | Accepted |
| [ADR-Z-0004](../adr/Z-0004-sqlite-first.md) | SQLite first, Postgres at stage 2, one store interface | Accepted |
| [ADR-Z-0005](../adr/Z-0005-docker-sandbox.md) | Docker sandbox driver is the reference implementation | Accepted |
| [ADR-Z-0006](../adr/Z-0006-linear-tracker.md) | Linear is the first tracker provider, GitHub Issues second | Accepted |
| [ADR-Z-0007](../adr/Z-0007-secp256k1-schnorr.md) | Signing scheme secp256k1 Schnorr, Nostr compatible | Accepted |
| [ADR-Z-0008](../adr/Z-0008-anthropic-api-key-auth.md) | Anthropic auth is API key only; per-provider auth matrix, quarterly review | Accepted |

Project-level choices that are not in this register (name, public from day one, stage 1 is local, plan-then-apply) stay in [plan §6](../design/plan.md).

## Acceptance for this skeleton

The repository tree exists, `go build ./...` succeeds, the license is MIT, and this README-facing PRD / design / plan trio is linked from the root README. Behavior comes later.
