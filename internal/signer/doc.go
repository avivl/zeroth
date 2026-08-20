// Package signer signs actions and audit records.
//
// Signing is how Zeroth makes the audit trail attributable. The scheme is
// secp256k1 Schnorr, Nostr-compatible (ADR-Z-0007). Key management (where
// the secret lives) is not designed in this package.
package signer
