#!/usr/bin/env bash
# tests/acceptance/down.sh
set -euo pipefail
cd "$(dirname "$0")"
# The compose project name is pinned once in .env, so this always targets
# the same stack up.sh started.
docker compose down -v
