# Reject-with-comment feeds the next plan

Linear [42-54](https://linear.app/42-golems/issue/42-54/verify-or-build-reject-with-comment-actually-feeding-back-into-the).
Operator docs promised that rejecting a plan with a comment would feed
that reasoning into the next draft. Until this change, `POST
/plans/{id}/request-changes` stored the text on the plan row and moved
the session back to running, then did nothing else. The Approvals inbox
had no comment box. There was no `zeroth reject`. Un-assign/re-assign
started a new run that never read the issue comment thread.

## What the flow does now

1. Operator rejects from the Approvals inbox or `zeroth reject PLAN_ID
   --comment "that heading doesn't exist, use the real one"`.
2. The daemon stores the comment on the plan, posts
   `### Zeroth plan rejected` on the tracker issue, and appends
   `## Operator rejection` plus the comment to the run prompt.
3. A second harness turn starts on the same run. Cross-exam runs again.
4. A later assign-to-Zeroth run calls `ListComments` and includes the
   thread in the new prompt, so the same correction survives un-assign.

## Evidence (not a fake Provider)

`TestRejectCommentFeedsNewRunThroughLinearDriver` drives the real
`internal/tracker/linear` Provider against `FakeGraphQL` (the Linear
HTTP surface double, not a second Provider). It:

1. Assigns `42-1` through Linear polling.
2. Waits for a drafted plan.
3. Rejects with `that heading doesn't exist, use the real one`.
4. Asserts the second harness `Start` prompt contains that sentence.
5. Asserts Linear stored `### Zeroth plan rejected` with the sentence.
6. Asserts `ListComments` returns it.
7. Un-assigns, re-assigns, and asserts the new run's prompt still
   contains the sentence.

`TestRejectCommentFeedsNextHarnessPrompt` covers the same path through
the daemon with the in-process tracker stub, including the CLI-shaped
request-changes body.

`TestProviderConformance/linear/list_comments` is the port contract a
later GitHub Issues provider must pass.

Live GraphQL remains opt-in (`ZEROTH_LIVE_LINEAR=1`), same as
[LIVE_VERIFICATION.md](LIVE_VERIFICATION.md).
