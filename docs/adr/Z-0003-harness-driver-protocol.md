# ADR-Z-0003: Harness driver protocol (ACP or shim)

- Status: Proposed
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)
- Spike: [42-9](https://linear.app/42-golems/issue/42-9/gate-g7-evaluate-acp-as-the-harness-driver-protocol-write-adr-z-0003) (Gate G7)

## Context

The stage-1 harness is Claude Code (`internal/harness/claudecode`). Zeroth must drive it through the `harness.Driver` port without giving the vendor a path around plan-then-apply. Two plausible ways to speak to the harness exist: the [Agent Client Protocol](https://agentclientprotocol.com/) (ACP), and a Zeroth-owned shim around the Claude Code CLI or SDK.

Choosing in an appendix and migrating later is the failure this register exists to avoid. The choice is not made here; spike 42-9 decides, and this ADR records the outcome.

## Decision drivers

- The harness is untrusted relative to the kernel. Protocol convenience must not skip draft, cross-exam, approve, apply.
- Stage 1 is one implementation. The protocol is the adapter, not a second harness product.
- ACP is an open JSON-RPC standard with a Claude Code adapter. A shim is smaller and owned, and it tracks one vendor's surface.

## Considered options

- ACP: speak the Agent Client Protocol to Claude Code (directly or via a maintained adapter).
- Shim: wrap the Claude Code CLI or SDK in a small `claudecode` driver that we own.
- Vendor SDK in the daemon (no port). Rejected: the daemon depends on the port, not on Claude Code by name.

## Decision

**Proposed.** Spike [42-9](https://linear.app/42-golems/issue/42-9/gate-g7-evaluate-acp-as-the-harness-driver-protocol-write-adr-z-0003) evaluates ACP against a shim and accepts this ADR with one of those two options. Until then the `harness.Driver` port stays; `claudecode` remains a stub.

This file is the record. The spike does not leave the decision in chat or in an issue comment.

## Consequences

- Until 42-9 lands, no harness I/O ships on a guessed protocol.
- Whichever option wins must still go through the port and the plan lifecycle.
- A later second harness is a new implementation of the same port, not a second protocol by default.

## Revisit triggers

- Spike 42-9 completes: this ADR must move from Proposed to Accepted (or Rejected) with the chosen option named.
- After acceptance, ACP (or the shim) cannot express lease-gated tool use or plan-then-apply without a kernel bypass.
- Claude Code drops or breaks the surface the chosen option depends on, and the other option is still viable.
- A second stage-1 harness is required and cannot share the chosen protocol.
