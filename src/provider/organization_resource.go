// Package provider implements the GlitchTip Terraform provider.
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
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

// NewOrganizationResource is the resource factory registered in provider.go.
func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

// OrganizationResource manages a GlitchTip organization via full CRUD.
type OrganizationResource struct {
	client *glitchtip.Client
}

type organizationResourceModel struct {
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`
}

func (r *OrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Organization ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL slug, derived from the name by GlitchTip. Used to address the organization in the API and in `terraform import`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Organization display name.",
				Required:            true,
			},
		},
	}
}

func (r *OrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*glitchtip.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("expected *glitchtip.Client, got: %T. Report this to the provider maintainers.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.client.CreateOrganization(ctx, glitchtip.CreateOrganizationRequest{Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationModelFromAPI(org))...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.client.GetOrganization(ctx, state.Slug.ValueString())
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationModelFromAPI(org))...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update := glitchtip.UpdateOrganizationRequest{}
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		update.Name = &name
	}

	org, err := r.client.UpdateOrganization(ctx, state.Slug.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Organization", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationModelFromAPI(org))...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteOrganization(ctx, state.Slug.ValueString()); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return // already gone
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Organization", err.Error())
	}
}

// ImportState accepts the organization slug as the import identifier.
func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("slug"), req, resp)
}

func organizationModelFromAPI(org *glitchtip.Organization) organizationResourceModel {
	return organizationResourceModel{
		ID:   types.StringValue(org.ID),
		Slug: types.StringValue(org.Slug),
		Name: types.StringValue(org.Name),
	}
}
