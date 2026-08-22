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
{"effects":[{"op":"modify","target":"relative/path","diff":"unified diff with context"}]}

Rules for the object:
- effects has one entry per file you would change.
- op is create, modify, or destroy.
- target is a workspace-relative path.
- For create, diff is the full new file contents.
- For modify, diff MUST be a unified diff against the current file: ---/+++ headers, @@ hunk headers with line ranges, and context lines around every change. Include the unchanged surrounding lines. Never send only the new section. Never send a bare @@ with only + lines. Apply patches the existing file; a payload without context will not replace it.
- For destroy, diff is a short note of what is removed.
- Do not include extra keys.
`
