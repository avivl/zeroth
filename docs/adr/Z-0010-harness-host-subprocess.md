# ADR-Z-0010: Stage-1 harness is a host subprocess against the overlay

- Status: Accepted
- Date: 2026-08-22
- Linear: [42-52](https://linear.app/42-golems/issue/42-52/verify-harness-sandbox-isolation-the-claude-code-harness-runs-as-a-host)

## Context

The sandbox port (`sandbox.Driver`) is the isolation boundary for agent
work: overlay workspace, deny-by-default egress, host-path writes from
inside `Exec` do not mutate the host ([ADR-Z-0005](Z-0005-docker-sandbox.md),
conformance `host_isolation`). Operator-facing language described the
agent as working in that isolated workspace, with nothing touching the
operator checkout.

The code exercised by every dogfooded session does not run the agent
inside that boundary. `internal/harness/claudecode` launches `claude` with
`os/exec.Cmd` as a child of `zerothd`. `sandbox.Exec` (`docker exec`, env
isolation, credential tmpfs) is a separate mechanism the harness never
calls. The daemon instead asks Docker for `HostWorkspace()` (the host
directory bind-mounted at `/workspace`) and passes that path to
`harness.Start` as cwd, so the plan builder and the agent observe the
same overlay tree ([42-49](https://linear.app/42-golems/issue/42-49/plan-builder-observeworkspace-fails-silently-when-hostworkspace-falls)).

That wiring was visible in architecture.md ("the harness (a host
subprocess)") and in `HostWorkspace`, but it was not a decision. Passing
sandbox conformance does not prove the agent is isolated, because those
cases drive `Exec`, not the harness. Stage 2 shared-scope multiplayer
must not inherit this as a solved property.

## Decision drivers

- Stage 1 needs a golden workflow on the operator's machine: host-installed
  `claude`, streaming stdin/stdout for Steer, SIGTERM/SIGKILL of a process
  group, API-key env (ADR-Z-0008).
- `sandbox.Exec` is run-to-completion. It cannot stream tokens or accept
  mid-run Steer. Wiring the harness through it requires a new streaming
  attach on the sandbox port, a container image that contains Node and
  the Claude Code CLI (the stage-1 image is `alpine:3.20`), and an egress
  lease to Anthropic. That is a new feature, not a spawn() bug.
- Workspace identity and process isolation are different properties.
  The overlay is already a copy of the checkout. Plan mode
  (`--tools ""`, `--permission-mode plan`) plus plan-then-apply is the
  stage-1 control on mutation.
- Silence is the defect. Either the harness executes inside the sandbox,
  or the deferral is written down.

## Considered options

- Wire `claudecode` through `sandbox.Exec` (or a new streaming Exec) in
  this change. Rejected for stage 1: port extension, custom image, and
  Anthropic egress are out of scope for a verify-and-decide issue, and
  they would break the golden workflow until all three land together.
- Keep the host subprocess and leave docs implying in-sandbox execution.
  Rejected: that is the silent gap.
- Keep the host subprocess, write this ADR, correct operator docs, and
  add a test that fails if the process is later sandboxed without a new
  ADR.

## Decision

Stage 1 keeps the Claude Code harness as a host subprocess of `zerothd`.
cwd is the sandbox overlay's `HostWorkspace()`, not the operator
checkout. `sandbox.Exec` stays the isolation boundary for daemon-mediated
commands (memory hydrate, credentials, conformance). It is not how the
agent loop runs.

What is true:

- Spawn copies the operator git checkout into the overlay. Relative
  writes in the harness cwd hit that copy.
- Draft observation and apply read the same overlay (`HostWorkspace()`).
- Apply publishes via a git worktree and a pull request. It does not
  save in the operator's working tree.
- `sandbox.Exec` isolation (host canary, `--network none`, credential
  tmpfs) remains real for commands that go through that port.

What is not true yet:

- The `claude` process is not inside the container. Killing or breaking
  the sandbox does not stop the harness.
- The process can read and write host paths outside the overlay.
- Host environment variables are visible to the child (`os.Environ()`
  plus the spec env). Deny-by-default sandbox egress does not apply to it.
- `--tools ""` and `--permission-mode plan` are invocation flags, not an
  isolation boundary.

In-sandbox harness execution is a later change and a new ADR. Stage 2
must not treat stage-1 isolation as already exercised.

## Consequences

- Operator docs (README, `docs/linear-setup.md`) describe the host
  subprocess and the overlay copy, not "nothing touches your checkout"
  as a process-isolation claim.
- `TestSpawnIsHostSubprocess` and `TestHarnessStartUsesSandboxOverlayNotCheckout`
  encode the contract. They fail if spawn moves into the sandbox without
  superseding this ADR, or if `Start` is pointed at the operator checkout
  instead of `HostWorkspace()`.
- Architecture's trust-boundary paragraph names the split: Exec is
  isolated, the harness loop is not.
- A missing Docker install is still a sandbox failure (ADR-Z-0005). The
  overlay and Exec path require Docker. The harness binary is still the
  host `claude`.

## Revisit triggers

- A streaming sandbox attach exists (stdin/stdout, Steer, Stop) and a
  stage-1 image can run the Claude Code CLI.
- Conformance or a live run shows the host subprocess touching the
  operator checkout, leaking credentials into the overlay, or surviving
  `harness.Stop`.
- Stage 2 shared-scope multiplayer is designed. Process isolation is a
  prerequisite, not a follow-up after the control plane is shared.
- The compensating controls (`--tools ""`, plan mode) are removed or
  bypassed so the agent can mutate files without a plan.
