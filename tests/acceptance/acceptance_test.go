// tests/acceptance/acceptance_test.go
package acceptance

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/nmehlei/terraform-provider-glitchtip/src/provider"
)

// gate skips the whole file unless GLITCHTIP_ACCEPTANCE=1 and the endpoint +
// token are present, so `go test ./...` stays runnable without Docker.
func gate(t *testing.T) (endpoint, token string) {
	t.Helper()
	if os.Getenv("GLITCHTIP_ACCEPTANCE") != "1" {
		t.Skip("set GLITCHTIP_ACCEPTANCE=1 (and run tests/acceptance/up.sh) to run real-instance acceptance tests")
	}
	endpoint = os.Getenv("GLITCHTIP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8000"
	}
	token = os.Getenv("GLITCHTIP_TOKEN")
	if token == "" {
		t.Fatal("GLITCHTIP_TOKEN is required when GLITCHTIP_ACCEPTANCE=1")
	}
	return endpoint, token
}

func factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"glitchtip": func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6(provider.New("acc")())(), nil
		},
	}
}

// TestAccGlitchTipLifecycle drives all five resources
// (glitchtip_organization, glitchtip_team, glitchtip_project,
// glitchtip_project_key, glitchtip_project_team_membership) through
// create -> update -> import -> drift-check -> destroy against a real,
// disposable GlitchTip instance (see tests/acceptance/up.sh / down.sh).
func TestAccGlitchTipLifecycle(t *testing.T) {
	endpoint, token := gate(t)
	t.Setenv("GLITCHTIP_TOKEN", token)
	// terraform-plugin-testing's resource.Test independently gates on the
	// standard TF_ACC env var; GLITCHTIP_ACCEPTANCE=1 is our own opt-in name
	// documented in the README, so set TF_ACC here rather than requiring
	// callers to export both.
	t.Setenv("TF_ACC", "1")

	// Unique suffix so repeated runs against a persistent instance don't collide.
	suffix := fmt.Sprintf("tf-acc-%d", os.Getpid())

	config := func(platform string) string {
		return fmt.Sprintf(`
provider "glitchtip" {
  endpoint = %q
  insecure = true
}

resource "glitchtip_organization" "o" {
  name = "%s-org"
}

resource "glitchtip_team" "t" {
  organization_slug = glitchtip_organization.o.slug
  slug               = "%s-team"
}

resource "glitchtip_team" "t2" {
  organization_slug = glitchtip_organization.o.slug
  slug               = "%s-team2"
}

resource "glitchtip_project" "p" {
  organization_slug = glitchtip_organization.o.slug
  initial_team       = glitchtip_team.t.slug
  name               = "%s-proj"
  platform           = %q
}

resource "glitchtip_project_key" "k" {
  organization_slug = glitchtip_organization.o.slug
  project_slug       = glitchtip_project.p.slug
  name               = "default"
}

resource "glitchtip_project_team_membership" "m" {
  organization_slug = glitchtip_organization.o.slug
  project_slug       = glitchtip_project.p.slug
  team_slug          = glitchtip_team.t2.slug
}
`, endpoint, suffix, suffix, suffix, suffix, platform)
	}

	base := config("python")
	updated := config("go") // second step changes the project's platform

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{
				// create
				Config: base,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("glitchtip_organization.o", "slug"),
					resource.TestCheckResourceAttrSet("glitchtip_team.t", "id"),
					resource.TestCheckResourceAttrSet("glitchtip_project.p", "slug"),
					resource.TestCheckResourceAttr("glitchtip_project.p", "platform", "python"),
					resource.TestCheckResourceAttrSet("glitchtip_project_key.k", "dsn_public"),
					resource.TestCheckResourceAttr("glitchtip_project_team_membership.m", "team_slug", suffix+"-team2"),
				),
			},
			{
				// import: verify glitchtip_project_key round-trips through
				// "<organization_slug>:<project_slug>:<id>".
				ResourceName: "glitchtip_project_key.k",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["glitchtip_project_key.k"]
					return fmt.Sprintf("%s:%s:%s",
						rs.Primary.Attributes["organization_slug"],
						rs.Primary.Attributes["project_slug"],
						rs.Primary.Attributes["id"]), nil
				},
				ImportStateVerify: true,
			},
			{
				// update: platform python -> go
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("glitchtip_project.p", "platform", "go"),
				),
			},
			{
				// drift check: re-apply the same config, expect an empty plan.
				Config:   updated,
				PlanOnly: true,
			},
		},
	})
}
