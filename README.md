# Terraform Provider for GlitchTip

Manages resources on a [GlitchTip](https://glitchtip.com) instance via its REST API.

## What's Supported

| Resource | List | Get | Create | Update | Delete | Import |
| --- | --- | --- | --- | --- | --- | --- |
| glitchtip_organization | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| glitchtip_team | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| glitchtip_project | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| glitchtip_project_key | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| glitchtip_project_alert | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| glitchtip_project_team_membership | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

## Quickstart

```hcl
terraform {
  required_providers {
    glitchtip = {
      source = "nmehlei/glitchtip"
    }
  }
}

provider "glitchtip" {
  endpoint = "https://app.glitchtip.com"
  # token is sensitive - prefer the GLITCHTIP_TOKEN environment variable.
}

resource "glitchtip_organization" "acme" {
  name = "Acme"
}

resource "glitchtip_team" "platform" {
  organization_slug = glitchtip_organization.acme.slug
  slug              = "platform"
}

resource "glitchtip_project" "checkout" {
  organization_slug   = glitchtip_organization.acme.slug
  initial_team        = glitchtip_team.platform.slug
  name                = "checkout-service"
  platform            = "python"
  event_throttle_rate = 0.0
}

resource "glitchtip_project_key" "default" {
  organization_slug = glitchtip_organization.acme.slug
  project_slug      = glitchtip_project.checkout.slug
  name              = "default"
}

output "checkout_dsn" {
  value     = glitchtip_project_key.default.dsn_public
  sensitive = true
}
```

## Configuration

| Attribute | Required | Default | Description |
| --- | --- | --- | --- |
| `endpoint` | No | `https://app.glitchtip.com` | Base URL of the GlitchTip instance. Override for a self-hosted instance, e.g. `https://glitchtip.example.com`. |
| `token` | Yes | — | Bearer auth token for the GlitchTip API. Falls back to the `GLITCHTIP_TOKEN` environment variable when unset. |
| `insecure` | No | `false` | Allow a plain-HTTP endpoint. Only for local development against a disposable instance — never set this against a production endpoint. |

## Import

Resources can be imported using the `terraform import` command. Each resource has a distinct import identifier format:

### glitchtip_organization

Identifier format: `<slug>`

Example:
```shell
terraform import glitchtip_organization.acme acme
```

See [docs/resources/organization.md](docs/resources/organization.md) for details.

### glitchtip_team

Identifier format: `<organization_slug>:<team_slug>`

Example:
```shell
terraform import glitchtip_team.platform acme:platform
```

See [docs/resources/team.md](docs/resources/team.md) for details.

### glitchtip_project

Identifier format: `<organization_slug>:<project_slug>:<initial_team>`

Example:
```shell
terraform import glitchtip_project.checkout acme:checkout-service:platform
```

See [docs/resources/project.md](docs/resources/project.md) for details.

### glitchtip_project_key

Identifier format: `<organization_slug>:<project_slug>:<key_id>`

Example:
```shell
terraform import glitchtip_project_key.default acme:checkout-service:1
```

See [docs/resources/project_key.md](docs/resources/project_key.md) for details.

### glitchtip_project_team_membership

Identifier format: `<organization_slug>:<project_slug>:<team_slug>`

Example:
```shell
terraform import glitchtip_project_team_membership.checkout_sre acme:checkout-service:sre
```

See [docs/resources/project_team_membership.md](docs/resources/project_team_membership.md) for details.

## Testing

### Unit Tests

Run unit tests without any external dependencies:

```shell
go test ./src/...
```

### Acceptance Tests

Real-instance acceptance tests require a GlitchTip instance running locally via Docker. The test suite is gated on the `GLITCHTIP_ACCEPTANCE=1` environment variable.

Start the instance, run tests, and clean up:

```shell
cd tests/acceptance
./up.sh
GLITCHTIP_ACCEPTANCE=1 go test -v ./tests/acceptance
./down.sh
```

See [tests/acceptance/README.md](tests/acceptance/README.md) for detailed setup and troubleshooting.

## License

This provider is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.
