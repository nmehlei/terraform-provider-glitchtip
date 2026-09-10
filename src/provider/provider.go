// Package provider implements the GlitchTip Terraform provider.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = &GlitchTipProvider{}

// New returns a provider.Provider factory, as required by
// providerserver.Serve. version is injected at build time (see src/main.go).
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GlitchTipProvider{version: version}
	}
}

type GlitchTipProvider struct {
	version string
}

func (p *GlitchTipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "glitchtip"
	resp.Version = p.version
}

func (p *GlitchTipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages resources on a GlitchTip instance's REST API (`/api/0/`).",
	}
}

func (p *GlitchTipProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *GlitchTipProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}

func (p *GlitchTipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
