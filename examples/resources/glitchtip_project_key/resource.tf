resource "glitchtip_project_key" "default" {
  organization_slug = glitchtip_organization.acme.slug
  project_slug      = glitchtip_project.checkout.slug
  name              = "default"
}

output "checkout_dsn" {
  value     = glitchtip_project_key.default.dsn_public
  sensitive = true
}
