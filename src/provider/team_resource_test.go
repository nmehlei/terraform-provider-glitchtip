package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccTeamResource_lifecycle exercises glitchtip_team's full CRUD +
// import cycle against a fake GlitchTip API.
//
// GlitchTip teams have no `name` field at all - `slug` is the team's only
// settable identity, and (unlike organizations, where slug is immutable) it
// is genuinely mutable via PUT. The "update" step below changes slug and
// asserts the resource updates in place rather than being replaced.
func TestAccTeamResource_lifecycle(t *testing.T) {
	teams := map[string]map[string]any{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/acme/teams/":
			var in struct {
				Slug string `json:"slug"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			teams[in.Slug] = map[string]any{"id": "10", "slug": in.Slug}
			_ = json.NewEncoder(w).Encode(teams[in.Slug])
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/teams/acme/platform/":
			tm, ok := teams["platform"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(tm)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/teams/acme/platform-renamed/":
			tm, ok := teams["platform-renamed"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(tm)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/teams/acme/platform/":
			var in struct {
				Slug *string `json:"slug"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			tm := teams["platform"]
			if in.Slug != nil {
				delete(teams, "platform")
				tm["slug"] = *in.Slug
				teams[*in.Slug] = tm
			}
			_ = json.NewEncoder(w).Encode(tm)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/teams/acme/platform/":
			delete(teams, "platform")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/teams/acme/platform-renamed/":
			delete(teams, "platform-renamed")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := func(slug string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_team" "test" {
  organization_slug = "acme"
  slug              = %q
}
`, srv.URL, slug)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg("platform"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_team.test", "slug", "platform"),
					resource.TestCheckResourceAttr("glitchtip_team.test", "organization_slug", "acme"),
					resource.TestCheckResourceAttrSet("glitchtip_team.test", "id"),
				),
			},
			{
				Config: cfg("platform-renamed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("glitchtip_team.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("glitchtip_team.test", "slug", "platform-renamed"),
			},
			{
				ResourceName:      "glitchtip_team.test",
				ImportState:       true,
				ImportStateId:     "acme:platform-renamed",
				ImportStateVerify: true,
			},
		},
	})
}
