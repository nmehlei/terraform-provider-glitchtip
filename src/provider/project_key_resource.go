// src/provider/project_key_resource.go
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
	_ resource.Resource                = &ProjectKeyResource{}
	_ resource.ResourceWithImportState = &ProjectKeyResource{}
)

// NewProjectKeyResource is the resource factory registered in provider.go.
func NewProjectKeyResource() resource.Resource { return &ProjectKeyResource{} }

// ProjectKeyResource manages a GlitchTip project key (DSN) via full CRUD.
type ProjectKeyResource struct {
	client *glitchtip.Client
}

type projectKeyResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	ProjectSlug      types.String `tfsdk:"project_slug"`
	Name             types.String `tfsdk:"name"`
	DSNPublic        types.String `tfsdk:"dsn_public"`
	DSNSecurity      types.String `tfsdk:"dsn_security"`
}

func (r *ProjectKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_key"
}

func (r *ProjectKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip project key. Exposes the DSN clients send events with. " +
			"`terraform import` identifier: `<organization_slug>:<project_slug>:<key_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Key ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization the project belongs to. Changing this forces a new key.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"project_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the project this key belongs to. Changing this forces a new key.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Key label. Optional; GlitchTip assigns a default label when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dsn_public": schema.StringAttribute{
				MarkdownDescription: "Public DSN clients use to send events. Sensitive.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dsn_security": schema.StringAttribute{
				MarkdownDescription: "Security-header reporting endpoint DSN. Sensitive.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ProjectKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ProjectKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.CreateProjectKey(ctx, plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(),
		glitchtip.CreateProjectKeyRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Project Key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectKeyModelFromAPI(
		plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(), key))...)
}

func (r *ProjectKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.GetProjectKey(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.ID.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Project Key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectKeyModelFromAPI(
		state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), key))...)
}

func (r *ProjectKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	update := glitchtip.UpdateProjectKeyRequest{}
	if !plan.Name.Equal(state.Name) && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		update.Name = &v
	}
	key, err := r.client.UpdateProjectKey(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.ID.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Project Key", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectKeyModelFromAPI(
		state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), key))...)
}

func (r *ProjectKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProjectKey(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), state.ID.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Project Key", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<key_id>".
func (r *ProjectKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}

func projectKeyModelFromAPI(orgSlug, projectSlug string, k *glitchtip.ProjectKey) projectKeyResourceModel {
	return projectKeyResourceModel{
		ID:               types.StringValue(k.ID),
		OrganizationSlug: types.StringValue(orgSlug),
		ProjectSlug:      types.StringValue(projectSlug),
		Name:             types.StringValue(k.Name),
		DSNPublic:        types.StringValue(k.DSN.Public),
		DSNSecurity:      types.StringValue(k.DSN.Security),
	}
}
