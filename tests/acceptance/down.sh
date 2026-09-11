#!/usr/bin/env bash
# tests/acceptance/down.sh
set -euo pipefail
cd "$(dirname "$0")"
# Must match up.sh's project name so this only ever tears down our own stack.
export COMPOSE_PROJECT_NAME=glitchtip-tf-acceptance
docker compose down -v
