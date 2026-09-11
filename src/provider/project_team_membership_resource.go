// src/provider/project_team_membership_resource.go
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
	_ resource.Resource                = &ProjectTeamMembershipResource{}
	_ resource.ResourceWithImportState = &ProjectTeamMembershipResource{}
)

// NewProjectTeamMembershipResource is the resource factory registered in provider.go.
func NewProjectTeamMembershipResource() resource.Resource { return &ProjectTeamMembershipResource{} }

// ProjectTeamMembershipResource associates one existing team with one
// existing project, beyond the project's initial_team. Removing it from
// configuration detaches that team; it never touches the initial team.
type ProjectTeamMembershipResource struct {
	client *glitchtip.Client
}

type projectTeamMembershipResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	ProjectSlug      types.String `tfsdk:"project_slug"`
	TeamSlug         types.String `tfsdk:"team_slug"`
}

func (r *ProjectTeamMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_team_membership"
}

func (r *ProjectTeamMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Associates an existing team with an existing project. Manage the project's first " +
			"team with `glitchtip_project`'s `initial_team`; use this resource for every additional team. " +
			"`terraform import` identifier: `<organization_slug>:<project_slug>:<team_slug>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier: `<organization_slug>:<project_slug>:<team_slug>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization. Changing this forces a new association.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"project_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the project. Changing this forces a new association.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"team_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the team to associate. Changing this forces a new association.",
				Required:            true,
				PlanModifiers:       replace,
			},
		},
	}
}

func (r *ProjectTeamMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (m projectTeamMembershipResourceModel) syntheticID() string {
	return m.OrganizationSlug.ValueString() + ":" + m.ProjectSlug.ValueString() + ":" + m.TeamSlug.ValueString()
}

func (r *ProjectTeamMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.AssignProjectTeam(ctx, plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(), plan.TeamSlug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Associating Team With Project", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.syntheticID())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ProjectTeamMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.GetProjectTeamMembership(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.TeamSlug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Project-Team Association", err.Error())
		return
	}
	state.ID = types.StringValue(state.syntheticID())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is unreachable in practice (every attribute is RequiresReplace) but
// implemented to satisfy the resource.Resource interface.
func (r *ProjectTeamMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(plan.syntheticID())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ProjectTeamMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectTeamMembershipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.RemoveProjectTeam(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.TeamSlug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Removing Project-Team Association", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<team_slug>".
func (r *ProjectTeamMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_slug"), parts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
