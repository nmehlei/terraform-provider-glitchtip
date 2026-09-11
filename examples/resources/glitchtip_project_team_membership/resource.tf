resource "glitchtip_team" "sre" {
  organization_slug = glitchtip_organization.acme.slug
  slug              = "sre"
}

resource "glitchtip_project_team_membership" "checkout_sre" {
  organization_slug = glitchtip_organization.acme.slug
  project_slug      = glitchtip_project.checkout.slug
  team_slug         = glitchtip_team.sre.slug
}
