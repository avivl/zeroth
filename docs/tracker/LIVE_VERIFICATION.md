# Live Linear tracker verification

Linear [42-47](https://linear.app/42-golems/issue/42-47/linear-tracker-poll-loop-silently-swallows-errors-add-logging),
follow-up to [42-45](https://linear.app/42-golems/issue/42-45/linear-tracker-also-trigger-on-native-agent-delegation-not-just).
The default suite drives `FakeGraphQL`. This note is the durable record
that the assignee-or-delegate poll filter was checked against Linear's
real GraphQL schema.

CI still uses the fake. Re-run is opt-in.

## Run

| | |
| --- | --- |
| UTC | 2026-08-22T19:24:01Z |
| Host | Linux 6.12.94+, x86_64, Go 1.27.0 |
| Endpoint | `https://api.linear.app/graphql` |
| Command | `ZEROTH_LIVE_LINEAR=1 go test ./internal/tracker/linear -run TestLive -v` |
| Result | **PASS** (`TestLiveIssueFilterSchema`). `TestLiveListAssigned` skipped (no workspace API key in this environment). |
| Git | `9b74ea7` (this branch, after poll-loop error logging) |

```
=== RUN   TestLiveIssueFilterSchema
--- PASS: TestLiveIssueFilterSchema (0.22s)
=== RUN   TestLiveListAssigned
    live_test.go:42: ZEROTH_LINEAR_API_KEY not set
--- SKIP: TestLiveListAssigned (0.00s)
PASS
```

Introspection of `IssueFilter` on the live API returned both `delegate`
and `or` (and `assignee`) as input fields. The 42-45 poll filter is a
valid `IssueFilter`. A GraphQL rejection of `delegate` is not why
assign-to-Zeroth went quiet; that failure was the poll loop returning
on error with no log line.

`issues(filter: ...)` itself requires authentication (HTTP 401
`AUTHENTICATION_ERROR` with a dummy key). That path is
`TestLiveListAssigned`, gated on `ZEROTH_LINEAR_API_KEY`.

## Fake vs live

| Area | FakeGraphQL | Live Linear | Disposition |
| --- | --- | --- | --- |
| `IssueFilter.delegate` | Implemented in `issueMatchesFilter` | Present on `__type(name: "IssueFilter")` | **Confirmed.** No filter-shape change. |
| `IssueFilter.or` | Implemented in `issueMatchesFilter` | Present on the same type | **Confirmed.** |
| Auth / GraphQL errors in `tick` | Now logged at error | Same code path | **Fixed** in this PR. Visible at default `info`. |
| `zeroth runs` after a real delegate | Covered by fake assign tests | Needs a workspace API key | **Not run here.** No `ZEROTH_LINEAR_API_KEY` in this environment. Operator re-run: set the live env vars from [linear-setup.md](../linear-setup.md), then `ZEROTH_LIVE_LINEAR=1 go test ./internal/tracker/linear -run TestLiveListAssigned -v`. |
