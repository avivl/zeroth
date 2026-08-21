// Package claudecode is the Claude Code-backed [harness.Driver].
//
// It wraps the `claude` CLI (ADR-Z-0003 shim): `--tools ""`,
// `--permission-mode plan`, and [ProposeEffectsPrompt]. The agent
// proposes effects. It does not apply them. Anthropic auth is API-key
// only (ADR-Z-0008). The key is passed to the subprocess as env and is
// never written into the workspace.
package claudecode
