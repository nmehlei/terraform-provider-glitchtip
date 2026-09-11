#!/usr/bin/env bash
# tests/acceptance/up.sh
set -euo pipefail
cd "$(dirname "$0")"
# Pin an explicit, unique compose project name. Without this, `docker compose`
# defaults the project name to the directory basename ("acceptance"), which
# collides with any other unrelated compose stack on a shared Docker daemon
# that happens to use the same directory name — observed in practice during
# Task 16: `docker compose up` reused pre-existing containers/volumes from a
# completely different, unrelated project also named "acceptance" (including
# an incompatible Postgres 15 data volume). Do not remove this.
export COMPOSE_PROJECT_NAME=glitchtip-tf-acceptance
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

# Mint a superuser + API token non-interactively via the Django management
# shell, using the exact pattern proven working in docs/api-notes.md
# ("Step 3: first user + org + API token"). Deliberately do NOT create an
# organization here: glitchtip_organization is a full CRUD resource and the
# acceptance suite exercises organization creation itself via Terraform.
#
# NOTE the module paths are `apps.organizations_ext...` / `apps.api_tokens...`
# (the brief's speculative `organizations_ext.models` / `api_tokens.models`
# without the `apps.` prefix do not import - see docs/api-notes.md Step 3).
echo "Minting acceptance superuser + API token..."
docker compose exec -T web ./manage.py shell -c "
from django.contrib.auth import get_user_model
from apps.api_tokens.models import APIToken

User = get_user_model()
u, created = User.objects.get_or_create(
    email='acceptance@example.com',
    defaults={'is_superuser': True, 'is_staff': True, 'is_active': True})
if created:
    u.set_password('acceptance-test-pw-12345')
    u.save()

t, created = APIToken.objects.get_or_create(user=u, label='acceptance-cli')
if created:
    t.add_permissions(['project:read','project:write','project:admin','project:releases',
        'team:read','team:write','team:admin','event:read','event:write','event:admin',
        'org:read','org:write','org:admin','member:read','member:write','member:admin'])
    t.save()
t.refresh_from_db()
print('GLITCHTIP_TOKEN=' + t.token)
" | grep '^GLITCHTIP_TOKEN='

echo "Export the GLITCHTIP_TOKEN printed above before running the acceptance suite."
