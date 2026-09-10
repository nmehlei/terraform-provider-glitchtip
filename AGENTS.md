# Terraform Provider for GlitchTip

## Mission

Build a public, independent Terraform provider for GlitchTip. Use Go and
HashiCorp's Terraform Plugin Framework; do not use SDKv2 for new code.

## First action

Read `docs/superpowers/specs/2026-09-10-terraform-provider-glitchtip-design.md`
and `docs/api-notes.md`.

## Security and testing

- Accept API credentials only through a sensitive provider attribute or
  `GLITCHTIP_TOKEN`; never log, return, or write them to fixtures.
- Treat DSNs as sensitive state.
- Use disposable Docker GlitchTip instances for acceptance tests. Never
  point tests at a shared or production instance.

## Repository conventions

- Put Go source under `src/` and tests under `tests/`.
- Use semantic versioning, generated Registry documentation, signed
  releases, and a public GitHub release workflow.

## Completion gate for v1

Do not publish until every resource has tested create/read/update/delete,
import, drift detection, and acceptance tests.
