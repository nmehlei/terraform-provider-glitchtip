// src/provider/project_team_membership_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectTeamMembershipResource_lifecycle(t *testing.T) {
	teams := []map[string]string{{"slug": "platform"}}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			teams = append(teams, map[string]string{"slug": "sre"})
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "checkout", "teams": teams})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/teams/sre/":
			kept := teams[:0]
			for _, tm := range teams {
				if tm["slug"] != "sre" {
					kept = append(kept, tm)
				}
			}
			teams = kept
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/teams/":
			_ = json.NewEncoder(w).Encode(teams)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "100", "slug": "checkout", "name": "Checkout", "teams": teams})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_project_team_membership" "test" {
  organization_slug = "acme"
  project_slug      = "checkout"
  team_slug         = "sre"
}
`, srv.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.TestCheckResourceAttr(
					"glitchtip_project_team_membership.test", "id", "acme:checkout:sre"),
			},
			{
				ResourceName:      "glitchtip_project_team_membership.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:sre",
				ImportStateVerify: true,
			},
		},
	})
}
