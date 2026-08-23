# Live Claude Code harness verification

Linear [42-36](https://linear.app/42-golems/issue/42-36/run-the-live-harness-test-and-record-proof-of-real-token-tool-call),
follow-up to [42-23](https://linear.app/42-golems/issue/42-23/harnessdriver-and-the-claude-code-adapter) / PR #27.
Requirement: Z1-006.

The default conformance suite drives `internal/harness/claudecode/testdata/fakecli`.
This note is the durable record that `TestLiveClaudeCodeStream` has been run
against a real `claude` subprocess with `ANTHROPIC_API_KEY` from the
environment (ADR-Z-0008). CI still uses the fake CLI. Re-run is opt-in.

## Run

| | |
| --- | --- |
| UTC | 2026-08-21T20:36:38Z |
| Host | Linux 6.12.94+, x86_64, Go 1.27.0 |
| Binary | `/home/ubuntu/.local/bin/claude` (not fakecli) |
| Version | 2.1.239 (Claude Code) |
| Command | `ZEROTH_LIVE_HARNESS=1 go test ./internal/harness/claudecode/... -run TestLiveClaudeCodeStream -v` |
| Result | **PASS** (9.19s) |
| Git | `fa4e36a` (this branch, after the stdin / partial-message fixes below) |

Prompt: `Reply with the single word ok. Do not call tools.` The adapter still
sends [ProposeEffectsPrompt](../../internal/harness/claudecode/prompt.go) as
`--system-prompt`, with `--tools ""` and `--permission-mode plan`.

```
=== RUN   TestLiveClaudeCodeStream
    live_test.go:37: claude binary: /home/ubuntu/.local/bin/claude
    live_test.go:38: claude version: 2.1.239 (Claude Code)
    live_test.go:92: event kind=token payload="{\"effects\":[]}" effects=[]
    live_test.go:92: event kind=token payload="{\"effects\":[]}" effects=[]
    live_test.go:92: event kind=exited payload="stopped" effects=[]
    live_test.go:131: event counts: map[exited:1 token:2]
    live_test.go:132: concatenated tokens: "{\"effects\":[]}{\"effects\":[]}"
    live_test.go:153: session log: events=4 token=true tool_call=false
--- PASS: TestLiveClaudeCodeStream (9.19s)
PASS
ok  	github.com/avivl/zeroth/internal/harness/claudecode	9.188s
```

The session log is an in-memory `session.Supervisor` (pending, running, then
two `token` events). Tokens reached the session event log. No credentials
appeared in payloads or in the temp workspace.

## Fake CLI vs live

| Area | Fake CLI | Live `claude` 2.1.239 | Disposition |
| --- | --- | --- | --- |
| First user turn | Prompt is `os.Args` last | `--input-format stream-json` ignores the argv prompt and waits on stdin | **Fixed.** `Start` writes a stream-json user message on stdin. Argv prompt is kept for the fake CLI and `ps`. |
| Token deltas | Always emits `stream_event` / `content_block_delta` | `--verbose` alone does not. `--include-partial-messages` does. | **Fixed.** Added to the shim flags. |
| After `result` | Process exits | Stdin kept open for Steer, so the process stays up | **Accepted.** The live test Stops after a quiet burst. `EventExited` payload is `stopped`. |
| Truncated stdout | `TRUNCATE` prompt writes one line past the 1 MiB scanner cap, then exits 0 | An over-long or cut-short stream is possible on any long tool result | **Fixed.** `read` keeps `sc.Err()` instead of discarding it: the scan error becomes an `EventError`, the `EventExited` payload reads `stream error: <err>; exit 0`, and the partial transcript is not salvaged into effects (42-67). |
| `tool_call` | Always synthesizes a `Read` tool_use | `--tools ""` plus the system prompt: no tool_use on this run | **Accepted.** Stage 1 is propose-effects, not tool execution (ADR-Z-0003, G4). The fake CLI covers the parser path. |
| `effects` event | Result JSON has a non-empty 3-file set | This prompt produced `{"effects":[]}` | **Accepted.** `ParseEffects` rejects an empty list (G4 corpus is non-empty). `handleResult` falls back to a token, so the two token events are delta plus result. |
| User vs system prompt | Fake ignores the system prompt text | Live followed ProposeEffectsPrompt (`{"effects":[]}`) rather than the user "ok" | **Accepted.** The system prompt is the adapter contract. |

No remaining parse mismatch or stream desync that needs a kernel or port change.
The two live bugs above were in the shim invocation, not in `internal/policy`.
