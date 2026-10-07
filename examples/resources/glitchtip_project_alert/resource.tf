# Mail the project's members about every new issue.
resource "glitchtip_project_alert" "new_issues" {
  organization_slug = glitchtip_organization.acme.slug
  project_slug      = glitchtip_project.checkout.slug
  name              = "new issues"
  timespan_minutes  = 1
  quantity          = 1

  recipients = [
    { type = "email" },
    # Webhook-style targets need a url, e.g. ntfy, discord, googlechat, teams, feishu or webhook.
    # { type = "ntfy", url = "https://ntfy.example.com/checkout" },
  ]
}
