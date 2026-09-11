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
				Name              string   `json:"name"`
				Platform          string   `json:"platform"`
				EventThrottleRate *float64 `json:"eventThrottleRate"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			rate := 0.0
			if in.EventThrottleRate != nil {
				rate = *in.EventThrottleRate
			}
			proj = map[string]any{"id": "100", "slug": "checkout", "name": in.Name,
				"platform": in.Platform, "eventThrottleRate": rate,
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
			// Mirror the real API: GlitchTip's ProjectIn schema requires
			// "name" on every PUT (see Task 16 / project_resource.go), so
			// a request missing it should 422 the same way the live
			// instance does.
			if _, ok := in["name"]; !ok {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"detail": []map[string]any{{"loc": []string{"body", "payload", "name"}, "msg": "Field required"}},
				})
				return
			}
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
  organization_slug   = "acme"
  initial_team        = "platform"
  name                = %q
  platform            = "python"
  event_throttle_rate = 10
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
					// Regression guard: event_throttle_rate must actually
					// be sent on Create, not silently dropped.
					resource.TestCheckResourceAttr("glitchtip_project.test", "event_throttle_rate", "10"),
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
