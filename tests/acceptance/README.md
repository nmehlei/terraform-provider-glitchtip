# Acceptance tests

These run against a **real, disposable** GlitchTip instance in Docker (not a
mock). They create and destroy real organizations, teams, projects and keys
through the provider's full CRUD lifecycle. Never point them at a shared or
production instance.

## Prerequisites

- Docker (with `docker compose`)
- Go and `terraform` (v1.9+) on `PATH`
- ~5-6 minutes for the instance's first-boot database migration

## Running

```shell
cd tests/acceptance
./up.sh                       # starts GlitchTip, prints GLITCHTIP_TOKEN=<token>
export GLITCHTIP_ACCEPTANCE=1
export GLITCHTIP_ENDPOINT=http://localhost:8000
export GLITCHTIP_TOKEN=<token printed by up.sh>
cd ../..
go test ./tests/acceptance/... -v -timeout 20m
tests/acceptance/down.sh      # tears everything down, removes volumes
```

Without `GLITCHTIP_ACCEPTANCE=1` the suite skips cleanly, so `go test ./...`
stays green with no Docker running.

## What it covers

`TestAccGlitchTipLifecycle` drives all five resources through one lifecycle
against the real API:

- `glitchtip_organization` — create
- `glitchtip_team` (x2) — create
- `glitchtip_project` — create, then update (`platform` changes)
- `glitchtip_project_key` — create, then `terraform import` with
  `ImportStateVerify` (import id: `<organization_slug>:<project_slug>:<id>`)
- `glitchtip_project_team_membership` — create

The final step re-applies the updated config and asserts an empty plan
(drift check), then `resource.Test` destroys everything at the end of the
run, exercising every resource's `Delete`.

## Notes

- `up.sh` mints a superuser + API token via the Django management shell
  (`./manage.py shell`), using the exact pattern documented in
  `docs/api-notes.md` ("Step 3: first user + org + API token"). It
  deliberately does **not** create an organization — `glitchtip_organization`
  is a full CRUD resource, so the test itself creates and destroys the
  organization via Terraform.
- Each run uses a suffix derived from the test process's PID
  (`tf-acc-<pid>`) so repeated runs against a persistent instance don't
  collide on slugs.
- `down.sh` runs `docker compose down -v`, removing the Postgres volume —
  nothing from the instance persists between runs.
