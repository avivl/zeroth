#!/usr/bin/env bash
# Per-boot startup for Zeroth: bring up the Docker daemon the sandbox driver
# (internal/sandbox/docker) and zerothd's socket probe depend on. There is no
# systemd in the Cloud Agent VM, so dockerd is launched directly. Idempotent:
# a daemon that is already up is left alone.
set -euo pipefail

log() { printf '\n=== %s ===\n' "$1"; }

if sudo docker info >/dev/null 2>&1; then
  log "Docker already running"
  exit 0
fi

log "Starting dockerd"
sudo mkdir -p /var/log
# setsid moves dockerd into its own session and process group so it survives
# after this start script returns and the start step's process group is torn
# down. A plain "nohup ... &" only ignores SIGHUP and can still be reaped, which
# leaves the daemon down on boot. Redirect under sudo so the log opens as root.
sudo bash -c 'setsid dockerd </dev/null >>/var/log/dockerd.log 2>&1 &'

for _ in $(seq 1 30); do
  if sudo docker info >/dev/null 2>&1; then
    log "Docker is up"
    exit 0
  fi
  sleep 1
done

echo "dockerd did not become ready in time" >&2
sudo tail -n 40 /var/log/dockerd.log >&2 || true
exit 1
