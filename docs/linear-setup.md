# Connecting Linear (assign-to-Zeroth)

This is the operator walkthrough for 42-29: assigning a Linear issue to the
Zeroth agent identity, or delegating it to that identity while a human stays
assignee, starts a headless run. It covers what that identity is, how to
configure `zerothd` to talk to a real Linear workspace, and the end-to-end
flow from assignment (or delegation) to a merged PR.

Every flag and environment variable below is taken directly from
`cmd/zerothd/config.go` — if a future change renames or removes one, this
doc is stale and should be fixed in the same PR that changes the flag.

## What the Zeroth agent identity is

Zeroth does not need a paid teammate seat to act in your Linear workspace.
The recommended setup is a Linear **OAuth application**, the same pattern
this project's own "Cursor" integration already uses: register an app,
authorize it for your workspace, and it appears as its own actor (visible
in `Settings > Members` with an `@oauthapp.linear.app`-style identity)
that issues can be assigned or delegated to like any teammate.

1. In Linear: **Workspace Settings > API > OAuth applications > New
   application**.
2. Name it `Zeroth`. The callback URL can be a placeholder
   (`http://localhost/callback`) — `zerothd` only needs the resulting
   access token, not a live OAuth redirect flow.
3. Create the app, then install/authorize it for your workspace. This is
   the step that mints the actor.
4. Copy the app's access token. This becomes `ZEROTH_LINEAR_API_KEY`
   below, with `ZEROTH_LINEAR_AUTH_STYLE=oauth`.
5. Find the new actor's Linear user id (via the API or an existing
   integration) — this becomes `ZEROTH_LINEAR_AGENT_USER`.

A personal API key from a real account (teammate or guest) also works —
set `ZEROTH_LINEAR_AUTH_STYLE=personal` (the default) in that case, and
use that account's own personal API key and user id instead. This is
simpler to set up but consumes a seat and ties the identity to a human
account rather than an app-scoped actor.

## Configuration

All of these are `zerothd` flags, each with a matching `ZEROTH_*`
environment variable (flags win over env vars, which win over
`--config` file values).

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `--linear-api-key` | `ZEROTH_LINEAR_API_KEY` | (none) | Linear API key for assign-to-Zeroth. A personal API key or an OAuth app access token, depending on `--linear-auth-style`. |
| `--linear-auth-style` | `ZEROTH_LINEAR_AUTH_STYLE` | `personal` | `personal` sends the key raw in `Authorization`; `oauth` sends `Authorization: Bearer <key>`. Must match how the key above was issued — a mismatch here is the most common setup failure and shows up as an authentication error on the first poll. |
| `--linear-agent-user` | `ZEROTH_LINEAR_AGENT_USER` | (none) | Linear user id of the Zeroth agent identity. This is the actor whose classic assignments and native delegations `zerothd` watches. |
| `--linear-team-id` | `ZEROTH_LINEAR_TEAM_ID` | (none) | Optional: restrict polling to one Linear team. |
| `--linear-project-id` | `ZEROTH_LINEAR_PROJECT_ID` | (none) | Optional: restrict polling to one Linear project. |
| `--linear-poll-interval` | `ZEROTH_LINEAR_POLL_INTERVAL` | `15s` | How often `zerothd` polls for new/changed assignments. Stage 1 default; webhooks are opt-in. |
| `--linear-webhook-secret` | `ZEROTH_LINEAR_WEBHOOK_SECRET` | (none) | Opt-in: if set, `zerothd` also accepts Linear webhook deliveries signed with this HMAC secret, in addition to polling. |
| `--linear-endpoint` | `ZEROTH_LINEAR_ENDPOINT` | (none) | Override the Linear GraphQL endpoint. Leave unset unless you have a reason to point elsewhere. |

Two more flags matter for the walkthrough below, unrelated to Linear:

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `--addr` | `ZEROTH_ADDR` | `127.0.0.1:8420` | Bind address for `zerothd`'s local control plane (the HTTP/WebSocket API the CLI and web UI talk to). |
| `--signing-key` | `ZEROTH_SIGNING_KEY` | (none) | Path to the secp256k1 signing key file. Required for the audit chain (42-27) — every consequential action is signed. |

## Running it end to end

This is the golden workflow: clone, run, assign, approve in the browser,
get a PR.

```bash
git clone https://github.com/avivl/zeroth
cd zeroth

export ZEROTH_LINEAR_API_KEY="<the token from step 4 above>"
export ZEROTH_LINEAR_AUTH_STYLE=oauth        # or "personal" if you used a personal API key
export ZEROTH_LINEAR_AGENT_USER="<Zeroth's actor user id>"
export ZEROTH_LINEAR_TEAM_ID="<your team id>"       # optional but recommended
export ZEROTH_LINEAR_PROJECT_ID="<your project id>" # optional but recommended
export ZEROTH_SIGNING_KEY="$(pwd)/zeroth-signing.key"  # zerothd creates this on first run if absent

task up      # starts zerothd on 127.0.0.1:8420 with SQLite + the Docker sandbox driver
```

In a second terminal:

```bash
task web     # Vite dev server on http://localhost:5173, proxying API calls to 127.0.0.1:8420
```

Now, in Linear:

1. Pick (or create) an issue in the connected team/project. Start with
   something low-stakes — the dogfooding ratchet in 42-32 explicitly
   recommends the safest class first: docs or tests.
2. Assign it to the Zeroth actor, or keep yourself as assignee and
   delegate it to Zeroth (Linear's native agent-delegation, the same
   pattern this repo uses for Cursor).
3. Within one poll interval (15s by default), Zeroth reads the issue and
   project memory, spawns a sandbox, copies this git checkout into the
   overlay, and posts a comment on the issue with its plan. `zerothd`
   uses the git toplevel of the directory it was started from as that
   checkout (the `cd zeroth` above). A modify whose target is missing
   from the overlay fails with a workspace-observe error in the daemon
   logs and on the issue, not a generic "no precondition observed"
   message.
4. Open `http://localhost:5173` and go to **Approvals**. The pending plan
   appears there: a change-plan card with create/modify/destroy/memory
   rows, each expandable to a diff, with leases and expiries shown per
   resource, and the cross-exam verdict inline.
5. Click **Approve**, then **Apply**. The signature chip next to the
   applied plan should read valid — click **Verify** to confirm.
6. Zeroth opens a PR, links it back on the Linear issue, and moves the
   issue's status. The issue comment also carries cost, a transcript
   link, and an audit summary.
7. To confirm the run is genuinely auditable, stop `zerothd` entirely and
   run, offline:

   ```bash
   zeroth verify <run-id>
   ```

   This must pass with the daemon stopped and no network access — that is
   the point of the signed hash chain (42-27).

To cancel a run instead of approving it, un-assign the issue in Linear
(or clear the Zeroth delegate if a human is still the assignee).
`zerothd` fails the session and kills the sandbox — a subsequent `Exec`
against it fails, confirming the sandbox actually died rather than just
being marked stopped in a database row.

## Troubleshooting

**Authentication fails on the first poll.** This is almost always
`--linear-auth-style` not matching how the key was issued. An OAuth app
access token sent without `oauth` (i.e. sent raw, no `Bearer` prefix) or
a personal API key sent with `oauth` (wrapped in a `Bearer` prefix it
doesn't expect) both fail the same way: Linear's GraphQL API returns an
authentication error, and `zerothd` logs `tracker linear poll` at error
on every tick (visible at the default `info` level; `--log-level debug`
is not required). Double-check which kind of key you generated and set
the flag to match.

**Nothing happens after assigning the issue.** Read the daemon logs
first. A GraphQL or auth failure is an error line, not silence. A
healthy poll that matched nothing is a debug line (`issues=0`); raise
`--log-level debug` if you need to tell "loop is alive" from "loop is
broken." Then check `--linear-team-id`/`--linear-project-id` — if set,
they filter which issues `zerothd` even looks at. Also confirm the issue
is actually assigned to the id in `--linear-agent-user`, or delegated to
that same id via Linear's native delegate field, not just labeled or
commented on. A mention without assignee or delegate does not start a
run.

To re-check the live GraphQL schema (no workspace key required):

```bash
ZEROTH_LIVE_LINEAR=1 go test ./internal/tracker/linear -run TestLiveIssueFilterSchema -v
```

A recorded run is in [docs/tracker/LIVE_VERIFICATION.md](tracker/LIVE_VERIFICATION.md).
To poll a real workspace, also set `ZEROTH_LINEAR_API_KEY` (and the usual
auth-style / agent-user vars) and run `TestLiveListAssigned`.

**The run flips to completed with no plan.** That was a stand-in
worker dumping the issue description as live-output tokens, then
succeeding. The daemon now starts the Claude Code harness once per run.
A missing `ANTHROPIC_API_KEY`, a missing `claude` binary, or a turn that
emits no proposed effects fails the run and comments the reason on the
issue. Look in `zerothd` logs for `run failed without a change plan`.

**The sandbox never seems to stop after un-assigning.** Confirm
`--docker-socket` points at a reachable Docker daemon; if `zerothd` can't
reach Docker at all, cancellation can't actually kill anything, only mark
the session failed in the store.
