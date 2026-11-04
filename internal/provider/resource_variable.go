// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

var (
	_ resource.Resource                = &variableResource{}
	_ resource.ResourceWithImportState = &variableResource{}
)

// NewVariableResource is devopser_variable, the stand-in for tfe_variable.
func NewVariableResource() resource.Resource { return &variableResource{} }

type variableResource struct{ client *devopser.Client }

type variableModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Key         types.String `tfsdk:"key"`
	Value       types.String `tfsdk:"value"`
	Category    types.String `tfsdk:"category"`
	Sensitive   types.Bool   `tfsdk:"sensitive"`
	HCL         types.Bool   `tfsdk:"hcl"`
	Description types.String `tfsdk:"description"`
}

func (r *variableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variable"
}

func (r *variableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A workspace variable (tfe_variable).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The variable id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"workspace_id": schema.StringAttribute{Required: true, Description: "The workspace the variable belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"key": schema.StringAttribute{Required: true, Description: "Variable name."},
			"value": schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString(""),
				Description: "Variable value. Write-only when sensitive: the API never returns it."},
			"category": schema.StringAttribute{Required: true,
				Description: "terraform (an input variable) or env (an environment variable).",
				Validators:  []validator.String{stringvalidator.OneOf("terraform", "env")}},
			"sensitive": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description:   "Hide the value in the API and the portal.",
				PlanModifiers: nil},
			"hcl": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Parse the value as HCL rather than a string."},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "Free text shown in the portal."},
		},
	}
}

func (r *variableResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData)
}

func (r *variableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan variableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wsID, err := parseID(plan.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("workspace_id"), "Invalid workspace id", err.Error())
		return
	}
	value := plan.Value.ValueString()
	v, err := r.client.Variables.Create(ctx, wsID, devopser.VariableCreateOptions{
		Key:         plan.Key.ValueString(),
		Value:       &value,
		Category:    devopser.CategoryType(plan.Category.ValueString()),
		Sensitive:   optBool(plan.Sensitive),
		HCL:         optBool(plan.HCL),
		Description: optString(plan.Description),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating variable "+plan.Key.ValueString(), err.Error())
		return
	}
	plan.ID = types.StringValue(strconv.FormatInt(v.ID, 10))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *variableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state variableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wsID, err1 := parseID(state.WorkspaceID.ValueString())
	id, err2 := parseID(state.ID.ValueString())
	if err := errors.Join(err1, err2); err != nil {
		resp.Diagnostics.AddError("Invalid variable id", err.Error())
		return
	}
	v, err := r.client.Variables.Read(ctx, wsID, id)
	if errors.Is(err, devopser.ErrResourceNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading variable", err.Error())
		return
	}
	state.Key = types.StringValue(v.Key)
	state.Category = types.StringValue(string(v.Category))
	state.Sensitive = types.BoolValue(v.Sensitive)
	state.HCL = types.BoolValue(v.HCL)
	state.Description = types.StringValue(v.Description)
	// Sensitive values are write-only: keep the configured value, as tfe does.
	if !v.Sensitive {
		state.Value = types.StringValue(v.Value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *variableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state variableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wsID, err1 := parseID(state.WorkspaceID.ValueString())
	id, err2 := parseID(state.ID.ValueString())
	if err := errors.Join(err1, err2); err != nil {
		resp.Diagnostics.AddError("Invalid variable id", err.Error())
		return
	}
	key, value, desc := plan.Key.ValueString(), plan.Value.ValueString(), plan.Description.ValueString()
	category := devopser.CategoryType(plan.Category.ValueString())
	_, err := r.client.Variables.Update(ctx, wsID, id, devopser.VariableUpdateOptions{
		Key: &key, Value: &value, Category: &category, Sensitive: optBool(plan.Sensitive),
		HCL: optBool(plan.HCL), Description: &desc,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error updating variable "+key, err.Error())
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *variableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state variableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wsID, err1 := parseID(state.WorkspaceID.ValueString())
	id, err2 := parseID(state.ID.ValueString())
	if err := errors.Join(err1, err2); err != nil {
		resp.Diagnostics.AddError("Invalid variable id", err.Error())
		return
	}
	if err := r.client.Variables.Delete(ctx, wsID, id); err != nil && !errors.Is(err, devopser.ErrResourceNotFound) {
		resp.Diagnostics.AddError("Error deleting variable "+state.Key.ValueString(), err.Error())
	}
}

// ImportState takes <workspace name or id>/<variable id>, after tfe's org/workspace/var-id.
func (r *variableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import id", "Use <workspace name or id>/<variable id>.")
		return
	}
	wsID := parts[0]
	if _, err := strconv.ParseInt(wsID, 10, 64); err != nil {
		ws, err := r.client.Workspaces.ReadByName(ctx, wsID)
		if err != nil {
			resp.Diagnostics.AddError("Error finding workspace "+wsID, err.Error())
			return
		}
		wsID = strconv.FormatInt(ws.ID, 10)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), wsID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
