resource "glitchtip_team" "platform" {
  organization_slug = glitchtip_organization.acme.slug
  slug              = "platform"
}
