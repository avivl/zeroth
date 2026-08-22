# Connecting Linear (assign-to-Zeroth)

This document takes you from a fresh clone to an issue that Zeroth has picked
up, planned, and is waiting for your approval on. It is written for an operator
who has admin rights on a Linear workspace and a machine that can run
`zerothd`.

If you only want the two-minute version: create a Linear OAuth application,
authorize it into your workspace, put its actor token in
`ZEROTH_LINEAR_API_KEY` with `ZEROTH_LINEAR_AUTH_STYLE=oauth`, point
`ZEROTH_LINEAR_AGENT_USER` at the actor it shows up as, start `zerothd`, and
assign an issue to that actor.

---

## 1. What the "Zeroth agent identity" is

Zeroth does not log into Linear as a person. It is a **Linear OAuth
application** that has been authorized for your workspace. Once authorized, the
application appears in Linear as its own actor: it can be assigned issues, it
authors comments under its own name and avatar, and it shows up in the assignee
picker alongside your teammates.

This is the same pattern the built-in "Cursor" integration uses. The important
consequences:

* **It does not consume a teammate or guest seat.** An OAuth app actor is not a
  billed member of the workspace.
* **Its actions are attributable.** Every comment, state transition, and
  assignment change Zeroth makes is stamped with the app actor, so an audit of
  the issue history distinguishes agent activity from human activity without
  any convention or prefix.
* **Its permissions are the app's, not yours.** Revoking the OAuth application
  from workspace settings immediately stops all Zeroth writes, which is the
  intended kill switch.

A **personal API key** also works and is the fastest way to try the flow on a
scratch workspace. The trade-off is that every comment Zeroth writes will
appear to come from *you*, and it does consume your seat's permissions. Use
`ZEROTH_LINEAR_AUTH_STYLE=personal` in that case. Prefer the OAuth app for
anything you would call a real deployment.

### Creating the OAuth application

1. In Linear, open **Settings -> API -> OAuth applications -> Create new**.
2. Give it a name (`Zeroth` is the obvious choice) and an icon. This name is
   what your team will see in the assignee picker and on every comment, so
   pick the one you want to live with.
3. Grant it the scopes it needs to read issues and write comments and issue
   updates. Zeroth needs read access to issues, projects, and comments, and
   write access to comments and issue state/assignment.
4. Enable the option that lets the application act as its own user / be
   assigned work. Without this the app can authenticate but will not appear as
   an assignable actor, and assignment-driven triggering will never fire.
5. Authorize the application into the workspace and copy the resulting **actor
   token**. This is the value for `ZEROTH_LINEAR_API_KEY`.

After authorization, find the actor in the workspace member list (or in the
assignee picker) and note its identifier. That identifier is what you put in
`ZEROTH_LINEAR_AGENT_USER`; it is how `zerothd` recognizes "this issue was
assigned to me" as opposed to "this issue was assigned to somebody else".

---

## 2. Configuration

Every setting below can be supplied as an environment variable or as a
command-line flag. The flag wins if both are present. These names are defined
in `cmd/zerothd/config.go`; if this table and that file ever disagree, the
source is correct and this document is a bug.

| Environment variable | Flag | Required | Meaning |
| --- | --- | --- | --- |
| `ZEROTH_LINEAR_API_KEY` | `--linear-api-key` | yes | The credential `zerothd` authenticates to the Linear GraphQL API with. Either an OAuth actor token or a personal API key, matching `ZEROTH_LINEAR_AUTH_STYLE`. Treat it as a secret; prefer the environment variable over the flag so it does not land in shell history or a process listing. |
| `ZEROTH_LINEAR_AUTH_STYLE` | `--linear-auth-style` | no (defaults to `personal`) | How the credential is presented on the wire. `personal` sends the key as a raw `Authorization` header, which is what Linear personal API keys expect. `oauth` sends it as `Authorization: Bearer <token>`, which is what OAuth actor tokens expect. Getting this wrong is the single most common setup failure; see Troubleshooting. |
| `ZEROTH_LINEAR_AGENT_USER` | `--linear-agent-user` | yes | The Linear actor that represents Zeroth. Assignment of an issue to this actor is the trigger that starts a run; un-assignment from it is the trigger that cancels one. |
| `ZEROTH_LINEAR_TEAM_ID` | `--linear-team-id` | yes | The team whose issues `zerothd` watches. Scoping to a team keeps the polling query small and keeps an accidental workspace-wide assignment from waking the agent. |
| `ZEROTH_LINEAR_PROJECT_ID` | `--linear-project-id` | no | Narrows the watch further to a single project inside the team. Leave it unset to watch the whole team. Setting it is the recommended way to run a first pilot: create a throwaway project, point Zeroth at it, and nothing outside that project can trigger a run. |
| `ZEROTH_LINEAR_POLL_INTERVAL` | `--linear-poll-interval` | no | How often `zerothd` polls Linear for newly assigned issues, as a Go duration (`30s`, `2m`). Polling is the baseline transport and always runs, webhook or not. Shorter intervals mean lower latency and more API calls; Linear rate-limits, so do not set this to something absurd. |
| `ZEROTH_LINEAR_WEBHOOK_SECRET` | `--linear-webhook-secret` | no | Opt-in. If set, `zerothd` exposes a webhook endpoint and verifies incoming Linear webhook signatures against this secret, giving near-instant pickup instead of waiting for the next poll. If unset, no webhook endpoint is served and the poller is the only path. The webhook is an accelerator, never a requirement: if it is misconfigured or unreachable, the poll loop still picks the issue up. |

### A worked example

```sh
export ZEROTH_LINEAR_API_KEY="..."                 # OAuth actor token
export ZEROTH_LINEAR_AUTH_STYLE=oauth
export ZEROTH_LINEAR_AGENT_USER="..."              # the Zeroth app actor
export ZEROTH_LINEAR_TEAM_ID="..."
export ZEROTH_LINEAR_PROJECT_ID="..."              # optional but recommended for a pilot
export ZEROTH_LINEAR_POLL_INTERVAL=30s
# export ZEROTH_LINEAR_WEBHOOK_SECRET="..."        # optional, opt-in

zerothd
```

The equivalent with flags, for a one-off run:

```sh
zerothd \
  --linear-auth-style=oauth \
  --linear-agent-user="..." \
  --linear-team-id="..." \
  --linear-project-id="..." \
  --linear-poll-interval=30s
```

On startup `zerothd` logs which team (and project, if scoped) it is watching
and which actor it is watching for. Read that line before you assign anything.
If it names a different team than you expect, assignment will silently do
nothing and you will spend twenty minutes wondering why.

---

## 3. The end-to-end operator flow

Once `zerothd` is running against your workspace:

**1. Assign an issue to the Zeroth actor.** No magic comment, no label, no
slash command. Assignment *is* the request. Anyone on the team who can assign
an issue can start a run, which is the point: the interface is the one your
team already uses.

**2. Zeroth acknowledges.** Within one poll interval (or immediately, if the
webhook is configured), the issue picks up a comment from the agent actor
saying it has taken the work. If you do not see this comment, the agent never
saw the assignment, and the problem is configuration, not the agent.

**3. Zeroth reads and plans.** It reads the issue body, its comments, and the
project's accumulated memory, then works in an isolated sandbox. It posts its
**plan** as a comment on the issue: what it intends to change and why. It then
stops and waits. It does not proceed past the plan on its own.

**4. You approve.** Approval happens either from the CLI or from the
**Approvals inbox** in the web UI, which lists every run currently blocked on a
human. Approving releases the run; rejecting ends it. The plan comment on the
issue is the same plan you are approving, so a reviewer who only ever looks at
Linear still sees exactly what was authorized.

**5. Zeroth executes and reports.** On completion it posts a closing comment on
the issue containing:

* the **cost** of the run,
* a link to the full **transcript**,
* a link to the **pull request** it opened, if it produced code,
* an **audit summary** of what it actually did — the tool calls, the files
  touched, the commands run.

The issue thread is therefore a complete record on its own. You should never
have to log into the agent's own UI to answer "what did this thing do and what
did it cost".

**6. If you change your mind, un-assign the issue.** Removing the Zeroth actor
from the assignee field mid-run cancels the run: the agent stops work, tears
down the sandbox, and posts a comment noting the cancellation and the cost
incurred up to that point. This is the same gesture you would use to take work
away from a human teammate, and it is the fastest way to stop a run you do not
like. Un-assignment is authoritative; you do not have to find the right button
in a dashboard under time pressure.

---

## 4. Troubleshooting

**Nothing happens when I assign an issue.**

By far the most likely cause is an **auth mismatch**: the credential in
`ZEROTH_LINEAR_API_KEY` is one style and `ZEROTH_LINEAR_AUTH_STYLE` says the
other. An OAuth actor token sent without the `Bearer` prefix, or a personal API
key sent with it, is rejected by Linear as unauthenticated. `zerothd` will log
an authentication failure from the Linear API on its first poll — check the
startup logs before checking anything else. The fix is to set
`ZEROTH_LINEAR_AUTH_STYLE=oauth` for an OAuth application token, or
`personal` for a key you generated under your own user settings.

Other causes, in rough order of likelihood:

* **Wrong team or project.** The issue is outside the `ZEROTH_LINEAR_TEAM_ID`
  (or `ZEROTH_LINEAR_PROJECT_ID`) that `zerothd` is watching. Compare against
  the startup log line.
* **Wrong actor.** `ZEROTH_LINEAR_AGENT_USER` does not match the actor the
  issue was actually assigned to. This happens when a workspace has both a
  personal-key setup and an OAuth app installed and the two get crossed.
* **Missing scopes.** The OAuth application can read but not comment, so the
  run starts and then fails on its first write. The Linear API error in the
  logs will name the permission.
* **Poll interval.** You are simply waiting less time than
  `ZEROTH_LINEAR_POLL_INTERVAL`. Configure the webhook secret if the latency
  bothers you.

**The webhook never fires.** Confirm the secret in Linear's webhook settings
matches `ZEROTH_LINEAR_WEBHOOK_SECRET` exactly and that the endpoint is
reachable from Linear. A signature mismatch is logged and the event dropped.
This degrades to poll-speed pickup rather than breaking the system, which is
why the webhook is opt-in.
