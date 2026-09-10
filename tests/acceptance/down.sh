#!/usr/bin/env bash
# tests/acceptance/down.sh
set -euo pipefail
cd "$(dirname "$0")"
docker compose down -v
