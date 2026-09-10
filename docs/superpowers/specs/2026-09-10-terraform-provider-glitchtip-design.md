# terraform-provider-glitchtip — design

Status: approved (approach A), pending written-spec review
Date: 2026-09-10
Author: Claude (with nm@homesk.de)

## Objective

Publish an independent, Apache-2.0, community Terraform provider that
declaratively manages a [GlitchTip](https://glitchtip.com/) instance's
organizations, teams, projects, project keys (DSNs), and project↔team
membership, with a stable Registry provider address and signed
cross-platform releases.

This follows the pattern already proven by two sibling providers in the
same GitHub account: `terraform-provider-bugsink` (repo layout, client
design, real-instance acceptance testing, release pipeline) and
`terraform-provider-revenuecat` (attachment-resource pattern for
many-to-many relationships). Where the two disagree, this design follows
bugsink — see "Alternatives considered" for why.

## Background: the GlitchTip API

GlitchTip is Sentry-SDK-compatible and exposes a REST API at `/api/0/`
(plus an OpenAPI schema at `/api/openapi.json` on every instance).
Unlike Bugsink, it has full CRUD — including DELETE — for every resource
this provider needs:

| Resource | List/Get | Create | Update | Delete |
| --- | --- | --- | --- | --- |
| Organization | yes | yes | yes | yes |
| Team | yes | yes | yes | yes |
| Project | yes | yes | yes | yes |
| Project key (DSN) | yes | yes | yes | yes |
| Project↔team membership | yes (via project) | yes (assign) | n/a | yes (remove) |

Auth is a bearer auth token (`Authorization: Bearer <token>`), created
per-user or per-organization in the GlitchTip UI. There is no documented
token-scoping mechanism narrower than "belongs to a user with access to
the org" — this is a known limitation to call out in docs, same spirit as
bugsink's "tokens are installation-wide" caveat.

Because organizations, teams, and projects are addressed by **slug**, not
by an opaque server-generated ID, slugs double as part of the resource
identity. GlitchTip derives a project/team's slug from its name at create
time and does not expose a way to set an arbitrary slug independent of
name (mirroring Sentry). This provider surfaces `slug` as a computed
attribute rather than letting practitioners set it directly, avoiding a
class of "plan never converges" bugs where a requested slug and the
server-derived one disagree.

## Scope

**v1 resources**, all full create/read/update/delete + import + drift
detection:

- `glitchtip_organization` — top-level org. Attributes: `id`/`slug`
  (computed), `name` (required).
- `glitchtip_team` — a team within an organization. Attributes:
  `organization_slug` (required, `RequiresReplace`), `id`/`slug`
  (computed), `name` (required).
- `glitchtip_project` — a project within an organization, associated
  with at least one team at creation. Attributes: `organization_slug`
  (required, `RequiresReplace`), `initial_team` (required,
  `RequiresReplace` — see "Approach A" below), `id`/`slug` (computed),
  `name` (required), `platform` (optional), `event_throttle_rate`
  (optional).
- `glitchtip_project_key` — a DSN-bearing key for a project. Attributes:
  `organization_slug` + `project_slug` (required, `RequiresReplace`),
  `id` (computed), `name` (optional), `dsn_public` / `dsn_security`
  (computed, **sensitive**).
- `glitchtip_project_team_membership` — associates an existing project
  with an existing team beyond the project's `initial_team`. Attributes:
  `organization_slug`, `project_slug`, `team_slug` (all required,
  `RequiresReplace` — the resource has no updatable fields, only
  existence).

**Out of scope for v1** (same spirit as bugsink excluding issues/events/
sourcemaps, and revenuecat excluding projects-as-resource): issues,
events, alerts/notifications, uptime monitors, users/members, SSO
config, billing. These are all real GlitchTip API surface but add
significant modeling work each; v1 stays focused on the
org/team/project/key skeleton practitioners need to provision a new
service's error tracking.

**Data sources**: none in v1. Every v1 resource is fully manageable
(unlike revenuecat's read-only `project`), so there's no case yet where a
practitioner needs to reference something this provider can't create.
Revisit if a real workflow needs to look up an org/team created outside
Terraform.

## Approach A: project↔team via a separate attachment resource

A GlitchTip project is created under an organization and requires at
least one team at creation time (`POST /api/0/organizations/{org}/projects/`
takes a `team` field, or the equivalent `POST
/api/0/teams/{org}/{team}/projects/`), but can be associated with
additional teams afterward via `POST
/api/0/projects/{org}/{project}/teams/{team}/` and disassociated via
`DELETE` on the same path.

This provider models that as:

- `glitchtip_project.initial_team` — the team the project is created
  under. Required, immutable (`RequiresReplace`): changing which team a
  project was *initially* created under is not a rename, it's a
  different project in GlitchTip's model, so we don't pretend it's an
  in-place update.
- `glitchtip_project_team_membership` — every *additional* team↔project
  link, one resource per link, matching `revenuecat_entitlement_product_
  attachment`'s shape but simpler: this is a bare link with no owned set
  to diff, just create-if-absent / delete-if-present. Import format:
  `org_slug:project_slug:team_slug`.

Removing `glitchtip_project_team_membership` from configuration detaches
that team from the project; it never touches `initial_team`'s
membership, which is only removable by deleting the project itself.

This keeps every resource single-purpose and independently testable,
consistent with the design-for-isolation principle both sibling
providers follow, and sidesteps the "two resources own the same set and
fight" failure mode revenuecat's README warns about for its attachment
resources — here each membership resource owns exactly one link, not a
set, so there's nothing to fight over.

## Provider configuration

```hcl
provider "glitchtip" {
  endpoint = "https://app.glitchtip.com"  # default; override for self-hosted
  token    = var.glitchtip_token           # sensitive; falls back to GLITCHTIP_TOKEN
  insecure = false                          # allow plain HTTP, local dev only
}
```

- `endpoint` — optional, defaults to `https://app.glitchtip.com` (the
  hosted SaaS). Self-hosted users override it. This differs from
  bugsink, where `endpoint` is required with no default, because
  GlitchTip's hosted offering is a first-class, commonly-used target and
  Bugsink has none.
- `token` — sensitive, optional in config, required overall (falls back
  to `GLITCHTIP_TOKEN` env var, errors if both are absent).
- `insecure` — optional bool, default false; permits `http://` endpoints
  for local Docker development only, never a production target.

At `Configure` time the client fetches `{endpoint}/api/openapi.json` (or
the lightest available version-bearing endpoint) and validates it's a
GlitchTip instance the client's wire format was written against,
following bugsink's `checkVersion` pattern — fail closed rather than
send requests whose shape the server doesn't recognize.

## Client package (`src/glitchtip`)

Hand-written HTTP client, zero Terraform Plugin Framework dependency
(constructible and testable standalone), following bugsink's
`src/bugsink/client.go` shape:

- `Config{Endpoint, Token, Timeout, AllowInsecure, HTTPClient}`, `New(ctx,
  cfg) (*Client, error)` validates config, resolves the base URL, strips
  any embedded userinfo, checks the remote version, returns a ready
  client.
- `do(ctx, method, path, body, out)` — single low-level request method:
  sets `Authorization: Bearer`, `Accept: application/json`,
  `User-Agent`; enforces per-request timeout via context; reads response
  bodies through a bounded `io.LimitReader`; decodes JSON into `out`;
  wraps non-2xx as `*APIError` with status/body/method/URL.
  Credential-safe by construction — `baseURL` never carries userinfo,
  and errors are built only from method/URL/status/body, never from
  request headers.
- `resolve(path)` — relative paths resolve against the base URL;
  absolute pagination URLs returned by the server are only followed if
  they match the configured scheme+host and carry no embedded
  credentials (same anti-redirect protection as bugsink, needed because
  GlitchTip's list endpoints also paginate via `Link` headers / `next`
  cursors).
- Retry: unlike bugsink (no retry) but like revenuecat, retry
  idempotent requests (GET, and PUT/DELETE by resource ID) on 429 and
  5xx with capped exponential backoff, `WithMaxRetries` /
  `WithTimeout` functional options mirroring revenuecat's client
  constructor. GlitchTip's hosted SaaS is more likely to rate-limit
  than a self-hosted Bugsink instance, so this is worth carrying over.
- One file per resource family: `organizations.go`, `teams.go`,
  `projects.go`, `project_keys.go`, `project_team_memberships.go`. Each
  exposes typed `List/Get/Create/Update/Delete` (or the applicable
  subset) plus request/response structs, matching bugsink's
  `teams.go`/`projects.go` split.
- `errors.go` — `*ConfigError` (bad local configuration) and `*APIError`
  (bad response), with an `APIError.NotFound()` helper the resource
  `Read` methods use to detect out-of-band deletion and call
  `resp.State.RemoveResource(ctx)`.

## Provider & resource layer (`src/provider`)

Standard terraform-plugin-framework shape, one file per resource,
matching bugsink's `team_resource.go`:

- `provider.go` — schema, `Configure` (env-var fallback, error
  diagnostics for missing token/endpoint, constructs `*glitchtip.Client`,
  stores it as `DataSourceData`/`ResourceData`), `Resources()` returning
  all five `New*Resource` factories, `DataSources()` returning `nil` for
  v1.
- Each resource: `Metadata`, `Schema` (with `MarkdownDescription` on
  every attribute — these generate the Registry docs via `tfplugindocs`),
  `Configure` (type-asserts `*glitchtip.Client`), `Create`/`Read`/
  `Update`/`Delete`, `ImportState` via
  `resource.ImportStatePassthroughID` where the import identifier is a
  single ID, or a small custom `ImportState` that splits a
  colon-delimited compound identifier (organization/team/project scoped
  resources) — following revenuecat's `proj1abc:entl1xyz` convention,
  adapted to GlitchTip's slug-based addressing, e.g.
  `org_slug:project_slug:team_slug` for `glitchtip_project_team_
  membership`.
- Unlike bugsink's `TeamResource.Delete`, every resource here has a real
  `Delete` that calls the API and returns a normal diagnostic on
  failure — no hard-fail-by-design caveat, because GlitchTip has real
  delete endpoints.
- Plan modifiers: `id`/`slug` use `UseStateForUnknown` (server-assigned,
  never changes after create); `organization_slug`,
  `initial_team`, and the three identity fields on
  `glitchtip_project_team_membership` use `RequiresReplace`.
- `dsn_public`/`dsn_security` on `glitchtip_project_key` are `Computed`
  + `Sensitive`, matching bugsink's DSN-as-sensitive-state stance.

## Error handling & drift

- Every `Read` method checks `APIError.NotFound()` and calls
  `resp.State.RemoveResource(ctx)` — out-of-band deletion in the
  GlitchTip UI shows as a clean "will be created" plan, not a crash.
- `Update` sends only changed fields (PATCH-style partial update
  request structs with pointer fields, `omitempty`), matching bugsink's
  `UpdateTeamRequest` shape, so an unrelated field a practitioner never
  set is never accidentally clobbered.
- Deleting `glitchtip_organization` while it still owns teams/projects:
  GlitchTip's own API decides whether that cascades or errors; the
  provider does not attempt to enforce an ordering beyond what
  Terraform's own dependency graph already gives it from resource
  references (a project referencing a team's `id` already makes
  Terraform destroy the project first).

## Testing strategy

Following bugsink (not revenuecat) as the primary approach, because
GlitchTip — like Bugsink — ships an official `docker-compose.yml` for a
disposable local instance and the user's stated preference is
real-instance verification over an API contract inferred from docs:

| Layer | Command | Needs | Catches |
| --- | --- | --- | --- |
| Unit | `go test ./src/...` | nothing | client request shapes, error decoding, schema rules |
| Acceptance | `tests/acceptance` via `up.sh`/`down.sh` | Docker | real create/read/update/delete/import/drift against a real GlitchTip instance |

`tests/acceptance/docker-compose.yml` starts a disposable GlitchTip
stack (web + worker + Postgres + Redis, per GlitchTip's own compose
file) seeded with an org and an auth token; `up.sh`/`down.sh` wrap
startup/teardown the same way bugsink's do. `provider_acceptance_test.go`
drives real `terraform` runs (via `terraform-exec`, already a bugsink
dependency) through the full lifecycle of all five resources plus
import.

No mock server, no `internal/mockrevenuecat`-equivalent — the two
sibling providers disagree here, and this design picks bugsink's
approach deliberately: the disposable-Docker path is proven, the
instance is genuinely light to run in CI, and skipping a hand-maintained
mock removes an entire class of "mock and reality drift apart" risk that
revenuecat's own README calls out as its main known weakness.

CI (`test.yml`): `gofmt -l .`, `go build ./...`, `go vet ./...`,
`go test -race ./src/... ./tests/unit/...`, then the acceptance job
(skipped on fork PRs) spinning up Docker GlitchTip and running
`tests/acceptance`.

## Release pipeline

Identical to bugsink, since the user approved the Bugsink model and
Registry publication is a stated goal:

- `GitVersion.yml` — Mainline mode, patch increment on `main`.
- `.github/workflows/release.yml` — GitVersion computes next SemVer,
  tags `main` on push, GoReleaser builds/signs/publishes in the same
  job (avoiding the tag-push-doesn't-retrigger-workflows trap bugsink's
  own workflow comment documents).
- `.goreleaser.yml` — `dir: src`, CGO disabled, darwin/linux/windows ×
  amd64/arm64 (windows/arm64 excluded), zip archives, GPG-signed
  checksums, `terraform-registry-manifest.json` (protocol 6) attached to
  every release.
- `terraform-registry-manifest.json` — `{"version": 1, "metadata":
  {"protocol_versions": ["6.0"]}}`.
- Provider address: `registry.terraform.io/nmehlei/glitchtip`.
- Docs generated via `tfplugindocs` (`go:generate` directive in
  `main.go`, same as bugsink).

## Repository layout

```
terraform-provider-glitchtip/
├── src/
│   ├── main.go
│   ├── glitchtip/          # HTTP client, zero TF deps
│   │   ├── client.go
│   │   ├── errors.go
│   │   ├── organizations.go
│   │   ├── teams.go
│   │   ├── projects.go
│   │   ├── project_keys.go
│   │   └── project_team_memberships.go
│   └── provider/
│       ├── provider.go
│       ├── organization_resource.go
│       ├── team_resource.go
│       ├── project_resource.go
│       ├── project_key_resource.go
│       └── project_team_membership_resource.go
├── tests/
│   ├── unit/
│   └── acceptance/
│       ├── docker-compose.yml
│       ├── up.sh / down.sh
│       └── provider_acceptance_test.go
├── docs/                    # generated by tfplugindocs + this design doc
├── examples/
│   ├── provider/provider.tf
│   └── resources/glitchtip_*/{resource.tf,import.sh}
├── openspec/                # spec-driven change tracking, per sibling repos
├── .github/workflows/{test,release}.yml
├── .goreleaser.yml
├── GitVersion.yml
├── terraform-registry-manifest.json
├── go.mod
└── AGENTS.md
```

## Alternatives considered

- **RevenueCat model (mock API + no real instance)**: rejected per the
  user's explicit choice. GlitchTip's official docker-compose makes a
  real disposable instance cheap, removing the main reason revenuecat
  reached for a mock (no accessible real API in that session's network).
- **`teams` as an inline set on `glitchtip_project`** (Approach B):
  rejected — couples project CRUD to membership diffing and makes
  partial-membership import impossible; the attachment-resource split
  is simpler per-resource even though it's one more resource type
  overall.
- **Single-team-only projects** (Approach C): rejected — sheds real
  GlitchTip capability (multi-team projects) and makes moving a project
  between teams destructive with no upgrade path later without a
  breaking schema change.
- **Data sources in v1**: deferred — every v1 resource is fully
  manageable, so there's no read-only-reference need yet (unlike
  revenuecat, where `project` genuinely cannot be created via API).

## Open questions for implementation

None blocking — the API surface needed for v1 (org/team/project/key CRUD
plus project-team assign/remove) is documented Sentry-compatible
behavior. The usual "verify against the real instance" caveat applies:
exact field names for `platform` enum values and `event_throttle_rate`
semantics should be confirmed against a running GlitchTip instance's
OpenAPI schema during implementation, the same way bugsink's client
notes its fields are "per docs/api-contract.md's canonical fields" — if
the schema disagrees with this design's assumed field names, the client
package is where that correction is isolated (single source of wire
truth), same guarantee revenuecat's README makes for its own client.
