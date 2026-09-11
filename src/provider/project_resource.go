// src/provider/project_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &ProjectResource{}
	_ resource.ResourceWithImportState = &ProjectResource{}
)

// NewProjectResource is the resource factory registered in provider.go.
func NewProjectResource() resource.Resource { return &ProjectResource{} }

// ProjectResource manages a GlitchTip project via full CRUD. The project's
// initial team association is set at creation and is immutable; further
// team associations are managed with glitchtip_project_team_membership.
type ProjectResource struct {
	client *glitchtip.Client
}

type projectResourceModel struct {
	ID                types.String  `tfsdk:"id"`
	Slug              types.String  `tfsdk:"slug"`
	OrganizationSlug  types.String  `tfsdk:"organization_slug"`
	InitialTeam       types.String  `tfsdk:"initial_team"`
	Name              types.String  `tfsdk:"name"`
	Platform          types.String  `tfsdk:"platform"`
	EventThrottleRate types.Float64 `tfsdk:"event_throttle_rate"`
}

func (r *ProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *ProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip project within an organization. `terraform import` identifier: " +
			"`<organization_slug>:<project_slug>:<initial_team>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Project ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL slug, derived from the name by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization this project belongs to. Changing this forces a new project.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"initial_team": schema.StringAttribute{
				MarkdownDescription: "Slug of the team the project is created under. GlitchTip requires a project " +
					"to belong to at least one team at creation. Changing this forces a new project; manage " +
					"additional team associations with `glitchtip_project_team_membership`.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Project display name.",
				Required:            true,
			},
			"platform": schema.StringAttribute{
				MarkdownDescription: "Platform identifier (e.g. `python`, `javascript-react`, `go`). Optional; " +
					"GlitchTip leaves it unset if omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"event_throttle_rate": schema.Float64Attribute{
				MarkdownDescription: "Fraction of incoming events to drop, `0.0`–`1.0`. Optional; defaults to the " +
					"GlitchTip server default when omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ProjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := glitchtip.CreateProjectRequest{Name: plan.Name.ValueString()}
	if !plan.Platform.IsNull() && !plan.Platform.IsUnknown() {
		in.Platform = plan.Platform.ValueString()
	}
	project, err := r.client.CreateProject(ctx, plan.OrganizationSlug.ValueString(), plan.InitialTeam.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectModelFromAPI(
		plan.OrganizationSlug.ValueString(), plan.InitialTeam.ValueString(), project))...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, err := r.client.GetProject(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectModelFromAPI(
		state.OrganizationSlug.ValueString(), state.InitialTeam.ValueString(), project))...)
}

func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	update := glitchtip.UpdateProjectRequest{}
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		update.Name = &v
	}
	if !plan.Platform.Equal(state.Platform) && !plan.Platform.IsUnknown() {
		v := plan.Platform.ValueString()
		update.Platform = &v
	}
	if !plan.EventThrottleRate.Equal(state.EventThrottleRate) && !plan.EventThrottleRate.IsUnknown() {
		v := plan.EventThrottleRate.ValueFloat64()
		update.EventThrottleRate = &v
	}
	project, err := r.client.UpdateProject(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectModelFromAPI(
		state.OrganizationSlug.ValueString(), state.InitialTeam.ValueString(), project))...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProject(ctx, state.OrganizationSlug.ValueString(), state.Slug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Project", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<initial_team>".
func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("initial_team"), parts[2])...)
}

func projectModelFromAPI(orgSlug, initialTeam string, p *glitchtip.Project) projectResourceModel {
	return projectResourceModel{
		ID:                types.StringValue(p.ID),
		Slug:              types.StringValue(p.Slug),
		OrganizationSlug:  types.StringValue(orgSlug),
		InitialTeam:       types.StringValue(initialTeam),
		Name:              types.StringValue(p.Name),
		Platform:          types.StringValue(p.Platform),
		EventThrottleRate: types.Float64Value(p.EventThrottleRate),
	}
}
