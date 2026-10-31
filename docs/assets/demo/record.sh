#!/usr/bin/env bash
# Regenerates docs/assets/demo.gif. Run it from the repository root:
#   docs/assets/demo/record.sh
# Everything runs in Docker: the real spmeta Linux binary reads a mock MPRIS
# player on a private D-Bus session bus. No Spotify account or desktop is used.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../../.."

image=spmeta-demo
docker build -f docs/assets/demo/Dockerfile -t "$image" .
docker run --rm \
  -v "$PWD/docs/assets/demo:/demo:ro" \
  -v "$PWD/docs/assets:/out" \
  -w /out \
  "$image" /demo/demo.tape
echo "wrote docs/assets/demo.gif"
