# GlitchTip API wire-format notes (observed ground truth)

This document records the **actual observed** request/response shapes from a
disposable local GlitchTip instance, brought up via
`tests/acceptance/docker-compose.yml` + `up.sh` / `down.sh`. It exists because
the design doc's assumed field names, paths and status codes (taken from
GlitchTip/Sentry-compatible documentation) had not been verified against a
live instance.

**Every later client task MUST re-check its section here before writing code
and treat this file — not the design doc's guesses — as the authority.**

- GlitchTip version observed: **v6.2.6** (`glitchtip/glitchtip:latest`, pulled 2026-09-10)
- API framework: **django-ninja** (`"openapi": "3.1.0"`, `SettingsOut`/`*Schema`/`*In` pydantic models)
- Base URL used: `http://localhost:8000`
- All API responses are **camelCase JSON**, even though the underlying Django
  model fields are snake_case. This is the single most important finding:
  the design doc assumed snake_case (`event_throttle_rate`); the wire format
  is `eventThrottleRate`.

---

## Setup notes / deviations from the brief

### Compose: healthcheck (`web`) — CHANGED

The brief's `web` healthcheck used
`["CMD", "curl", "-f", "http://localhost:8000/_health/"]`. The
`glitchtip/glitchtip:latest` image does **not** ship a `curl` binary
(`docker inspect` health log showed
`exec: "curl": executable file not found in $PATH`), so the check failed
forever even though the app itself was fine (`curl` from the **host** to
`http://localhost:8000/_health/` returned `200`). Replaced with the
`python3` binary that **is** present in the image:

```yaml
test: ["CMD", "python3", "-c", "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:8000/_health/').status == 200 else 1)"]
```

### Compose: `migrate` step — ADDED (`web` + `worker` command wrappers unchanged, but DB must be migrated)

The `glitchtip/glitchtip` image's `./bin/start.sh` only runs
`./manage.py migrate` when the Heroku-specific `DYNO` env var matches `web*`:

```sh
case "$HEROKU_DYNO" in
    web*) ./manage.py migrate ;;
    worker*) SERVER_ROLE=worker ;;
esac
```

With the brief's compose file `DYNO` is unset, so **migrations never run** and
every API/ORM call fails with `relation "users_user" does not exist`.

Fix applied in `docker-compose.yml`: added `DYNO: "web.1"` to the `web`
service environment so the image's own entrypoint migrates on boot. (A
one-off `docker compose exec -T web ./manage.py migrate` also works and is
what was run during this exploration before the compose file was updated.)
Migration takes ~4-6 minutes on first boot (it builds partitioned tables /
UUIDv7 daily partitions for `issue_events`, `logs`, `performance`).

### `up.sh`: `timeout` binary — CHANGED

The brief's `up.sh` used GNU `timeout`, not installed by default on macOS.
Replaced `timeout 180 bash -c '...'` with a manual bounded polling loop with
the same 180s bound and the same success condition
(`docker inspect -f '{{.State.Health.Status}}'` == `healthy`).

Everything else (image tags, other env vars, port mapping, `down.sh` =
`docker compose down -v`) matches the brief verbatim.

---

## Step 3: first user + org + API token — AUTOMATED via Django shell (not the UI)

The brief's Step 3 assumed a human clicking through the web UI. Automated
instead via `manage.py shell`, run inside the `web` container. **Later tasks'
acceptance harness should reuse this exact pattern.**

```bash
cd tests/acceptance

# 1. superuser
docker compose exec -T web ./manage.py shell -c "
from django.contrib.auth import get_user_model
User = get_user_model()
u, created = User.objects.get_or_create(
    email='acceptance@example.com',
    defaults={'is_superuser': True, 'is_staff': True, 'is_active': True})
if created:
    u.set_password('acceptance-test-pw-12345'); u.save()
print(created, u.pk)
"

# 2. organization (registration UI would auto-create one; we create it
#    explicitly). Organization.add_user() promotes the FIRST member to OWNER.
docker compose exec -T web ./manage.py shell -c "
from apps.organizations_ext.models import Organization
from django.contrib.auth import get_user_model
u = get_user_model().objects.get(email='acceptance@example.com')
org, _ = Organization.objects.get_or_create(name='acceptance-org', defaults={'slug': 'acceptance-org'})
org.add_user(u)   # role 3 == OWNER (see apps.organizations_ext.constants.OrganizationUserRole)
print(org.slug, list(org.organization_users.values_list('user__email','role')))
"

# 3. API token. NOTE: APIToken.scopes is a django-bitfield BitField, NOT a
#    list. Passing a list to objects.create raises TypeError. Use the model's
#    own add_permissions() helper.
docker compose exec -T web ./manage.py shell -c "
from apps.api_tokens.models import APIToken
from django.contrib.auth import get_user_model
u = get_user_model().objects.get(email='acceptance@example.com')
t, created = APIToken.objects.get_or_create(user=u, label='acceptance-cli')
if created:
    t.add_permissions(['project:read','project:write','project:admin','project:releases',
        'team:read','team:write','team:admin','event:read','event:write','event:admin',
        'org:read','org:write','org:admin','member:read','member:write','member:admin'])
t.refresh_from_db()
print(t.token, t.get_scopes())
"
```

- `APIToken` fields: `id, created, token, user, label, scopes`
- `token` = `binascii.hexlify(os.urandom(32))` → **64 lowercase hex chars**
- Available scope flags (bitfield order): `project:read, project:write,
  project:admin, project:releases, team:read, team:write, team:admin,
  event:read, event:write, event:admin, org:read, org:write, org:admin,
  member:read, member:write, member:admin`
- **Auth header that works:** `Authorization: Bearer <token>`
- The token minted in this run: `<redacted-64hex>` (disposable — the stack is
  torn down with `down.sh -v`, nothing persists; redacted here as a habit,
  not because it's live)

---

## Authentication

- `Authorization: Bearer <64-hex-token>` — works for all `/api/0/...`
  resource endpoints probed below.
- `GET /api/0/api-tokens/` → **`401 {"detail": "Unauthorized"}`** with a
  Bearer token. That endpoint (self-service token management) requires a
  **session cookie**, not token auth. The provider cannot manage its own
  tokens over the API — tokens must be pre-provisioned.
- `GET /api/openapi.json` → **`200`**, ~266 KB, `openapi: 3.1.0`,
  `info.title: "GlitchTip API"`. No `servers` array. This is the machine
  authority for request/response schemas; a copy of the relevant schema
  fragments is inlined below.

---

## Pagination

List endpoints return a **`Link` header**, plus `X-Max-Hits` and `X-Hits`:

```
link: {'<http://localhost:8000/api/0/organizations/acceptance-org/teams/>; rel="previous"; results="false", <http://localhost:8000/api/0/organizations/acceptance-org/teams/>; rel="next"; results="false"'}
x-max-hits: 1000
x-hits: 2
```

Observations / gotchas:

- ⚠️ The `Link` header value is wrapped in `{'...'}` — a **Python set-literal
  repr leaking into the header** (GlitchTip quirk, present in v6.2.6). A
  parser must strip the leading `{'` and trailing `'}` before RFC-8288
  parsing, or match `<url>; rel="next"; results="..."` with a regex.
- Each entry carries a non-standard `results="true"|"false"` param.
  `results="false"` on the `next` rel means **there is no next page**. This
  is the reliable "has more pages" signal — not the mere presence of the
  `next` link (it is always present).
- `X-Hits` = number of rows in the current response window; `X-Max-Hits` =
  server cap (1000).
- Body is a **bare JSON array** (`[ {...}, {...} ]`), not an envelope.
- Could not force a >1-page response with this small dataset; the `next`
  link with `results="false"` is what a 1-page result looks like.

---

## 404 shape (for NotFound detection)

```
GET /api/0/projects/acceptance-org/does-not-exist/   -> 404  {"detail": "Not Found"}
GET /api/0/organizations/nope-nope/                  -> 404  {"detail": "Not Found"}
```

Body is always exactly `{"detail": "Not Found"}`. Validation errors use a
different shape (django-ninja):

```
POST /api/0/organizations/acceptance-org/teams/  -d '{"name":"platform"}'
-> 422  {"detail": [{"type": "missing", "loc": ["body", "payload", "slug"], "msg": "Field required"}]}
```

So: **404 → `{"detail": <string>}`**, **422 → `{"detail": <array of objects>}`**.
Detect NotFound on status code 404, not body parsing.

---

## Organizations

### `POST /api/0/organizations/` → 201 (create)

Probed 2026-09-10 (controller-run, after the initial exploration) against a
fresh instance with a Bearer token belonging to a user with **no**
organization yet:

```
POST /api/0/organizations/  -d '{"name":"probe-org"}'   -> 201
```

Response is the full `OrganizationDetailSchema` (same shape as the detail
GET below — `projects`, `teams`, `access`, `openMembership` all present):

```json
{"name": "probe-org", "id": "1", "dateCreated": "2026-09-10T18:28:39.040Z",
 "status": {"id": "active", "name": "active"},
 "avatar": {"avatarType": "", "avatarUuid": null},
 "isEarlyAdopter": false, "require2fa": false, "slug": "probe-org",
 "isAcceptingEvents": true, "eventThrottleRate": 0,
 "projects": [], "teams": [], "access": [ ...token scopes... ],
 "openMembership": true}
```

- Request body: `{"name": "..."}` (same `OrganizationInSchema` as PUT). `slug`
  is **server-derived from `name`** (`probe-org`), not accepted in the body.
- The creating user is auto-added as OWNER of the new org (the `access`
  array in the response held the full scope set).
- So `glitchtip_organization` **can** be a full CRUD resource — Task 4's
  `CreateOrganizationRequest{Name string}` design is valid.

### `DELETE /api/0/organizations/{slug}/` → 204 (no body)

Probed 2026-09-10 (controller-run). `DELETE /api/0/organizations/probe-org/`
→ **204**, empty body; a subsequent `GET /api/0/organizations/` returned
`[]`. Deletion is immediate (no soft-delete / pending-deletion state
observed at this API surface).

### `GET /api/0/organizations/` → 200 (list)

```json
[{"name": "acceptance-org", "id": "1", "dateCreated": "2026-09-10T16:03:25.579Z",
  "status": {"id": "active", "name": "active"},
  "avatar": {"avatarType": "", "avatarUuid": null},
  "isEarlyAdopter": false, "require2fa": false, "slug": "acceptance-org",
  "isAcceptingEvents": true, "eventThrottleRate": 0}]
```

### `GET /api/0/organizations/{slug}/` → 200 (detail — `OrganizationDetailSchema`)

Adds `projects[]`, `teams[]`, `access[]` (list of scope strings the token
holds), `openMembership`:

```json
{"name": "acceptance-org", "id": "1", "dateCreated": "...",
 "status": {"id": "active", "name": "active"},
 "avatar": {"avatarType": "", "avatarUuid": null},
 "isEarlyAdopter": false, "require2fa": false, "slug": "acceptance-org",
 "isAcceptingEvents": true, "eventThrottleRate": 0,
 "projects": [], "teams": [],
 "access": ["member:write","event:write", ... ,"org:admin"],
 "openMembership": true}
```

### `PUT /api/0/organizations/{slug}/` → 200

Request body (`OrganizationInSchema`): **`{"name": "..."}` only** — `name`
is the *only* writable field over the API.

```
PUT ... -d '{"name":"acceptance-org-renamed"}'  -> 200
```

- ⚠️ **`slug` is immutable via the API.** After renaming `name` to
  `acceptance-org-renamed`, the response still had `"slug": "acceptance-org"`.
  The Terraform resource must treat org slug as create-time / computed, not
  updatable, and must not send it in updates.
- Response is the full `OrganizationDetailSchema` (with nested
  `projects[].teams[]`).

### Field name mapping (model → wire)

| Django model field      | Wire (JSON) key       | Notes |
|-------------------------|-----------------------|-------|
| `name`                  | `name`                | writable |
| `slug`                  | `slug`                | read-only over API |
| `is_accepting_events`   | `isAcceptingEvents`   | read-only over API |
| `event_throttle_rate`   | `eventThrottleRate`   | integer, percent 0-100; read-only over API at org level |
| `open_membership`       | `openMembership`      | read-only over API |
| `created`               | `dateCreated`         | ISO-8601, `Z` suffix, ms precision |
| `id`                    | `id`                  | **string**, not int |

---

## Teams

### `POST /api/0/organizations/{orgSlug}/teams/` → 201

⚠️ **Request body is `{"slug": "..."}` — NOT `{"name": ...}`.** The brief
assumed `name`. `TeamIn` schema: `slug` required, `maxLength: 50`,
`pattern: ^[-a-zA-Z0-9_]+$`. Teams have **no `name` field at all**.

```
POST -d '{"name":"platform"}'  -> 422 {"detail":[{"type":"missing","loc":["body","payload","slug"],"msg":"Field required"}]}
POST -d '{"slug":"platform"}'  -> 201
```

Response (`TeamSchema`):

```json
{"id": "1", "slug": "platform", "dateCreated": "2026-09-10T16:04:41.634Z",
 "isMember": true, "memberCount": 1, "projects": []}
```

### `GET /api/0/organizations/{orgSlug}/teams/` → 200 (list)

```json
[{"id": "1", "slug": "platform", "dateCreated": "...", "isMember": true, "memberCount": 1, "projects": []}]
```

### `GET /api/0/teams/{orgSlug}/{teamSlug}/` → 200

Same as list item plus `projects[]` populated with full `ProjectSchema`
objects.

### `PUT /api/0/teams/{orgSlug}/{teamSlug}/` → 200

Body = `TeamIn` = `{"slug": "..."}`.

- ✅ **Team slug IS mutable.** `PUT -d '{"slug":"platform-renamed"}'` → 200,
  response `"slug": "platform-renamed"`. (Contrast with org slug.) The
  Terraform resource must then address the team by its new slug.

### `DELETE /api/0/teams/{orgSlug}/{teamSlug}/` → **204** (no body)

### Field name mapping (team)

| model field   | wire key      | notes |
|---------------|---------------|-------|
| `slug`        | `slug`        | the only writable field; mutable |
| `id`          | `id`          | string |
| `created`     | `dateCreated` | |
| —             | `isMember`    | computed, per-token |
| —             | `memberCount` | computed |
| —             | `projects`    | only on detail GET / PUT / create responses |

---

## Projects

### `POST /api/0/teams/{orgSlug}/{teamSlug}/projects/` → 201

Body (`ProjectIn`): `name` **required** (`maxLength: 64`); `slug`, `platform`,
`eventThrottleRate` optional-nullable. `name` matched the brief.

```
POST -d '{"name":"checkout","platform":"python"}'  -> 201
```

Response (`ProjectSchema`):

```json
{"name": "checkout", "slug": "checkout", "id": "1",
 "avatar": {"avatarType": "", "avatarUuid": null}, "color": "",
 "features": [], "hasAccess": true, "isBookmarked": false, "isInternal": false,
 "isMember": true, "isPublic": false, "scrubIPAddresses": true,
 "dateCreated": "2026-09-10T17:09:30.946Z", "platform": "python",
 "firstEvent": null, "eventThrottleRate": 0}
```

- `slug` is auto-derived from `name` when omitted.
- ⚠️ **Creating a project auto-creates one default project key (DSN)** with
  `name: ""` — see Project Keys below. Terraform must not assume "0 keys after
  create".

### `GET /api/0/projects/{orgSlug}/{projectSlug}/` → 200 (`ProjectOrganizationSchema`)

Same fields as create response **plus a nested `organization` object**, and
⚠️ **NO `teams` key**:

```
keys = [avatar, color, dateCreated, eventThrottleRate, features, firstEvent,
        hasAccess, id, isBookmarked, isInternal, isMember, isPublic, name,
        organization, platform, scrubIPAddresses, slug]
```

To read a project's team assignments you must use
`GET /api/0/projects/{orgSlug}/{projectSlug}/teams/` (see below) or the
org-level project list (which *does* include `teams`). The plain project GET
does not carry them.

### `GET /api/0/organizations/{orgSlug}/projects/` → 200 (list)

Bare array of `ProjectSchema`, **each augmented with a `teams` array**:

```json
[{ ...ProjectSchema..., "teams": [{"id": "1", "slug": "platform"}]}]
```

### `GET /api/0/teams/{orgSlug}/{teamSlug}/projects/` → 200

Bare array of `ProjectSchema` (no `teams` key on the items).

### `PUT /api/0/projects/{orgSlug}/{projectSlug}/` → 200

Body = `ProjectIn` (`name` required; `slug`, `platform`, `eventThrottleRate`
optional).

```
PUT -d '{"name":"checkout","platform":"node","eventThrottleRate":10}'  -> 200
```

- ✅ `platform` and `eventThrottleRate` **are** writable at the project level
  (unlike at the org level). Response echoed `"platform": "node",
  "eventThrottleRate": 10`.
- `slug` unchanged by a `name`-only rename in this test (not separately
  verified as mutable — treat with caution, prefer setting `slug` explicitly
  at create).
- Response is `ProjectOrganizationSchema` (nested `organization`, no `teams`).

### `DELETE /api/0/projects/{orgSlug}/{projectSlug}/` → 204 (no body)

Probed 2026-09-10 (controller-run): created `probe-org/t1/p1` then
`DELETE /api/0/projects/probe-org/p1/` → **204**, empty body. Matches
teams/keys; contrast with project↔team unassign (200 + body).

### Field name mapping (project)

| model field          | wire key            | notes |
|----------------------|---------------------|-------|
| `name`               | `name`              | required on create, `maxLength: 64` |
| `slug`               | `slug`              | auto from name; writable on create |
| `platform`           | `platform`          | nullable string, writable via PUT |
| `event_throttle_rate`| `eventThrottleRate` | int percent, writable via PUT |
| `scrub_ip_addresses` | `scrubIPAddresses`  | bool, not in `ProjectIn` (read-only over this endpoint) |
| `id`                 | `id`                | string |
| `created`            | `dateCreated`       | |
| —                    | `firstEvent`        | nullable datetime, computed |
| —                    | `organization`      | nested obj, detail GET / PUT only |
| —                    | `color`, `features`, `hasAccess`, `isBookmarked`, `isInternal`, `isMember`, `isPublic` | computed |

---

## Project Keys (DSN)

### `GET /api/0/projects/{orgSlug}/{projectSlug}/keys/` → 200 (list)

One key already exists immediately after project creation:

```json
[{"name": "", "rateLimit": null, "dateCreated": "2026-09-10T17:09:33.765Z",
  "id": "2c349102-9f9a-45d0-8842-1daa80ef216c",
  "dsn": {
    "public": "http://2c3491029f9a45d088421daa80ef216c@localhost:8000/1",
    "secret": "http://2c3491029f9a45d088421daa80ef216c@localhost:8000/1",
    "security": "http://localhost:8000/api/1/security/?glitchtip_key=2c3491029f9a45d088421daa80ef216c"},
  "label": "", "public": "2c349102-9f9a-45d0-8842-1daa80ef216c", "projectID": 1}]
```

### `POST /api/0/projects/{orgSlug}/{projectSlug}/keys/` → 201

Body (`ProjectKeyIn`): `name` (nullable string), `rateLimit` (nullable
`{count, window}` object). `{"name":"default"}` worked.

```json
{"name": "default", "rateLimit": null, "dateCreated": "2026-09-10T17:10:09.373Z",
 "id": "cb3eb2a2-21d8-406c-baf2-6e0838fed46e",
 "dsn": {
   "public": "http://cb3eb2a221d8406cbaf26e0838fed46e@localhost:8000/1",
   "secret": "http://cb3eb2a221d8406cbaf26e0838fed46e@localhost:8000/1",
   "security": "http://localhost:8000/api/1/security/?glitchtip_key=cb3eb2a221d8406cbaf26e0838fed46e"},
 "label": "default", "public": "cb3eb2a2-21d8-406c-baf2-6e0838fed46e", "projectID": 1}
```

DSN shape notes (design doc flagged this as assumed):

- `id` and `public` are the **same UUID** (canonical hyphenated form).
- The DSN URL embeds the UUID with **hyphens stripped** (32 hex chars), then
  `@<host>/<numeric project id>`. `projectID` is a **number** here (contrast
  with `id` being a string everywhere else).
- `dsn.public` and `dsn.secret` are **identical** in v6.2.6 (GlitchTip has no
  separate secret half; kept for Sentry compat).
- `dsn.security` is the CSP-report endpoint, `?glitchtip_key=<hyphenless uuid>`.
- `name` in the request maps to **both** `name` and `label` in the response
  (they mirror). Auto-created key has both as `""`.
- `rateLimit` is `null` or `{"count": <int>, "window": <int>}` (`KeyRateLimit`).

### `DELETE /api/0/projects/{orgSlug}/{projectSlug}/keys/{keyId}/` → **204** (no body)

`{keyId}` = the hyphenated UUID (the `id` / `public` value).

### Field name mapping (project key)

| wire key      | notes |
|---------------|-------|
| `id`          | UUID string (hyphenated) — the resource identifier in the URL |
| `public`      | same UUID as `id` |
| `name`        | writable; mirrors `label` |
| `label`       | echo of `name` |
| `rateLimit`   | `null` or `{count, window}` |
| `dsn`         | `{public, secret, security}` — all strings, public==secret |
| `projectID`   | **integer** |
| `dateCreated` | |

---

## Project ⇄ Team membership

### `POST /api/0/projects/{orgSlug}/{projectSlug}/teams/{teamSlug}/` → **201**

No request body. Response is the **project** (`ProjectSchema`) **with a
`teams` array** reflecting the new membership:

```json
{ ...ProjectSchema..., "teams": [{"id": "1", "slug": "platform"}, {"id": "2", "slug": "sre"}]}
```

### `GET /api/0/projects/{orgSlug}/{projectSlug}/teams/` → 200

Bare array of `TeamSchema`-ish items (`projects` key omitted):

```json
[{"id": "1", "slug": "platform", "dateCreated": "...", "isMember": true, "memberCount": 1}]
```

### `DELETE /api/0/projects/{orgSlug}/{projectSlug}/teams/{teamSlug}/` → **200** (WITH body)

⚠️ **Not 204.** Unlike deleting a team or a key, un-assigning a team from a
project returns **`200` with the updated project object** (`ProjectTeamSchema`
per OpenAPI; observed body is the project with the shrunken `teams` array):

```json
{ ...ProjectSchema..., "teams": [{"id": "1", "slug": "platform"}]}
```

So DELETE status codes are **not uniform**:

| operation                         | DELETE status | body |
|-----------------------------------|---------------|------|
| team (`/api/0/teams/{o}/{t}/`)     | 204           | none |
| project key                       | 204           | none |
| project↔team unassign             | **200**       | updated project JSON |

A generic "DELETE succeeded if 204" check would misfire on project↔team
unassign. Accept `200` and `204` (and `404` as already-gone) for delete
operations.

---

## Raw command log (this exploration)

```bash
# --- bring up ---
tests/acceptance/up.sh                      # (after the compose fixes above)
# one-off migrate was run before DYNO was added to compose:
docker compose exec -T web ./manage.py migrate

# --- seed (see Step 3 section for full shell snippets) ---
docker compose exec -T web ./manage.py shell -c "<create superuser>"
docker compose exec -T web ./manage.py shell -c "<create org + add_user>"
docker compose exec -T web ./manage.py shell -c "<create APIToken + add_permissions>"

export GT_TOKEN=<redacted-64hex>
export GT_ORG=acceptance-org

curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/$GT_ORG/
curl -sS -X PUT -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"name":"acceptance-org-renamed"}' http://localhost:8000/api/0/organizations/$GT_ORG/

curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"name":"platform"}' http://localhost:8000/api/0/organizations/$GT_ORG/teams/   # 422
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"slug":"platform"}' http://localhost:8000/api/0/organizations/$GT_ORG/teams/   # 201
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"slug":"sre"}' http://localhost:8000/api/0/organizations/$GT_ORG/teams/
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/$GT_ORG/teams/
curl -sS -X PUT -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"slug":"platform-renamed"}' http://localhost:8000/api/0/teams/$GT_ORG/platform/

curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"name":"checkout","platform":"python"}' http://localhost:8000/api/0/teams/$GT_ORG/platform/projects/
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/checkout/
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/$GT_ORG/projects/
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/teams/$GT_ORG/platform/projects/
curl -sS -X PUT -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"name":"checkout","platform":"node","eventThrottleRate":10}' http://localhost:8000/api/0/projects/$GT_ORG/checkout/

curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/checkout/keys/
curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" -H 'Content-Type: application/json' -d '{"name":"default"}' http://localhost:8000/api/0/projects/$GT_ORG/checkout/keys/
curl -sS -X DELETE -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/checkout/keys/<uuid>/   # 204

curl -sS -X POST -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/checkout/teams/sre/       # 201, returns project+teams
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/checkout/teams/
curl -sS -X DELETE -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/checkout/teams/sre/     # 200, returns project+teams
curl -sS -X DELETE -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/teams/$GT_ORG/sre/                       # 204

curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/projects/$GT_ORG/does-not-exist/    # 404 {"detail":"Not Found"}
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/nope-nope/            # 404
curl -sSD - -o /dev/null -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/organizations/$GT_ORG/teams/   # Link header
curl -sS -o /dev/null -w '%{http_code}' http://localhost:8000/api/openapi.json    # 200
curl -sS -H "Authorization: Bearer $GT_TOKEN" http://localhost:8000/api/0/api-tokens/    # 401 {"detail":"Unauthorized"}

# --- tear down ---
tests/acceptance/down.sh
```

### Follow-up probe (2026-09-10, controller-run, fresh instance)

Run to close the gaps the first exploration left open (org create/delete,
project delete). A superuser + token were seeded with **no org**, then:

```bash
curl -X POST -H "Authorization: Bearer $TOK" -d '{"name":"probe-org"}' \
  http://localhost:8000/api/0/organizations/                                  # 201, full detail schema, slug="probe-org"
curl -X PUT  -H "Authorization: Bearer $TOK" -d '{"name":"probe-org-renamed"}' \
  http://localhost:8000/api/0/organizations/probe-org/                        # 200, slug unchanged (immutable)
curl -X POST -H "Authorization: Bearer $TOK" -d '{"slug":"t1"}' \
  http://localhost:8000/api/0/organizations/probe-org/teams/                  # 201
curl -X POST -H "Authorization: Bearer $TOK" -d '{"name":"p1","platform":"python"}' \
  http://localhost:8000/api/0/teams/probe-org/t1/projects/                    # 201
curl -X DELETE -H "Authorization: Bearer $TOK" \
  http://localhost:8000/api/0/projects/probe-org/p1/                         # 204, empty body
curl -X DELETE -H "Authorization: Bearer $TOK" \
  http://localhost:8000/api/0/organizations/probe-org/                       # 204, empty body
curl -H "Authorization: Bearer $TOK" http://localhost:8000/api/0/organizations/   # [] — org gone
```

Stack torn down with `tests/acceptance/down.sh` afterwards.

## Verification of the compose file

The committed `docker-compose.yml` (curl→python3 healthcheck + `DYNO=web.1`)
was observed bringing the `web` service to `healthy` **from a cold
`down -v` state with no manual migrate step** — the follow-up probe above
ran against exactly such a cold-booted stack (`web` reported `healthy`
~350s after `up.sh`, first-boot migrations included). ⚠️ Because that
~350s exceeds `up.sh`'s 180s health-poll bound, `up.sh` itself exited
non-zero on the first cold run and the stack had to be re-checked after
migrations finished; the compose file and healthcheck are correct, but
`up.sh`'s 180s bound is too tight for a truly cold boot (image pulled,
DB unmigrated). Raise the bound or expect one manual re-run on a cold
machine — left at 180s here per the brief.

---

## Task 16 addendum: real-instance acceptance run findings

Run 2026-09-11, controller-run, against the compose stack in this repo.

- ⚠️ **Compose project name collision on a shared Docker daemon.** `up.sh`
  originally ran plain `docker compose up -d` from `tests/acceptance/`,
  which defaults the compose project name to the directory basename
  (`acceptance`). On this shared environment that collided with an
  unrelated, pre-existing compose stack also named `acceptance` (different
  services entirely — `aptabase-plus`, `clickhouse`, `mailcatcher` — plus a
  `postgres` container using a Postgres **15** data volume, incompatible
  with this stack's `postgres:16` image: `FATAL: database files are
  incompatible with server`). Fixed by pinning
  `COMPOSE_PROJECT_NAME=glitchtip-tf-acceptance` once, in
  `tests/acceptance/.env` (auto-loaded by `docker compose` from that
  directory — both `up.sh` and `down.sh` `cd` there first) — a single
  source of truth honored by any `docker compose` invocation against this
  stack, rather than duplicating the pin as a literal `export` in both
  scripts (the review round on this task flagged that duplication as the
  same failure mode reopening silently on drift). Note: the more obvious
  fix — a top-level `name:` key in `docker-compose.yml` itself, the modern
  Compose Specification mechanism — was tried first and reverted: the
  `docker compose` CLI installed in this environment (v2.2.3, predating
  that key) rejects it with `Additional property name is not allowed`.
  Anyone reusing this pattern on a shared Docker host should pin a project
  name the same way rather than relying on the directory-name default.
- ⚠️ **`PUT /api/0/projects/{org}/{project}/` requires `name` on every
  call, not only when it changes.** The provider's `ProjectResource.Update`
  originally only set `update.Name` when the plan's `name` differed from
  state, omitting it otherwise (e.g. a platform-only update). GlitchTip
  rejected that with `422 {"detail":[{"loc":["body","payload","name"],
  "msg":"Field required"}]}` — confirming `ProjectIn.name` is required on
  the wire even though it's conceptually unchanged. Fixed in
  `src/provider/project_resource.go` to always send the plan's current name
  on every `Update` call.
- Everything else in this document was confirmed accurate end-to-end: org
  create, team create x2, project create (with `initial_team`), project key
  create + DSN + `terraform import` (`ImportStateVerify`), project↔team
  membership create, a platform-only update, an empty-plan drift check, and
  full resource-graph destroy all passed against a live instance in one
  `TestAccGlitchTipLifecycle` run (see `tests/acceptance/`).

---

## Summary of corrections to the design doc's assumptions

| # | Design doc assumed | Reality |
|---|--------------------|---------|
| 1 | snake_case JSON (`event_throttle_rate`) | **camelCase** (`eventThrottleRate`) everywhere |
| 2 | Team create body `{"name": ...}` | **`{"slug": ...}`** — teams have no `name` |
| 3 | `id` fields are ints | `id` is a **string** on org/team/project/key (`projectID` inside a key is an int) |
| 4 | Uniform DELETE → 204 | team/key → 204; **project↔team unassign → 200 + body** |
| 5 | Project GET includes `teams` | It does **not**; use `/projects/{o}/{p}/teams/` or the org project list |
| 6 | (pagination unknown) | `Link` header, but wrapped in Python `{'...'}` set-repr; `results="false"` = no more pages; `X-Hits`/`X-Max-Hits` |
| 7 | 404 body unknown | Always `{"detail": "Not Found"}`; 422 validation is `{"detail": [ {...} ]}` |
| 8 | DSN `public`/`secret` differ | **Identical** in v6.2.6 |
| 9 | Org slug editable | **Immutable** over the API (only `name` is writable); team slug **is** mutable |
| 10 | (registration via UI) | Automated via `manage.py shell`; `/api/0/api-tokens/` is session-auth only |
| 11 | Compose comes up as-is | Needs `DYNO=web.1` (migrate) + non-curl healthcheck |
| 12 | Org / project `DELETE` status unknown | Both → **204** empty body (confirmed in the follow-up probe) |
| 13 | Org creatable via API? (untested) | **Yes** — `POST /api/0/organizations/` `{"name":...}` → 201; slug server-derived; creator auto-becomes OWNER |
