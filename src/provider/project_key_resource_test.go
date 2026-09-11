// src/provider/project_key_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectKeyResource_lifecycle(t *testing.T) {
	base := "/api/0/projects/acme/checkout/keys/"
	key := map[string]any{}
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			var in struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			key = map[string]any{"id": "key1", "label": in.Name, "dsn": map[string]string{
				"public": "https://pub@acme.example/100", "security": "https://acme.example/api/100/security/?glitchtip_key=pub"}}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodGet && r.URL.Path == base+"key1/":
			if len(key) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodPut && r.URL.Path == base+"key1/":
			var in struct {
				Name *string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Name != nil {
				key["label"] = *in.Name
			}
			_ = json.NewEncoder(w).Encode(key)
		case r.Method == http.MethodDelete && r.URL.Path == base+"key1/":
			key = map[string]any{}
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
resource "glitchtip_project_key" "test" {
  organization_slug = "acme"
  project_slug      = "checkout"
  name              = %q
}
`, srv.URL, name)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg("default"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project_key.test", "name", "default"),
					resource.TestCheckResourceAttrSet("glitchtip_project_key.test", "dsn_public"),
				),
			},
			{Config: cfg("renamed"), Check: resource.TestCheckResourceAttr("glitchtip_project_key.test", "name", "renamed")},
			{
				ResourceName:      "glitchtip_project_key.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:key1",
				ImportStateVerify: true,
			},
		},
	})
}
