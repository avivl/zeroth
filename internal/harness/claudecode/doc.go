// Package claudecode is the Claude Code-backed [harness.Driver].
//
// It wraps the `claude` CLI (ADR-Z-0003 shim): `--tools ""`,
// `--permission-mode plan`, `--include-partial-messages`, and
// [ProposeEffectsPrompt]. The agent proposes effects. It does not apply
// them. `--input-format stream-json` means the first turn is a stdin
// user message (the argv prompt is ignored by the real CLI). Anthropic
// auth is API-key only (ADR-Z-0008). The key is passed to the subprocess
// as env and is never written into the workspace.
package claudecode
