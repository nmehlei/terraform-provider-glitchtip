// src/provider/project_alert_resource.go
package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nmehlei/terraform-provider-glitchtip/src/glitchtip"
)

var (
	_ resource.Resource                = &ProjectAlertResource{}
	_ resource.ResourceWithImportState = &ProjectAlertResource{}
)

// NewProjectAlertResource is the resource factory registered in provider.go.
func NewProjectAlertResource() resource.Resource { return &ProjectAlertResource{} }

// ProjectAlertResource manages a GlitchTip project alert via full CRUD.
type ProjectAlertResource struct {
	client *glitchtip.Client
}

type projectAlertResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationSlug types.String `tfsdk:"organization_slug"`
	ProjectSlug      types.String `tfsdk:"project_slug"`
	Name             types.String `tfsdk:"name"`
	TimespanMinutes  types.Int64  `tfsdk:"timespan_minutes"`
	Quantity         types.Int64  `tfsdk:"quantity"`
	Uptime           types.Bool   `tfsdk:"uptime"`
	Recipients       types.Set    `tfsdk:"recipients"`
}

type alertRecipientModel struct {
	Type types.String `tfsdk:"type"`
	URL  types.String `tfsdk:"url"`
}

var alertRecipientAttrTypes = map[string]attr.Type{"type": types.StringType, "url": types.StringType}

func (r *ProjectAlertResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_alert"
}

func (r *ProjectAlertResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GlitchTip project alert: notify the recipients when `quantity` events happen within " +
			"`timespan_minutes` (or on an uptime-monitor failure). " +
			"`terraform import` identifier: `<organization_slug>:<project_slug>:<alert_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Alert ID, assigned by GlitchTip.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"organization_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the organization the project belongs to. Changing this forces a new alert.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"project_slug": schema.StringAttribute{
				MarkdownDescription: "Slug of the project the alert belongs to. Changing this forces a new alert.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Alert name.",
				Required:            true,
			},
			"timespan_minutes": schema.Int64Attribute{
				MarkdownDescription: "Length of the window, in minutes, in which `quantity` events trigger the alert.",
				Required:            true,
			},
			"quantity": schema.Int64Attribute{
				MarkdownDescription: "Number of events within `timespan_minutes` that triggers the alert (1 alerts on every new issue).",
				Required:            true,
			},
			"uptime": schema.BoolAttribute{
				MarkdownDescription: "Also alert on any uptime-monitor check failure. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"recipients": schema.SetNestedAttribute{
				MarkdownDescription: "Where the alert is sent. At least one is required.",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							MarkdownDescription: "`email` (to the project's members, no `url`), or a webhook-style target: " +
								"`webhook`, `discord`, `googlechat`, `teams`, `feishu` or `ntfy` (these need a `url`). " +
								"`zulip` is not supported.",
							Required: true,
						},
						"url": schema.StringAttribute{
							MarkdownDescription: "Webhook URL for the webhook-style types. Treat it as a secret if it contains a token.",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

func (r *ProjectAlertResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ProjectAlertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, diags := alertRequestFromModel(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	alert, err := r.client.CreateProjectAlert(ctx, plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating GlitchTip Project Alert", err.Error())
		return
	}
	model, diags := alertModelFromAPI(plan.OrganizationSlug.ValueString(), plan.ProjectSlug.ValueString(), alert)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *ProjectAlertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid Alert ID", fmt.Sprintf("%q is not a number.", state.ID.ValueString()))
		return
	}
	alert, err := r.client.GetProjectAlert(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), id)
	if err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading GlitchTip Project Alert", err.Error())
		return
	}
	model, diags := alertModelFromAPI(state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), alert)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *ProjectAlertResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid Alert ID", fmt.Sprintf("%q is not a number.", state.ID.ValueString()))
		return
	}
	body, diags := alertRequestFromModel(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	alert, err := r.client.UpdateProjectAlert(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), id, body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating GlitchTip Project Alert", err.Error())
		return
	}
	model, diags := alertModelFromAPI(state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), alert)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *ProjectAlertResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid Alert ID", fmt.Sprintf("%q is not a number.", state.ID.ValueString()))
		return
	}
	if err := r.client.DeleteProjectAlert(ctx, state.OrganizationSlug.ValueString(), state.ProjectSlug.ValueString(), id); err != nil {
		var apiErr *glitchtip.APIError
		if errors.As(err, &apiErr) && apiErr.NotFound() {
			return
		}
		resp.Diagnostics.AddError("Error Deleting GlitchTip Project Alert", err.Error())
	}
}

// ImportState expects "<organization_slug>:<project_slug>:<alert_id>".
func (r *ProjectAlertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitImportID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_slug"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_slug"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}

func alertRequestFromModel(ctx context.Context, m projectAlertResourceModel) (glitchtip.ProjectAlertRequest, diag.Diagnostics) {
	var recipients []alertRecipientModel
	diags := m.Recipients.ElementsAs(ctx, &recipients, false)
	req := glitchtip.ProjectAlertRequest{
		Name:            m.Name.ValueString(),
		TimespanMinutes: int(m.TimespanMinutes.ValueInt64()),
		Quantity:        int(m.Quantity.ValueInt64()),
		Uptime:          m.Uptime.ValueBool(),
	}
	for _, rec := range recipients {
		req.Recipients = append(req.Recipients, glitchtip.AlertRecipient{RecipientType: rec.Type.ValueString(), URL: rec.URL.ValueString()})
	}
	return req, diags
}

func alertModelFromAPI(orgSlug, projectSlug string, a *glitchtip.ProjectAlert) (projectAlertResourceModel, diag.Diagnostics) {
	elems := make([]attr.Value, 0, len(a.Recipients))
	for _, rec := range a.Recipients {
		url := types.StringNull()
		if rec.URL != "" {
			url = types.StringValue(rec.URL)
		}
		elems = append(elems, types.ObjectValueMust(alertRecipientAttrTypes, map[string]attr.Value{
			"type": types.StringValue(rec.RecipientType), "url": url,
		}))
	}
	set, diags := types.SetValue(types.ObjectType{AttrTypes: alertRecipientAttrTypes}, elems)
	return projectAlertResourceModel{
		ID:               types.StringValue(strconv.Itoa(a.ID)),
		OrganizationSlug: types.StringValue(orgSlug),
		ProjectSlug:      types.StringValue(projectSlug),
		Name:             types.StringValue(a.Name),
		TimespanMinutes:  types.Int64Value(int64(a.TimespanMinutes)),
		Quantity:         types.Int64Value(int64(a.Quantity)),
		Uptime:           types.BoolValue(a.Uptime),
		Recipients:       set,
	}, diags
}
