// src/provider/project_alert_resource_test.go
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectAlertResource_lifecycle(t *testing.T) {
	base := "/api/0/projects/acme/checkout/alerts/"
	var alert map[string]any
	srv := fakeGlitchTip(t, func(w http.ResponseWriter, r *http.Request) {
		write := func(status int) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(alert)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			_ = json.NewDecoder(r.Body).Decode(&alert)
			alert["id"] = 7
			write(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == base:
			list := []map[string]any{}
			if alert != nil {
				list = append(list, alert)
			}
			_ = json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPut && r.URL.Path == base+"7/":
			_ = json.NewDecoder(r.Body).Decode(&alert)
			alert["id"] = 7
			write(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == base+"7/":
			alert = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	t.Setenv("GLITCHTIP_TOKEN", "test-token")

	cfg := func(quantity int, extra string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}
resource "glitchtip_project_alert" "test" {
  organization_slug = "acme"
  project_slug      = "checkout"
  name              = "new issues"
  timespan_minutes  = 1
  quantity          = %d
  recipients = [
    { type = "email" },%s
  ]
}
`, srv.URL, quantity, extra)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg(1, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project_alert.test", "id", "7"),
					resource.TestCheckResourceAttr("glitchtip_project_alert.test", "quantity", "1"),
					resource.TestCheckResourceAttr("glitchtip_project_alert.test", "uptime", "false"),
					resource.TestCheckResourceAttr("glitchtip_project_alert.test", "recipients.#", "1"),
				),
			},
			{
				Config: cfg(3, `
    { type = "ntfy", url = "https://ntfy.example/wildberry" },`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project_alert.test", "quantity", "3"),
					resource.TestCheckResourceAttr("glitchtip_project_alert.test", "recipients.#", "2"),
				),
			},
			{
				ResourceName:      "glitchtip_project_alert.test",
				ImportState:       true,
				ImportStateId:     "acme:checkout:7",
				ImportStateVerify: true,
			},
		},
	})
}
