# pkg/api

`openapi.yaml` is the contract. Stage 1 is local and single-player. A flow that
cannot be expressed through this API ships on neither the web UI nor the CLI.

This draft is human-owned ([CODEOWNERS](../../.github/CODEOWNERS) on `pkg/api/`,
Linear 42-17). Treat field-level shapes as proposed, not locked.

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

## View mapping

The mockup itself is not in this repository. The stage-1 views are taken to
be the resource groups on the listed surface.

| View | Expressible through | Notes |
| --- | --- | --- |
| Runs | `POST/GET /runs`, `GET /runs/{id}`, `GET /runs/{id}/events`, `POST /runs/{id}/steer`, `POST /runs/{id}/background` | Live tail is the WebSocket on `GET /runs/{id}/events`. HTTP without Upgrade returns the same replay window for the CLI. |
| Plans | `GET /plans/{id}`, `POST /plans/{id}/approve`, `POST /plans/{id}/request-changes`, `POST /plans/{id}/branch` | No list. Reach a plan from a run (`plan_id`) or an approval. |
| Agents | `GET /agents`, `GET /agents/{id}`, `PATCH /agents/{id}` | PATCH is a signed audited action recorded by the daemon. |
| Approvals | `GET /approvals` | Inbox only. Decisions go to the plan (or memory proposal) paths. |
| Memory | `GET/POST /memory`, `GET /memory/proposals`, `POST /memory/proposals/{id}/accept`, `POST /memory/proposals/{id}/reject` | Operator writes vs agent proposals are separate paths. |
| Audit | `GET /audit`, `POST /audit/{id}/verify` | Verify is re-check of the Schnorr signature, not an edit. |
| Checkpoints | `GET /checkpoints`, `POST /checkpoints/{id}/restore` | No create. Checkpoints are minted by the daemon. |

`GET /health` is liveness for the daemon. It is not a product view.

## Gaps (do not guess)

These are places a mockup screen does not map cleanly onto the listed
surface. They are left out of `openapi.yaml` on purpose, except where the
issue text already named the action.

1. **Apply.** Kernel lifecycle is draft, cross-exam, approve, apply. The listed
   surface has no `POST /plans/{id}/apply`. Approve is the human gate; the
   daemon applies. Clients watch the run and the plan.
2. **Cross-exam.** No operator endpoint. Plan status can display `cross_exam`.
3. **Plan list.** No `GET /plans`. A plans index would have to be synthesized
   from runs or from `/approvals`.
4. **Plan diffs.** `Plan.steps` is title/detail/kind, not file hunks or
   secret-scan findings. Do not invent a patch schema here.
5. **Stop / cancel / foreground.** Background is listed. Those are not.
6. **Create-run fields.** `agent_id` and `goal` are required. `issue_ref` is
   optional. Workspace path and sandbox options are not specified.
7. **Run event payload.** `RunEvent.type` is an open string plus `message`.
   A closed event taxonomy waits on `internal/session`.
8. **WebSocket client.** `task generate` emits a GET helper for
   `/runs/{id}/events`. It does not emit a WebSocket client. The web UI will
   need a small hand-written caller against this path. That caller is not a
   second contract.
9. **Reconnect.** Replay is `last=N` only. An `after=` cursor for drop
   recovery is not listed.
10. **Agent config.** PATCH accepts `name`, `status`, and `config`
    (model, instructions). That matches "config changes are themselves signed
    audited actions." Scopes, grants, and leases are policy and are not on
    this surface. Credentials are never part of this contract.
11. **Approvals decide.** `GET /approvals` cannot approve. The inbox deep-links
    to `/plans/{id}/approve` (or memory accept/reject).
12. **Restore semantics.** Restore is listed. Whether it mints a new run,
    rewinds in place, or itself requires a plan is not locked. The operation
    description forbids using restore as a kernel bypass.
13. **Checkpoint create.** No `POST /checkpoints`.
14. **Policy / leases.** No `/scopes`, `/grants`, or `/leases`.
15. **Bind port.** Server URL is relative. Do not freeze a port in the spec.

## Auth

Stage 1 binds locally. This document has an empty security section: no
operator OAuth, no daemon API keys. Anthropic credentials are never part of
this contract ([ADR-Z-0008](../../docs/adr/Z-0008-anthropic-api-key-auth.md)).
