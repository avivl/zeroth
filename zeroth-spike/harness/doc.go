// Package harness is the spike's one harness touchpoint.
//
// Anthropic auth is API key only ([ADR-Z-0008](../../../docs/adr/Z-0008-anthropic-api-key-auth.md)).
// The key is read from ANTHROPIC_API_KEY. It is never logged, never
// returned, and never written to disk. Consumer OAuth is out of scope.
package harness
