#!/usr/bin/env bash
# Runs inside the demo container under dbus-run-session: starts the mock
# player on the private session bus, waits until spmeta can read it, then
# records the tape. Arguments are passed to vhs.
set -euo pipefail

export LANG=C.UTF-8
export DEMO_TRIGGER=/tmp/spmeta-demo-trigger
mockplayer &
player=$!
trap 'kill "$player" 2>/dev/null || true' EXIT

for _ in $(seq 50); do
  spmeta --quiet && break
  sleep 0.2
done
spmeta --quiet || { echo "spmeta could not read the mock player" >&2; exit 1; }

vhs "$@"
