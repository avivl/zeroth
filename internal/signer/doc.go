// Package signer signs actions and audit records.
//
// Signing is how Zeroth makes the audit trail attributable. The scheme is
// secp256k1 Schnorr, Nostr-compatible (ADR-Z-0007). Callers see a Service
// interface so a KMS backend can drop in later; stage 1 stores each agent's
// secret in the OS keychain or an encrypted file.
package signer
