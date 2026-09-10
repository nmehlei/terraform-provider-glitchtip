#!/usr/bin/env bash
# tests/acceptance/up.sh
set -euo pipefail
cd "$(dirname "$0")"
docker compose up -d
echo "Waiting for GlitchTip to become healthy..."
# Note: macOS ships no `timeout` binary by default (GNU coreutils), so we
# implement the same 180s bound with a manual loop instead of `timeout 180 ...`.
elapsed=0
until [ "$(docker compose ps -q web | xargs docker inspect -f '{{.State.Health.Status}}')" = "healthy" ]; do
  if [ "$elapsed" -ge 180 ]; then
    echo "Timed out waiting for GlitchTip to become healthy" >&2
    exit 1
  fi
  sleep 2
  elapsed=$((elapsed + 2))
done
echo "GlitchTip is up at http://localhost:8000"
