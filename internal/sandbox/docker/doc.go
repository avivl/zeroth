// Package docker is the Docker-backed [sandbox.Driver].
//
// Stage 1 isolation is a container with a read-only rootfs, tmpfs /tmp
// and /run/zeroth, and a host directory bind-mounted at /workspace.
// Spawn uses --network none. AllowEgress attaches a bridge and an
// HTTP/HTTPS CONNECT proxy whose allowlist is the rules the caller
// passed in.
//
// Credentials are injected per Exec as env or files on the /run/zeroth
// tmpfs, never into /workspace (Z1-113). ExportTar omits the hard
// exclusion list, secret-scans the rest, and fails closed on a finding.
// Long-running in-sandbox processes do not resume on restore.
package docker
