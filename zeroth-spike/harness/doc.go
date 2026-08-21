// Package harness is the spike's one harness touchpoint.
//
// Anthropic auth is API key only ([ADR-Z-0008](../../../docs/adr/Z-0008-anthropic-api-key-auth.md)).
// The key is read from ANTHROPIC_API_KEY. It is never logged, never
// returned, and never written to disk. Consumer OAuth is out of scope.
//
// G4 (Linear 42-8) asks Claude Code to emit structured effects instead
// of writing files. ProposeEffectsPrompt is the adapter text that
// belongs in the claudecode driver, not a one-off measurement trick.
package harness
