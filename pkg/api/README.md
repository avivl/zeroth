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

## View mapping

The mockup itself is not in this repository. The seven stage-1 views are taken
to be the seven resource groups on the listed surface. If a mockup screen is
something else, say so in review rather than stretching an endpoint.

| View | Expressible through | Notes |
| --- | --- | --- |
| Runs | `POST/GET /runs`, `GET /runs/{id}`, `GET /runs/{id}/events`, `POST /runs/{id}/steer`, `POST /runs/{id}/background` | Live tail is the WebSocket on `GET /runs/{id}/events`. |
| Plans | `GET /plans/{id}`, `POST /plans/{id}/approve`, `POST /plans/{id}/request-changes`, `POST /plans/{id}/branch` | No list. Reach a plan from a run (`current_plan_id`) or an approval. |
| Agents | `GET /agents`, `GET /agents/{id}`, `PATCH /agents/{id}` | PATCH is a signed audited action recorded by the daemon. |
| Approvals | `GET /approvals` | Inbox only. Decisions go to the subject resource, not a generic decide call. |
| Memory | `GET/POST /memory`, `GET /memory/proposals`, `POST /memory/proposals/{id}/accept`, `POST /memory/proposals/{id}/reject` | Operator writes vs agent proposals are separate paths. |
| Audit | `GET /audit`, `POST /audit/{id}/verify` | Verify is re-check of the Schnorr signature, not an edit. |
| Checkpoints | `GET /checkpoints`, `POST /checkpoints/{id}/restore` | No create. Checkpoints are minted by the daemon. |

`GET /health` is liveness for the daemon. It is not a product view.

## Gaps (do not guess)

These are places a mockup screen does not map cleanly onto the listed
surface, or where a field would be invented. They are left out of
`openapi.yaml` on purpose.

1. **Apply.** Kernel lifecycle is draft, cross-exam, approve, apply. The listed
   surface has approve, request-changes, and branch, and has no
   `POST /plans/{id}/apply`. If the plan view has an Apply control, it has no
   endpoint. This draft does not add one.
2. **Cross-exam.** No endpoint. If the plan view shows a cross-exam step as an
   operator action, it is not expressible yet. If it is daemon-side only, the
   plan status enum is enough to display.
3. **Plan list.** No `GET /plans`. A plans index would have to be synthesized
   from runs or from `/approvals`.
4. **Plan diffs.** `Plan.body` is text. A review view that needs file hunks,
   command lists, or secret-scan findings is not covered. Do not invent a
   patch schema here.
5. **Stop / cancel / foreground.** Background is listed. A Stop button, a
   Foreground button, or a retry control is not.
6. **Create-run fields.** Only `agent_id` is required. `prompt` is optional.
   Tracker issue, workspace path, and sandbox options are not specified. A
   "new run" form with those widgets does not map.
7. **Run event payload.** `RunEvent.type` is an open string plus `summary`.
   Tool calls, token counts, and diffs in the live pane need a closed event
   taxonomy after `internal/session` lands.
8. **WebSocket client.** `task generate` emits a GET helper for
   `/runs/{id}/events`. It does not emit a WebSocket client. The web UI will
   need a small hand-written caller against this path. That is the one
   intended exception to "do not hand-write clients," and it should stay a
   caller, not a second contract.
9. **Reconnect.** Replay is `last=N` only. An `after=seq` cursor for drop
   recovery is not listed.
10. **Agent config.** PATCH accepts `name`. Model, autonomy, instructions, and
    tool allowlists are not locked. Scopes, grants, and leases are policy and
    are not on this surface at all. If the agents view is a permission editor,
    it does not map.
11. **Approvals decide.** `GET /approvals` cannot approve. The inbox must deep
    link to `/plans/{id}/approve` (or memory accept/reject). `Approval.kind` is
    an open string until the inbox rows are pinned.
12. **Restore semantics.** Restore is listed, but whether it mints a new run,
    rewinds in place, or itself requires a plan is not locked. The description
    on the operation forbids using restore as a kernel bypass.
13. **Checkpoint create.** No `POST /checkpoints`. A "save checkpoint" control
    on the run view is not expressible.
14. **Policy / leases.** No `/scopes`, `/grants`, or `/leases`. A permissions
    view is not on the stage-1 surface.
15. **Bind port.** Server URL is loopback with no port. Do not freeze `8080`
    in the spec.

## Auth

Stage 1 binds locally. This document has an empty security section: no
operator OAuth, no daemon API keys. Anthropic credentials are never part of
this contract ([ADR-Z-0008](../../docs/adr/Z-0008-anthropic-api-key-auth.md)).
