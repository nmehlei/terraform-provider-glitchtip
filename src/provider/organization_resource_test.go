package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"
)

// fakeGlitchTip is a minimal stateful in-memory GlitchTip API used by every
// resource's *_resource_test.go. Each test registers only the routes it
// needs on top of the always-present /api/openapi.json.
func fakeGlitchTip(t *testing.T, routes http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.0.0"}})
			return
		}
		routes(w, r)
	}))
}

func protoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"glitchtip": func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6(New("test")())(), nil
		},
	}
}

func TestAccOrganizationResource_lifecycle(t *testing.T) {
	orgs := map[string]map[string]string{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/0/organizations/":
			var in struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			orgs["acme"] = map[string]string{"id": "1", "slug": "acme", "name": in.Name}
			_ = json.NewEncoder(w).Encode(orgs["acme"])
		case r.Method == http.MethodGet && r.URL.Path == "/api/0/organizations/acme/":
			o, ok := orgs["acme"]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(o)
		case r.Method == http.MethodPut && r.URL.Path == "/api/0/organizations/acme/":
			var in struct {
				Name *string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Name != nil {
				orgs["acme"]["name"] = *in.Name
			}
			_ = json.NewEncoder(w).Encode(orgs["acme"])
		case r.Method == http.MethodDelete && r.URL.Path == "/api/0/organizations/acme/":
			delete(orgs, "acme")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	config := func(name string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_organization" "test" {
  name = %q
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("Acme"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_organization.test", "name", "Acme"),
					resource.TestCheckResourceAttr("glitchtip_organization.test", "slug", "acme"),
					resource.TestCheckResourceAttrSet("glitchtip_organization.test", "id"),
				),
			},
			{
				Config: config("Acme Corp"),
				Check:  resource.TestCheckResourceAttr("glitchtip_organization.test", "name", "Acme Corp"),
			},
			{
				ResourceName: "glitchtip_organization.test",
				ImportState:  true,
				// GlitchTip addresses organizations by slug, not by the
				// numeric id (GET /api/0/organizations/<slug>/), so import
				// must be driven by slug rather than the harness's default
				// id-based import identifier.
				ImportStateIdFunc: func(s *tfstate.State) (string, error) {
					rs, ok := s.RootModule().Resources["glitchtip_organization.test"]
					if !ok {
						return "", fmt.Errorf("resource not found in state")
					}
					return rs.Primary.Attributes["slug"], nil
				},
				ImportStateVerify: true,
			},
		},
	})
}
