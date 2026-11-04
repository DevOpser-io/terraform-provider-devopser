// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

var (
	_ resource.Resource                = &workspaceResource{}
	_ resource.ResourceWithImportState = &workspaceResource{}
)

// NewWorkspaceResource is devopser_workspace, the stand-in for tfe_workspace.
func NewWorkspaceResource() resource.Resource { return &workspaceResource{} }

type workspaceResource struct{ client *devopser.Client }

type vcsRepoModel struct {
	Identifier              types.String `tfsdk:"identifier"`
	Branch                  types.String `tfsdk:"branch"`
	IngressSubmodules       types.Bool   `tfsdk:"ingress_submodules"`
	GitHubAppInstallationID types.String `tfsdk:"github_app_installation_id"`
}

type workspaceModel struct {
	ID                  types.String  `tfsdk:"id"`
	Name                types.String  `tfsdk:"name"`
	Organization        types.String  `tfsdk:"organization"`
	ProjectID           types.String  `tfsdk:"project_id"`
	AutoApply           types.Bool    `tfsdk:"auto_apply"`
	AutoApplyRunTrigger types.Bool    `tfsdk:"auto_apply_run_trigger"`
	QueueAllRuns        types.Bool    `tfsdk:"queue_all_runs"`
	TerraformVersion    types.String  `tfsdk:"terraform_version"`
	ForceDelete         types.Bool    `tfsdk:"force_delete"`
	WorkingDirectory    types.String  `tfsdk:"working_directory"`
	AWSConnectionID     types.String  `tfsdk:"aws_connection_id"`
	VCSRepo             *vcsRepoModel `tfsdk:"vcs_repo"`
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An Infrastructure Control workspace (tfe_workspace).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The workspace id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "Workspace name, unique in the organization."},
			"organization": schema.StringAttribute{Optional: true,
				Description: "Accepted for tfe compatibility and ignored."},
			"project_id": schema.StringAttribute{Optional: true,
				Description: "Accepted for tfe compatibility and ignored: DevOpser has no projects."},
			"auto_apply": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Apply successful plans without a confirmation."},
			"auto_apply_run_trigger": schema.BoolAttribute{Optional: true, Computed: true,
				Description: "Accepted for tfe compatibility. Run triggers are not supported yet.",
				Default:     booldefault.StaticBool(false)},
			"queue_all_runs": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Accepted for tfe compatibility. Creating a workspace never queues a run."},
			"terraform_version": schema.StringAttribute{Optional: true, Computed: true,
				Description: "Mapped to the workspace's OpenTofu version; Terraform versions use the nearest supported OpenTofu."},
			"force_delete": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Delete even while the state manages resources (they stay in AWS)."},
			"working_directory": schema.StringAttribute{Optional: true, Computed: true,
				Description: "Directory in the repository that runs execute in."},
			"aws_connection_id": schema.StringAttribute{Optional: true,
				Description: "The AWS connection runs use. DevOpser-only; tfe workspaces used env credentials."},
		},
		Blocks: map[string]schema.Block{
			"vcs_repo": schema.SingleNestedBlock{
				Description: "The GitHub repository runs check out.",
				Attributes: map[string]schema.Attribute{
					"identifier":         schema.StringAttribute{Optional: true, Description: "owner/repo"},
					"branch":             schema.StringAttribute{Optional: true, Description: "Branch to run from; the default branch when empty."},
					"ingress_submodules": schema.BoolAttribute{Optional: true, Description: "Check out git submodules too."},
					"github_app_installation_id": schema.StringAttribute{Optional: true,
						Description: "From devopser_github_app_installation. Accepted for tfe compatibility; the organization's installation is used."},
				},
			},
		},
	}
}

func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData)
}

func optString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	s := v.ValueString()
	return &s
}

func optBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func optID(v types.String) (*int64, error) {
	s := optString(v)
	if s == nil {
		return nil, nil
	}
	id, err := strconv.ParseInt(*s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a numeric id", *s)
	}
	return &id, nil
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a numeric id", s)
	}
	return id, nil
}

// tofuVersion maps tfe's terraform_version: 1.6+ OpenTofu versions pass through,
// anything else becomes the platform default (nil).
func tofuVersion(v types.String) *string {
	s := optString(v)
	if s == nil {
		return nil
	}
	var major, minor int
	if _, err := fmt.Sscanf(*s, "%d.%d", &major, &minor); err != nil || major != 1 || minor < 6 {
		return nil
	}
	short := fmt.Sprintf("%d.%d", major, minor)
	return &short
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	conn, err := optID(plan.AWSConnectionID)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("aws_connection_id"), "Invalid AWS connection id", err.Error())
		return
	}
	opts := devopser.WorkspaceCreateOptions{
		Name:             plan.Name.ValueString(),
		WorkingDirectory: optString(plan.WorkingDirectory),
		TofuVersion:      tofuVersion(plan.TerraformVersion),
		AWSConnectionID:  conn,
		AutoApply:        optBool(plan.AutoApply),
	}
	if plan.VCSRepo != nil {
		opts.RepoFullName = plan.VCSRepo.Identifier.ValueString()
		opts.Branch = optString(plan.VCSRepo.Branch)
		opts.Submodules = optBool(plan.VCSRepo.IngressSubmodules)
	}
	if opts.RepoFullName == "" {
		resp.Diagnostics.AddAttributeError(path.Root("vcs_repo"), "vcs_repo is required",
			"DevOpser workspaces run from a GitHub repository: set vcs_repo.identifier.")
		return
	}
	ws, err := r.client.Workspaces.Create(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace "+opts.Name, err.Error())
		return
	}
	r.fill(&plan, ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// fill copies server values into the model, keeping tfe-only attributes as configured.
func (r *workspaceResource) fill(m *workspaceModel, ws *devopser.Workspace) {
	m.ID = types.StringValue(strconv.FormatInt(ws.ID, 10))
	m.Name = types.StringValue(ws.Name)
	m.AutoApply = types.BoolValue(ws.AutoApply)
	m.WorkingDirectory = types.StringValue(ws.WorkingDirectory)
	if m.TerraformVersion.IsNull() || m.TerraformVersion.IsUnknown() {
		m.TerraformVersion = types.StringValue(ws.TofuVersion)
	}
	if ws.AWSConnectionID != nil {
		m.AWSConnectionID = types.StringValue(strconv.FormatInt(*ws.AWSConnectionID, 10))
	}
	if m.VCSRepo != nil {
		m.VCSRepo.Identifier = types.StringValue(ws.RepoFullName)
		m.VCSRepo.Branch = types.StringValue(ws.Branch)
		if !m.VCSRepo.IngressSubmodules.IsNull() || ws.Submodules {
			m.VCSRepo.IngressSubmodules = types.BoolValue(ws.Submodules)
		}
	}
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid workspace id", err.Error())
		return
	}
	ws, err := r.client.Workspaces.Read(ctx, id)
	if errors.Is(err, devopser.ErrResourceNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace", err.Error())
		return
	}
	if state.VCSRepo == nil {
		state.VCSRepo = &vcsRepoModel{IngressSubmodules: types.BoolNull(), GitHubAppInstallationID: types.StringNull()}
	}
	r.fill(&state, ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid workspace id", err.Error())
		return
	}
	conn, err := optID(plan.AWSConnectionID)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("aws_connection_id"), "Invalid AWS connection id", err.Error())
		return
	}
	name := plan.Name.ValueString()
	opts := devopser.WorkspaceUpdateOptions{
		Name:             &name,
		WorkingDirectory: optString(plan.WorkingDirectory),
		TofuVersion:      tofuVersion(plan.TerraformVersion),
		AWSConnectionID:  conn,
		AutoApply:        optBool(plan.AutoApply),
	}
	if plan.VCSRepo != nil {
		opts.RepoFullName = optString(plan.VCSRepo.Identifier)
		opts.Branch = optString(plan.VCSRepo.Branch)
		opts.Submodules = optBool(plan.VCSRepo.IngressSubmodules)
	}
	ws, err := r.client.Workspaces.Update(ctx, id, opts)
	if err != nil {
		resp.Diagnostics.AddError("Error updating workspace "+name, err.Error())
		return
	}
	r.fill(&plan, ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid workspace id", err.Error())
		return
	}
	if state.ForceDelete.ValueBool() {
		err = r.client.Workspaces.Delete(ctx, id)
	} else {
		err = r.client.Workspaces.SafeDelete(ctx, id)
	}
	if err != nil && !errors.Is(err, devopser.ErrResourceNotFound) {
		resp.Diagnostics.AddError("Error deleting workspace "+state.Name.ValueString(), err.Error())
	}
}

// ImportState takes a workspace id or name, as tfe takes an id or org/name.
func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		ws, err := r.client.Workspaces.ReadByName(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error importing workspace "+id, err.Error())
			return
		}
		id = strconv.FormatInt(ws.ID, 10)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
