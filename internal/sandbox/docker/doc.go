// Package docker is the Docker-backed [sandbox.Driver].
//
// Stage 1 isolation is a container with a read-only rootfs, tmpfs /tmp,
// and a host directory bind-mounted at /workspace. Spawn uses
// --network none. AllowEgress attaches a bridge and an HTTP/HTTPS
// CONNECT proxy whose allowlist is the rules the caller passed in.
package docker
