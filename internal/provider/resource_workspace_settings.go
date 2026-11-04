// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

var _ resource.Resource = &workspaceSettingsResource{}

// NewWorkspaceSettingsResource is devopser_workspace_settings (tfe_workspace_settings).
func NewWorkspaceSettingsResource() resource.Resource { return &workspaceSettingsResource{} }

type workspaceSettingsResource struct{ client *devopser.Client }

type workspaceSettingsModel struct {
	ID                types.String `tfsdk:"id"`
	WorkspaceID       types.String `tfsdk:"workspace_id"`
	GlobalRemoteState types.Bool   `tfsdk:"global_remote_state"`
}

func (r *workspaceSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_settings"
}

func (r *workspaceSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Settings of an existing workspace (tfe_workspace_settings).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The workspace id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"workspace_id": schema.StringAttribute{Required: true, Description: "The workspace these settings belong to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"global_remote_state": schema.BoolAttribute{Optional: true, Computed: true,
				Description: "Let every workspace in the organization read this workspace's outputs. Reset to false on destroy.",
				Default:     booldefault.StaticBool(false)},
		},
	}
}

func (r *workspaceSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData)
}

func (r *workspaceSettingsResource) apply(ctx context.Context, m *workspaceSettingsModel, global bool) error {
	id, err := parseID(m.WorkspaceID.ValueString())
	if err != nil {
		return err
	}
	ws, err := r.client.Workspaces.Update(ctx, id, devopser.WorkspaceUpdateOptions{GlobalRemoteState: &global})
	if err != nil {
		return err
	}
	m.ID = m.WorkspaceID
	m.GlobalRemoteState = types.BoolValue(ws.GlobalRemoteState)
	return nil
}

func (r *workspaceSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &plan, plan.GlobalRemoteState.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Error setting workspace settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workspaceSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseID(state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("workspace_id"), "Invalid workspace id", err.Error())
		return
	}
	ws, err := r.client.Workspaces.Read(ctx, id)
	if errors.Is(err, devopser.ErrResourceNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace settings", err.Error())
		return
	}
	state.GlobalRemoteState = types.BoolValue(ws.GlobalRemoteState)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &plan, plan.GlobalRemoteState.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Error updating workspace settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete resets the settings to their defaults, as tfe does.
func (r *workspaceSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &state, false); err != nil && !errors.Is(err, devopser.ErrResourceNotFound) {
		resp.Diagnostics.AddError("Error resetting workspace settings", err.Error())
	}
}
