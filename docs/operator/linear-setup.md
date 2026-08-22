# Connecting Linear (assign-to-Zeroth)

This is the operator guide for wiring `zerothd` to a real Linear workspace so
that assigning an issue to the Zeroth agent identity starts a run.

It assumes a fresh clone of this repository and nothing else. If you follow it
top to bottom you should end with an issue assigned to Zeroth, a plan comment on
that issue, and an approval prompt waiting for you.

> Docs-only note: every environment variable and flag name below is intended to
> mirror `cmd/zerothd/config.go` exactly. If you find a mismatch, the source is
> the truth and this file is the bug.

---

## 1. What the "Zeroth agent identity" is

Zeroth does **not** log in as a teammate. It authenticates as a Linear **OAuth
application actor** — the same shape of integration you already see when the
"Cursor" app comments on an issue.

What that means in practice:

* You create (or are invited to) an OAuth application in Linear, and authorize
  it for the workspace.
* Linear then shows that application as its own **actor**: it can be assigned
  issues, it can comment, and its activity is attributed to the app rather than
  to a human.
* It does **not** consume a teammate or guest seat, and it is not tied to any
  one person's account. Revoking the app's authorization revokes Zeroth's access
  in one step, without touching anybody's personal API keys.

There is a second, lower-ceremony option for local development: a **personal API
key**. That key acts as *you*, so assignments and comments appear under your own
name. It is fine for a first smoke test on a scratch team; it is not what you
want in a shared workspace.

Which of the two you are using is what `ZEROTH_LINEAR_AUTH_STYLE` selects. Get
this wrong and nothing else in this document will work — see
[Troubleshooting](#6-troubleshooting).

---

## 2. Prerequisites

1. A Linear workspace where you can create or authorize an OAuth application.
2. The team you want Zeroth to watch, and (optionally) a project inside it.
3. A built `zerothd`. From a fresh clone:

   ```sh
   task up
   ```

---

## 3. Getting the credentials

### Option A — OAuth actor token (recommended)

1. In Linear, go to **Settings → API → Applications** and create an application
   (name it something recognizable, e.g. `Zeroth`).
2. Authorize it for your workspace, requesting at minimum read/write scope on
   issues and comments.
3. Complete the OAuth exchange and keep the resulting **actor access token**.
4. Note the identifier Linear reports for the app actor. That value goes in
   `ZEROTH_LINEAR_AGENT_USER`; it is what `zerothd` compares an issue's assignee
   against to decide "this one is mine".

With an OAuth actor token, set `ZEROTH_LINEAR_AUTH_STYLE=oauth`. Tokens of this
kind are sent as `Authorization: Bearer <token>`.

### Option B — personal API key (local development only)

1. **Settings → API → Personal API keys → Create key.**
2. Copy the key.
3. `ZEROTH_LINEAR_AGENT_USER` is then *your* user, since that is the actor the
   key speaks as.

With a personal key, set `ZEROTH_LINEAR_AUTH_STYLE=personal`. Keys of this kind
are sent as a raw `Authorization: <key>` header, with no `Bearer` prefix.

### Finding the team and project IDs

The team key is visible in every issue identifier (`ENG-123` → team `ENG`), and
the full UUIDs for both team and project are available from the Linear API or
from the URL of the team/project settings page.

---

## 4. Configuration

Every setting can be supplied as an environment variable or as a command-line
flag. Flags win over environment variables.

| Environment variable | Flag | Required | Meaning |
| --- | --- | --- | --- |
| `ZEROTH_LINEAR_API_KEY` | `--linear-api-key` | yes | The credential itself: an OAuth actor token or a personal API key. Never commit this. |
| `ZEROTH_LINEAR_AUTH_STYLE` | `--linear-auth-style` | yes | `oauth` or `personal`. Selects how the credential is presented to Linear: `oauth` sends `Bearer <token>`, `personal` sends the raw key. Must match the kind of credential in `ZEROTH_LINEAR_API_KEY`. |
| `ZEROTH_LINEAR_AGENT_USER` | `--linear-agent-user` | yes | The actor Zeroth answers as. An issue assigned to this actor is an issue Zeroth will pick up; anything else is ignored. |
| `ZEROTH_LINEAR_TEAM_ID` | `--linear-team-id` | yes | The team to watch. Scopes polling so Zeroth does not walk the whole workspace. |
| `ZEROTH_LINEAR_PROJECT_ID` | `--linear-project-id` | no | Narrows the watch further to a single project. Leave unset to watch the whole team. |
| `ZEROTH_LINEAR_POLL_INTERVAL` | `--linear-poll-interval` | no | How often to poll Linear for newly assigned issues, as a Go duration (`30s`, `2m`). Polling is the baseline delivery mechanism and always runs. |
| `ZEROTH_LINEAR_WEBHOOK_SECRET` | `--linear-webhook-secret` | no | Opt-in. Set it to enable webhook delivery and to verify inbound webhook signatures. Unset means polling only — which is a perfectly good default. |
| `ZEROTH_REVIEWER_MODEL` | `--reviewer-model` | no | Independent cross-exam model (default `gpt-4o`). Must differ from the Claude Code producer. |
| `ZEROTH_REVIEWER_BASE_URL` | `--reviewer-base-url` | no | OpenAI-compatible Chat Completions root (default `https://api.openai.com/v1`). |
| `ZEROTH_REVIEWER_API_KEY` | `--reviewer-api-key` | no | Reviewer API key. `OPENAI_API_KEY` is also accepted. Without a key, every plan gets a pass-through placeholder instead of a real second-model review. |

A local `.env`-style setup for a first run:

```sh
export ZEROTH_LINEAR_API_KEY='…'
export ZEROTH_LINEAR_AUTH_STYLE=oauth
export ZEROTH_LINEAR_AGENT_USER='…'      # the Zeroth app actor
export ZEROTH_LINEAR_TEAM_ID='…'
export ZEROTH_LINEAR_PROJECT_ID='…'      # optional
export ZEROTH_LINEAR_POLL_INTERVAL=30s   # optional
export ZEROTH_REVIEWER_API_KEY='…'       # or OPENAI_API_KEY; without this, cross-exam is a placeholder
# export ZEROTH_LINEAR_WEBHOOK_SECRET='…'  # optional, opt-in
```

The flag form is equivalent and is easier to keep out of a shell history file:

```sh
zerothd \
  --linear-api-key "$LINEAR_TOKEN" \
  --linear-auth-style oauth \
  --linear-agent-user '…' \
  --linear-team-id '…' \
  --linear-poll-interval 30s
```

### Webhooks (optional)

Webhooks reduce assignment-to-start latency from "up to one poll interval" to
"about as fast as Linear can deliver". They are strictly an optimization:

1. Set `ZEROTH_LINEAR_WEBHOOK_SECRET` to a secret you generate.
2. In Linear, **Settings → API → Webhooks**, point a webhook at your `zerothd`
   endpoint, subscribe to issue events, and paste the same secret.

Inbound requests whose signature does not verify against the secret are
rejected. If the secret is unset, the webhook path is disabled entirely and
polling carries the load.

---

## 5. The end-to-end flow

Once `zerothd` is running with the configuration above:

1. **You assign an issue to the Zeroth actor** in Linear — normal assignment, no
   magic comment, no label.
2. **Zeroth notices**, via webhook if configured, otherwise on the next poll.
3. **It reads context**: the issue title, description, and comment thread, plus
   the accumulated project memory for that team/project.
4. **It works in a sandbox.** The run is isolated; nothing touches your working
   copy or the default branch.
5. **It comments its plan on the issue** and then *stops*. Zeroth does not merge
   anything on its own initiative — the plan comment is a request for approval.
   The cross-exam verdict sits above the collapsed plan body. A fail or
   `pass_with_notes` is a concern to read before you approve.
6. **You approve (or reject)** in either of two places:
   * the CLI, or
   * the **Approvals inbox** in the web UI, which collects every waiting run in
     one list.
7. **On completion, Zeroth comments back on the issue** with:
   * the run's **cost**,
   * a link to the full **transcript**,
   * a link to the **pull request**, if one was opened,
   * an **audit summary** of what it actually did.
8. **If you un-assign the issue mid-run**, that is the cancel signal. The run is
   cancelled, the sandbox is torn down, and the issue records that the run was
   cancelled rather than silently abandoned.

The first successful loop through steps 1–7 is the thing to aim for. If you get
a plan comment on an issue you assigned, the integration is working.

---

## 6. Troubleshooting

**Everything returns an authentication error.** This is the common failure, and
it is almost always a mismatch between the credential in
`ZEROTH_LINEAR_API_KEY` and the value of `ZEROTH_LINEAR_AUTH_STYLE`. An OAuth
actor token sent in `personal` style, or a personal API key sent as a `Bearer`
token, both fail the same unhelpful way. Check the pairing first:

| Credential | Correct `ZEROTH_LINEAR_AUTH_STYLE` | Header sent |
| --- | --- | --- |
| OAuth actor token | `oauth` | `Authorization: Bearer <token>` |
| Personal API key | `personal` | `Authorization: <key>` |

**Assignments are ignored.** Confirm that `ZEROTH_LINEAR_AGENT_USER` names the
actor you are actually assigning to, and that the issue lives inside
`ZEROTH_LINEAR_TEAM_ID` (and `ZEROTH_LINEAR_PROJECT_ID`, if you set it).

**Assignments are noticed, but slowly.** That is the poll interval. Lower
`ZEROTH_LINEAR_POLL_INTERVAL`, or configure webhooks.

**Webhooks never arrive.** The secret in Linear must match
`ZEROTH_LINEAR_WEBHOOK_SECRET` byte for byte; mismatched signatures are dropped.
Polling continues regardless, so a broken webhook shows up as latency rather
than as total silence.
