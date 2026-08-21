# Spike divergence log

Semantic surprises found while running the BA-6 docker sandbox against
a real workspace. This is not an ADR. Promote a decision if M2 needs
to change Z1-036 checkpoint semantics.

| Area | What we expected | What happened | Gate |
| --- | --- | --- | --- |
| Overlay mount | Kernel overlayfs on the workspace tree | Nested overlay-on-overlay (`overlay` root in this pod) returns `invalid argument`. Kernel overlayfs works on an ext4 loop. `fuse-overlayfs` works as the invoking user with `allow_other`; dockerd (root) cannot write that FUSE mount unless the container runs as the same uid. | G2 |
| Fuse vs container root | Container root can write a FUSE overlay | Write is `Operation not permitted` unless `--user` matches the fuse-overlayfs mounter. | G2 |
| Tar identity | Export/import tar bytes match | GNU tar headers carry mtime and owners. Byte-identity is the content hash of the tree (paths, modes, file bytes), not the tar file. | G2 |
| Async export | ExportTar might need to pause Exec | Export is a host-side `tar` of the overlay. Exec is `docker exec`. They run in parallel; export does not take a turn lock. A live tar can be slightly inconsistent if the turn writes during the walk. | G2 |
| Kill | Kill might snapshot RAM | `docker kill` drops PIDs. The overlay remains until `Stop`. Resume is `Start` from the last exported tar. Work after that export is gone. | G3 |
| Pid files | Restored pid file means the process is dead | The number in the pid file can be a live PID in the new container's namespace (PID reuse). Liveness is log growth or a port, not `kill -0 $(cat pid)`. | G3 |
| In-sandbox daemon | Documented limitation: process does not resume | Confirmed. Workspace files (logs, pid files) restore. The process does not. A resume must start the server again. | G3 |
| `--network none` | Isolation from the host network | Loopback still exists inside the container. A restored dev server is still not listening until restarted. | G3 |
| `--read-only` rootfs | Agent cannot persist outside the workspace | `/tmp` is a tmpfs. Only `/workspace` is in the checkpoint tar. Writes under `/tmp` are lost on restore. | G2 |
| Exit codes | `Exec` returns the command exit code | Non-zero exit is `ExecResult.ExitCode` with a nil error. Docker CLI failures (daemon down, killed container) are errors. | G1 |
| Host isolation | Writes from inside the sandbox do not mutate the host | Confirmed: an absolute host path written from `docker exec` does not change the host file. The path is either missing or a different `/tmp`. | G1 |

If G2 restore of fixture M is slower than 10 s p50, try rsync-style
deltas of the overlay upperdir before changing the checkpoint model.
If G3 resume is not clean, re-examine Z1-036 before M2.
