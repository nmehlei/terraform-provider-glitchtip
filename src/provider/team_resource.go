// src/provider/team_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &TeamResource{}
	_ resource.ResourceWithImportState = &TeamResource{}
)

// NewTeamResource is the resource factory registered in provider.go.
func NewTeamResource() resource.Resource { return &TeamResource{} }

// TeamResource manages a GlitchTip team via full CRUD.
//
// GlitchTip teams have no `name` field at all - `slug` is the team's only
// settable identity, and unlike glitchtip_organization (whose slug is
// immutable), a team's slug is mutable via PUT: changing it is a normal
// in-place Update, not a resource replacement.
type TeamResource struct {
	client *glitchtip.Client
}

type teamResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Slug             types.String `tfsdk:"slug"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
}

func (r *TeamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (r *TeamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip team within an organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Team ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The team's slug - its only settable identity (GlitchTip teams have no " +
					"separate display name). Used to address the team in the API and in `terraform import`. " +
					"Unlike an organization's slug, a team's slug is mutable: changing it updates the team in " +
					"place rather than replacing it.",
				Required: true,
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization this team belongs to. Changing this forces a new team.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *TeamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *TeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := r.client.CreateTeam(ctx, plan.OrganizationSlug.ValueString(), glitchtip.CreateTeamRequest{Slug: plan.Slug.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Team", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, teamModelFromAPI(plan.OrganizationSlug.ValueString(), team))...)
}

func (r *TeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := r.client.GetTeam(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Team", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, teamModelFromAPI(state.OrganizationSlug.ValueString(), team))...)
}

func (r *TeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state teamResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	update := glitchtip.UpdateTeamRequest{}
	if !plan.Slug.Equal(state.Slug) {
		slug := plan.Slug.ValueString()
		update.Slug = &slug
	}
	team, err := r.client.UpdateTeam(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Team", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, teamModelFromAPI(state.OrganizationSlug.ValueString(), team))...)
}

func (r *TeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteTeam(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Team", err.Error())
	}
}

// ImportState expects "<organization_slug>:<team_slug>".
func (r *TeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), parts[1])...)
}

func teamModelFromAPI(orgSlug string, team *glitchtip.Team) teamResourceModel {
	return teamResourceModel{
		ID:               types.StringValue(team.ID),
		Slug:             types.StringValue(team.Slug),
		OrganizationSlug: types.StringValue(orgSlug),
	}
}
