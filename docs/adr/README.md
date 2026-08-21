# Architecture decision records

ADRs are Markdown Architectural Decision Records (MADR). They live in this directory, one file per decision, from the first commit. The [PRD decision register](../prd/zeroth.md#decision-register) is an index of links, not the record.

## Format

Filename: `Z-NNNN-slug.md` (four-digit number, kebab-case slug). Title in the file: `ADR-Z-NNNN: …`.

Every ADR includes:

| Section | What it is for |
| --- | --- |
| Status | Proposed, Accepted, or Rejected (superseding ADRs say what they replace) |
| Context | The problem and why it had to be decided |
| Decision | The choice, in enough detail to implement against |
| Consequences | What follows, including the costs |
| Revisit triggers | Concrete events that force a new look. This is the part people skip and the part that makes the ADR worth writing |

MADR extras (decision drivers, considered options) are expected when they clarify the choice. Do not bury a decision in a PRD appendix, a chat, or an issue comment.

No em dashes in these files. Use a period, comma, colon, or parentheses.

## How to add one

1. Take the next `Z-NNNN`. Do not reuse a number. Do not leave a gap unless you are reserving a number that an open spike already named (as with [Z-0003](Z-0003-harness-driver-protocol.md) and Linear 42-9).
2. Copy the sections above. Status starts as **Proposed** until the decision is accepted.
3. Add a row to the table below and to the [PRD register](../prd/zeroth.md#decision-register).
4. Link the Linear issue. If a spike decides the outcome, the spike updates this file; it does not replace it.
5. When a decision changes, write a new ADR that supersedes the old one. Do not silently rewrite history.

## Register

| ADR | Title | Status |
| --- | --- | --- |
| [Z-0001](Z-0001-go-typescript-isolated-kernel.md) | Go and TypeScript, isolated policy kernel with typed IDs | Accepted |
| [Z-0002](Z-0002-mit-license.md) | MIT License | Accepted |
| [Z-0003](Z-0003-harness-driver-protocol.md) | Harness driver protocol (ACP or shim) | Accepted |
| [Z-0004](Z-0004-sqlite-first.md) | SQLite first, Postgres at stage 2, one store interface | Accepted |
| [Z-0005](Z-0005-docker-sandbox.md) | Docker sandbox driver is the reference implementation | Accepted |
| [Z-0006](Z-0006-linear-tracker.md) | Linear first, GitHub Issues second | Accepted |
| [Z-0007](Z-0007-secp256k1-schnorr.md) | secp256k1 Schnorr, Nostr compatible | Accepted |
| [Z-0008](Z-0008-anthropic-api-key-auth.md) | Anthropic auth is API key only; per-provider auth matrix, quarterly review | Accepted |
