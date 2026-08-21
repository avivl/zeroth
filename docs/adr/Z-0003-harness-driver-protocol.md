# ADR-Z-0003: Harness driver protocol (ACP or shim)

- Status: Accepted
- Date: 2026-08-21
- Linear: [42-9](https://linear.app/42-golems/issue/42-9/gate-g7-evaluate-acp-as-the-harness-driver-protocol-write-adr-z-0003)
- Spike: Gate G7

## Context

The stage-1 harness is Claude Code (`internal/harness/claudecode`). Zeroth drives it through the `harness.Driver` port, five methods (Start, Stream, Steer, Checkpoint, Stop) chosen to map onto ACP if adopted. Two ways to speak to the harness exist: the Agent Client Protocol (ACP), an open JSON-RPC standard that Claude Code, Codex, Gemini, and OpenHands can speak via adapters, and a Zeroth-owned shim wrapping the Claude Code CLI or SDK directly. Spike G7 decides between them, this ADR records the outcome.

## Decision drivers

- The decision rule going in: a custom protocol needs a reason ACP lacks, ambiguity is the only failing outcome.
- G4 ([42-8](https://linear.app/42-golems/issue/42-8/gate-g4-g5-structured-effects-from-claude-code-egress-deny-by-default)) already answered the concrete risk ACP was meant to solve, does the harness yield structured, parseable proposed effects. The shim, driven with `--tools ""` and `--permission-mode plan`, got 10/10 clean structured effects with no protocol and no parser-agent fallback needed.
- A real ACP adapter for Claude exists (`agentclientprotocol/claude-agent-acp`) and looks actively maintained, but it is a third-party bridge built on the Claude Agent SDK, not a first-party Anthropic surface, and its checkpoint and resume semantics are not confirmed to map onto what Zeroth needs.
- Stage 1 has exactly one harness. ACP's payoff, adapter reuse across Codex, Cursor, Pi, and Gemini, only materializes once a second harness is actually being added, and only for harnesses that speak ACP themselves.
- `harness.Driver` is a port either way. Whichever option is picked, a later second harness is a new driver implementation behind the same port, not a second protocol by default.

## Considered options

- ACP: speak the Agent Client Protocol to Claude Code via a maintained adapter.
- Shim: wrap the Claude Code CLI or SDK in a small, Zeroth-owned `claudecode` driver.
- Vendor SDK in the daemon, no port. Rejected, the daemon depends on the port, not on Claude Code by name.

## Decision

Shim. `internal/harness/claudecode` wraps the `claude` CLI directly, the same invocation proven in the G4 spike (`--tools ""`, `--permission-mode plan`, proposed-effects system prompt), behind the `harness.Driver` port. ACP is not adopted for stage 1.

## Consequences

- One harness, one small owned adapter, no external protocol dependency for stage 1.
- Adding Codex, Cursor, or Pi later means a new `harness.Driver` implementation per harness, this is expected cost, not something this decision avoids.
- If a future harness already speaks ACP, an ACP-based driver is still an option for that harness specifically, this ADR does not rule out a mixed world, it only says Claude Code does not need one now.
- The five-method interface (Start, Stream, Steer, Checkpoint, Stop) stays as designed, it is not deleted, since it was not replaced by ACP semantics.

## Revisit triggers

- A second stage-1 harness is added (M3 or later). Check whether it speaks ACP before writing a bespoke shim for it.
- The shim cannot express something ACP would give for free, mid-session steer beyond what is already proven, or checkpoint and resume semantics ACP handles natively and the shim cannot.
- Claude Code changes its CLI or SDK surface in a way that breaks the shim and an ACP adapter is unaffected.
- `agentclientprotocol/claude-agent-acp`, or a first-party Anthropic ACP surface, matures to the point of confirmed checkpoint and resume support, worth a fresh look even without a second harness forcing the question.
