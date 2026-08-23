// Package claudecode is the Claude Code-backed [harness.Driver].
//
// It wraps the `claude` CLI (ADR-Z-0003 shim): `--tools ""`,
// `--permission-mode plan`, `--include-partial-messages`, and
// [ProposeEffectsPrompt]. The agent proposes effects. It does not apply
// them. `--input-format stream-json` means the first turn is a stdin
// user message (the argv prompt is ignored by the real CLI). Anthropic
// auth is API-key only (ADR-Z-0008). The key is passed to the subprocess
// as env and is never written into the workspace.
//
// Stage 1 launches `claude` as a host os/exec child of zerothd, cwd the
// overlay HostWorkspace (ADR-Z-0010). That is not sandbox.Exec. Process
// isolation is a later ADR.
package claudecode
