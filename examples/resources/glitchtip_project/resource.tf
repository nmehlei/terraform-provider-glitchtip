resource "glitchtip_project" "checkout" {
  organization_slug   = glitchtip_organization.acme.slug
  initial_team        = glitchtip_team.platform.slug
  name                = "checkout-service"
  platform            = "python"
  event_throttle_rate = 0.0
}
