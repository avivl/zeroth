# ADR-Z-0011: Stage-1 cross-examiner is a different vendor than the producer

- Status: Accepted
- Date: 2026-08-22
- Linear: [42-53](https://linear.app/42-golems/issue/42-53/cross-examination-has-never-been-configured-every-plan-skips-the)

## Context

Cross-exam (Z1-019) is a committed differentiator: a second model in an independent context reviews every consequential plan before apply. Stage 1 shipped the lifecycle (draft, examine, approve, apply) but the only reviewer that ever ran was a pass-through placeholder whose notes were `No independent reviewer model is configured. Human approval is the gate.` Dogfooding therefore produced zero data on whether the mechanism works.

The producer is Claude Code (Anthropic). A same-model or same-vendor second pass does not count. The plan's default was "a different vendor than the producer, by construction."

## Decision drivers

- The reviewer must actually call a model. A hardcoded verdict is not an exam.
- The reviewer vendor must differ from the producer so correlated failure modes are not the whole story.
- Configuration must follow `zerothd` conventions: flags, `ZEROTH_*` env, yaml, defaults.
- CI cannot call a live vendor. A fake HTTP double that returns dynamic text is the conformance path.
- Human approval remains the gate. A failing exam must be visible before approve, not buried in a collapsed plan comment.

## Considered options

- Keep the pass-through and treat human approval as the only review. Rejected: the differentiator never runs.
- A second Claude model via the Anthropic Messages API. Rejected: same vendor as the producer.
- A second harness.Driver implementation. Rejected: stage 1 is one implementation per port, and a reviewer is not a harness.
- OpenAI Chat Completions (OpenAI-compatible HTTP) as the independent reviewer. Chosen.

## Decision

Stage 1's cross-examiner is an OpenAI-compatible Chat Completions call, default model `gpt-4o`, default base URL `https://api.openai.com/v1`. The producer remains Claude Code. Same-model second pass is still rejected by `internal/plan`.

`zerothd` wires it:

| Flag | Env | Default |
| --- | --- | --- |
| `--reviewer-model` | `ZEROTH_REVIEWER_MODEL` | `gpt-4o` |
| `--reviewer-base-url` | `ZEROTH_REVIEWER_BASE_URL` | `https://api.openai.com/v1` |
| `--reviewer-api-key` | `ZEROTH_REVIEWER_API_KEY` | (none) |

`OPENAI_API_KEY` is accepted as a vendor-standard alias when `--reviewer-api-key` / `ZEROTH_REVIEWER_API_KEY` is empty (ADR-Z-0008 matrix row: provider `openai`, method `api_key`, secret in the local environment, operator rotates).

The packet encoding is the whole prompt. The producer's transcript is never an argument. The model must reply `VERDICT:` plus `NOTES:`. Unparseable text is a fail.

When no API key is set, the pass-through reviewer still runs so local tests and a daemon without OpenAI credentials can reach the human inbox. That path is a warn, not the dogfood path.

A fail or `pass_with_notes` stays visible: Linear plan comments put the verdict outside `<details>`, the approvals inbox prefixes the summary with the verdict, and the UI treats those verdicts as an alert above Approve.

## Consequences

- Dogfooding with `ZEROTH_REVIEWER_API_KEY` or `OPENAI_API_KEY` produces a real second-model verdict.
- Operators need an OpenAI (or compatible) key in addition to `ANTHROPIC_API_KEY`.
- Dual review (two models, both must pass) still works: both calls go to the same Chat Completions reviewer with different model ids. Those ids must still differ from the producer and from each other.
- This is not a second harness port. Adding Codex or Gemini as a *producer* remains a new `harness.Driver`.

## Revisit triggers

- OpenAI withdraws API-key access or Chat Completions for the default model.
- A second producer harness lands and would share a vendor with this reviewer.
- Dual review needs a second vendor, not only a second model id.
- Stage 2 multiplayer needs delegated reviewer credentials; that is a new ADR.
