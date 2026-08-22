# pkg/api

`openapi.yaml` is the contract. Stage 1 is local and single-player. A flow that
cannot be expressed through this API ships on neither the web UI nor the CLI.

This surface is human-owned ([CODEOWNERS](../../.github/CODEOWNERS) on
`pkg/api/`, Linear 42-17). Field-level shapes still need a human pass before
merge.

```bash
task generate
```

writes Go server stubs to `gen/go/` and the TypeScript client to `gen/ts/`.
Do not hand-write those packages. `task generate:check` regenerates and fails
if `gen/` is stale. CI runs that check.

The Go stubs import `github.com/oapi-codegen/runtime` for path and query
binding. That is the one module the generated server needs; pin it in
`go.mod`, do not vendor a second copy.

Generated ID schemas are distinct Go types (`type RunID string`, not aliases).
That matches [ADR-Z-0001](../../docs/adr/Z-0001-go-typescript-isolated-kernel.md)
at the HTTP boundary.

The daemon binds `127.0.0.1:8420` by default (`ZEROTH_ADDR` or `zerothd --addr`).

## View mapping

The mockup itself is not in this repository. The stage-1 views are taken to
be the resource groups on this surface.

| View | Expressible through | Notes |
| --- | --- | --- |
| Runs | `POST/GET /runs`, `GET /runs/{id}`, `GET /runs/{id}/events`, `POST /runs/{id}/steer`, `POST /runs/{id}/background`, `POST /runs/{id}/foreground`, `POST /runs/{id}/stop`, `POST /runs/{id}/checkpoints` | Live tail is the WebSocket on `GET /runs/{id}/events`. HTTP without Upgrade returns the same replay window for the CLI. Stop checkpoints, then marks the run stopped so restore can fork a continuation. |
| Plans | `GET /plans`, `GET /plans/{id}`, `POST /plans/{id}/approve`, `POST /plans/{id}/request-changes`, `POST /plans/{id}/branch`, `POST /plans/{id}/apply` | Effects are structured diffs with lease, precondition, idempotency key, and postcondition. The plan carries a canonical hash, expiry, cost ceiling, and draft constraints. Cross-exam is a field on the plan, not an operator endpoint. Apply is the world-changing call. |
| Agents | `GET /agents`, `GET /agents/{id}`, `PATCH /agents/{id}`, `GET /agents/{id}/leases`, `GET /agents/{id}/cross-exam-stats` | PATCH is a signed audited action (`name`, `model`, `tools`, `autonomy_tier`, `reviewer`). Reviewer config is a model, optional dual (both must pass), and block-on-fail. Stats is pass rate plus silent-pass count. Leases are read-only; they are minted during apply. |
| Approvals | `GET /approvals` | Inbox only. `kind` is an open string. Decisions go to the subject resource. |
| Memory | `GET/POST /memory`, `GET /memory/proposals`, `POST /memory/proposals/{id}/accept`, `POST /memory/proposals/{id}/reject` | Operator writes vs agent proposals are separate paths. |
| Audit | `GET /audit`, `POST /audit/{id}/verify` | HTTP verify re-checks one Schnorr signature. `zeroth verify <run-id>` walks the hash chain offline. |
| Checkpoints | `GET /checkpoints`, `POST /runs/{id}/checkpoints`, `POST /checkpoints/{id}/restore` | Restore returns a new run forked from the snapshot. The original run is immutable. |

`GET /health` is liveness for the daemon. It is not a product view.

## Decisions locked in this draft

These match the operator pass on the ten review questions from the first
contract drafts:

1. **Apply** is `POST /plans/{id}/apply`. Response is the updated plan plus
   the audit entry id. Approve does not apply.
2. **Cross-exam** is system-triggered. Exposed as `Plan.cross_exam`
   (`verdict`, `reviewer_model`, `reasoning`, `at`). Known verdicts are
   `pass`, `fail`, `pass_with_notes`. Empty notes are allowed. No operator
   re-run. Pass rate is `GET /agents/{id}/cross-exam-stats`.
3. **Plan list** is `GET /plans?run_id=&status=`. `Plan.effects` is the
   structured diff. `summary` is free-text rationale. `secret_scan_findings`
   is on the plan so the operator sees the gate before apply.
4. **Foreground** and **stop** sit next to background. Stop is graceful
   cancel with a checkpoint on the way out.
5. **Create-run** body is `agent_id` (required), `tracker_ref`, `prompt`,
   `workspace_source`. No sandbox/driver field (stage 1 is Docker only).
6. **`RunEvent.type`** is an open string. Known values are documented on
   the schema. Codegen will not emit a WebSocket client; `web/src/api/runEvents.ts`
   is the thin wrapper around the generated `RunEvent` type.
7. **Agent PATCH** is `name`, `model`, `tools`, `autonomy_tier`, `reviewer`.
   Leases are `GET /agents/{id}/leases`. No auto-promotion.
8. **Approvals inbox** stays an inbox. `kind` is an open string.
9. **On-demand checkpoints** are `POST /runs/{id}/checkpoints`. Restore
   forks a new run.
10. **Bind** is `127.0.0.1:8420` (`ZEROTH_ADDR` / `--addr`). Not an ADR.

## Auth

Stage 1 binds locally. This document has an empty security section: no
operator OAuth, no daemon API keys. Anthropic credentials are never part of
this contract ([ADR-Z-0008](../../docs/adr/Z-0008-anthropic-api-key-auth.md)).
