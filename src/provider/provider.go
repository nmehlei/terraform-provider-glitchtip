// Package provider implements the GlitchTip Terraform provider.
package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

const (
	envToken = "GLITCHTIP_TOKEN"

	defaultEndpoint = "https://app.glitchtip.com"
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

type glitchtipProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
	Insecure types.Bool   `tfsdk:"insecure"`
}

func (p *GlitchTipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "glitchtip"
	resp.Version = p.version
}

func (p *GlitchTipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages resources on a GlitchTip instance's REST API (`/api/0/`).",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Base URL of the GlitchTip instance. Defaults to `" + defaultEndpoint + "` " +
					"(the hosted SaaS); override for a self-hosted instance, e.g. `https://glitchtip.example.com`.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "Bearer auth token for the GlitchTip API. Falls back to the `" + envToken +
					"` environment variable when unset; configuration fails if neither is present.",
				Optional:  true,
				Sensitive: true,
			},
			"insecure": schema.BoolAttribute{
				MarkdownDescription: "Allow a plain-HTTP endpoint. Only for local development against a " +
					"disposable instance - never set this against a production endpoint.",
				Optional: true,
			},
		},
	}
}

func (p *GlitchTipProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data glitchtipProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Endpoint.IsUnknown() || data.Token.IsUnknown() || data.Insecure.IsUnknown() {
		// A value that is still unknown at plan time may be supplied by
		// another resource during apply; defer rather than reporting missing.
		return
	}

	endpoint := strings.TrimSpace(data.Endpoint.ValueString())
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	token := strings.TrimSpace(data.Token.ValueString())
	if token == "" {
		token = strings.TrimSpace(os.Getenv(envToken))
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing GlitchTip API Token",
			"Set the token attribute in the provider configuration, or the "+envToken+" environment variable.",
		)
		return
	}

	client, err := glitchtip.New(ctx, glitchtip.Config{
		Endpoint:      endpoint,
		Token:         token,
		AllowInsecure: data.Insecure.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure GlitchTip Client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *GlitchTipProvider) Resources(_ context.Context) []func() resource.Resource {
	// Empty until Tasks 10-14 each add their New*Resource factory here —
	// Task 14's last step is the one that fills this in with all five and
	// re-verifies the full build, so no task in between references a
	// resource file that doesn't exist yet.
	return nil
}

func (p *GlitchTipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
