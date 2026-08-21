# pkg/api

`openapi.yaml` is the contract. Generated Go stubs live in `gen/go/` and the TypeScript client in `gen/ts/`. Do not hand-write those packages.

```bash
task generate
```

CI fails if `gen/` is stale relative to the spec (`task generate:check`).

The generated Go server wrapper binds path and query parameters with `github.com/oapi-codegen/runtime` (pinned in `go.mod`). Do not add other OpenAPI runtimes.

## Stage 1 surface

A flow that cannot be expressed here ships on neither the web UI nor the CLI.

| Method | Path | Role |
| --- | --- | --- |
| GET | `/health` | Daemon liveness |
| POST, GET | `/runs` | Start and list runs |
| GET | `/runs/{id}` | Run detail |
| GET | `/runs/{id}/events` | Replay last *n* events, then live tail (WebSocket upgrade, or HTTP snapshot) |
| POST | `/runs/{id}/steer` | Inject operator guidance |
| POST | `/runs/{id}/background` | Keep working without a foreground session |
| GET | `/plans/{id}` | Plan review |
| POST | `/plans/{id}/approve` | Human gate. The daemon applies; there is no client apply |
| POST | `/plans/{id}/request-changes` | Send a plan back |
| POST | `/plans/{id}/branch` | Alternative plan from this one |
| GET | `/agents`, `/agents/{id}` | Agent list and detail |
| PATCH | `/agents/{id}` | Config change (signed audited action) |
| GET | `/approvals` | Plan-approval inbox |
| GET, POST | `/memory` | Read and write memory |
| GET | `/memory/proposals` | Proposed memory |
| POST | `/memory/proposals/{id}/accept` | Accept a proposal |
| POST | `/memory/proposals/{id}/reject` | Reject a proposal |
| GET | `/audit` | Signed trail |
| POST | `/audit/{id}/verify` | Re-check a record's signature |
| GET | `/checkpoints` | List snapshots |
| POST | `/checkpoints/{id}/restore` | Restore a snapshot |

Apply is not a client operation. Approve is the human gate; clients watch the run and the plan.
