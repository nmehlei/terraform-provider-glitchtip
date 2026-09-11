// src/provider/project_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectResource_lifecycle(t *testing.T) {
	proj := map[string]any{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/teams/acme/platform/projects/":
			var in struct {
				Name     string `json:"name"`
				Platform string `json:"platform"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			proj = map[string]any{"id": "100", "slug": "checkout", "name": in.Name,
				"platform": in.Platform, "eventThrottleRate": 0.0,
				"teams": []map[string]string{{"slug": "platform"}}}
			_ = json.NewEncoder(w).Encode(proj)
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/projects/acme/checkout/":
			if len(proj) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(proj)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/projects/acme/checkout/":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			for k, v := range in {
				proj[k] = v
			}
			_ = json.NewEncoder(w).Encode(proj)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/projects/acme/checkout/":
			proj = map[string]any{}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := func(name string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_project" "test" {
  organization_slug = "acme"
  initial_team      = "platform"
  name              = %q
  platform          = "python"
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg("Checkout"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project.test", "slug", "checkout"),
					resource.TestCheckResourceAttr("glitchtip_project.test", "platform", "python"),
				),
			},
			{Config: cfg("Checkout Service"), Check: resource.TestCheckResourceAttr("glitchtip_project.test", "name", "Checkout Service")},
			{
				ResourceName:      "glitchtip_project.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:platform",
				ImportStateVerify: true,
				// initial_team is set from the import id, not the API, so
				// ImportStateVerify would otherwise flag it.
				ImportStateVerifyIgnore: []string{},
			},
		},
	})
}
