# ADR-Z-0007: secp256k1 Schnorr, Nostr compatible

- Status: Accepted
- Date: 2026-08-20
- Linear: [42-14](https://linear.app/42-golems/issue/42-14/commit-adr-z-0001-0008-into-docsadr)

## Context

Consequential actions and audit records are signed so a successful agent cannot quietly rewrite history. The skeleton left `internal/signer` as a seam without a scheme. A scheme chosen in chat, or deferred until the first signed record, is how signing becomes "whatever the library defaulted to."

## Decision drivers

- Signatures must be attributable and verifiable without the daemon's private memory.
- Compatibility with [Nostr](https://github.com/nostr-protocol/nips/blob/master/01.md) (BIP-340 Schnorr over secp256k1, x-only pubkeys) keeps identities portable.
- Stage 1 is local and single-player; this is the algorithm, not a key-management product.

## Considered options

- secp256k1 Schnorr (BIP-340), Nostr-compatible.
- Ed25519 (common in Go, not Nostr-native).
- ECDSA over secp256k1 or P-256.
- No signatures in stage 1 (HMAC or unsigned logs).

## Decision

Signing uses secp256k1 Schnorr, compatible with Nostr event signatures (BIP-340). `internal/signer` is the package that applies that scheme to actions and audit records. Key management (where the secret lives, how it is backed up) is not designed here.

## Consequences

- Audit verification can use existing Nostr/BIP-340 tooling.
- Libraries that only do ECDSA or Ed25519 are not a drop-in signer.
- Hardware keys and FIPS modules that lack BIP-340 are out of scope until a new ADR.

## Revisit triggers

- Nostr compatibility stops being a goal, and Ed25519 (or another scheme) is cheaper to operate.
- A platform or compliance requirement mandates a curve or algorithm BIP-340 does not satisfy.
- The Go signer cannot produce or verify Nostr-compatible signatures without a dependency we refuse to take, and a different scheme still meets the audit requirement.
- Key management (as opposed to the algorithm) needs a product decision; that is a different ADR, not a silent change to this one.
