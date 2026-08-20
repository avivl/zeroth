# ADR-Z-0008: Anthropic auth is API key only; per-provider auth matrix, quarterly review

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)

## Context

The stage-1 harness is Claude Code. Anthropic offers more than one way to authenticate (API keys, subscription/session flows, and whatever ships next). Mixing those in the daemon, or stuffing a token into the repo so a test passes, is how credentials leak and how auth becomes an undocumented snowflake.

## Decision drivers

- Stage 1 is local and single-player: the operator holds a key, the daemon does not become an OAuth client.
- Credentials never belong in git, fixtures, OpenAPI examples, or docs.
- A second provider will arrive; auth must be a matrix we review, not a Claude-shaped special case.

## Considered options

- API key only for Anthropic in stage 1, with a per-provider matrix and a quarterly review.
- Claude.ai / subscription session forwarding.
- OAuth device flow in the local UI from day one.
- A single "whatever the CLI already logged in as" passthrough with no matrix.

## Decision

Anthropic authentication in Zeroth is API key only. Other Anthropic login styles are out of scope for stage 1.

Auth is recorded as a per-provider matrix (provider, allowed methods, where the secret lives, who rotates it). The matrix is reviewed at least quarterly. New providers add a row; they do not inherit Anthropic's method by analogy.

## Consequences

- The operator configures an API key in the local environment (not in the repository). `.env` is gitignored; that is not permission to leave a secret on disk.
- Subscription-only Claude access is not a supported control-plane auth path.
- Quarterly review is an operational trigger, not a code comment we ignore.

## Revisit triggers

- The quarterly review finds a provider row that is wrong, missing, or unused.
- Anthropic withdraws API-key access for the Claude Code surface we drive, or ships a supported flow we must use instead.
- A second model provider lands and cannot be described as a matrix row (then the matrix shape is wrong).
- Stage 2 multiplayer needs delegated credentials or an OAuth client; that is a new ADR, not an extra Anthropic method smuggled into stage 1.
