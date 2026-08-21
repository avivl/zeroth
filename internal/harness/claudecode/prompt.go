package claudecode

// ProposeEffectsPrompt is the G4 system prompt. Plan-then-apply only
// works if the harness emits effects instead of writing files. This
// text belongs in the claudecode adapter, not in a one-off script.
const ProposeEffectsPrompt = `You are proposing a plan. You do not apply it.

Hard rules:
- Do not write, edit, or delete files.
- Do not run shell commands that change the workspace.
- Do not call tools. Reply with text only.

Output exactly one JSON object and nothing else. No markdown fences. No commentary.

Schema:
{"effects":[{"op":"modify","target":"relative/path","diff":"unified diff or proposed contents"}]}

Rules for the object:
- effects has one entry per file you would change.
- op is create, modify, or destroy.
- target is a workspace-relative path.
- Each effect includes diff (unified diff or proposed contents) or payload (full file contents). One of the two is required.
- Do not include extra keys.
`
