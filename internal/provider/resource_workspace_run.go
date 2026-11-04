// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

var _ resource.Resource = &workspaceRunResource{}

// NewWorkspaceRunResource is devopser_workspace_run (tfe_workspace_run): an
// apply run on create and a destroy run on destroy.
func NewWorkspaceRunResource() resource.Resource { return &workspaceRunResource{} }

type workspaceRunResource struct {
	client *devopser.Client
	// pollInterval is shortened in unit tests.
	pollInterval time.Duration
}

type runOptionsModel struct {
	ManualConfirm types.Bool `tfsdk:"manual_confirm"`
	WaitForRun    types.Bool `tfsdk:"wait_for_run"`
}

type workspaceRunModel struct {
	ID          types.String     `tfsdk:"id"`
	WorkspaceID types.String     `tfsdk:"workspace_id"`
	Apply       *runOptionsModel `tfsdk:"apply"`
	Destroy     *runOptionsModel `tfsdk:"destroy"`
}

func (r *workspaceRunResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_run"
}

func runBlock(what string) schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		Description: "Queue " + what + " run.",
		Attributes: map[string]schema.Attribute{
			"manual_confirm": schema.BoolAttribute{Optional: true,
				Description: "false: the provider confirms the run itself with its token, as tfe does."},
			"wait_for_run": schema.BoolAttribute{Optional: true,
				Description: "Wait for the run to finish and fail if it errors (default true)."},
		},
	}
}

func (r *workspaceRunResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Runs a workspace on create (apply) and on destroy (destroy), like tfe_workspace_run.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"workspace_id": schema.StringAttribute{Required: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		},
		Blocks: map[string]schema.Block{"apply": runBlock("an apply"), "destroy": runBlock("a destroy")},
	}
}

func (r *workspaceRunResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData)
}

func boolOr(v types.Bool, def bool) bool {
	if v.IsNull() || v.IsUnknown() {
		return def
	}
	return v.ValueBool()
}

// execute queues a run of kind and follows it per opts. It returns the run id.
func (r *workspaceRunResource) execute(ctx context.Context, wsID int64, kind devopser.RunKind, opts *runOptionsModel) (string, error) {
	run, err := r.client.Runs.Create(ctx, wsID, devopser.RunCreateOptions{Kind: kind, Message: "Queued by devopser_workspace_run"})
	if err != nil {
		return "", err
	}
	manualConfirm := boolOr(opts.ManualConfirm, false)
	waitForRun := boolOr(opts.WaitForRun, true)
	interval := r.pollInterval
	if interval == 0 {
		interval = 5 * time.Second
	}
	confirmed := false
	for {
		current, err := r.client.Runs.WaitFor(ctx, run.ID, interval, func(x *devopser.Run) bool {
			return x.Status == devopser.RunAwaitingApproval && !confirmed
		})
		if err != nil {
			return run.ID, err
		}
		switch {
		case current.Status == devopser.RunAwaitingApproval && !manualConfirm:
			if err := r.client.Runs.Apply(ctx, run.ID); err != nil {
				return run.ID, fmt.Errorf("confirming run %s: %w", run.ID, err)
			}
			confirmed = true
			if !waitForRun {
				return run.ID, nil
			}
		case current.Status == devopser.RunAwaitingApproval:
			// manual_confirm = true: a person confirms in DevOpser.
			if !waitForRun {
				return run.ID, nil
			}
			confirmed = true
		case current.Status == devopser.RunErrored, current.Status == devopser.RunDiscarded,
			current.Status == devopser.RunCancelled:
			return run.ID, fmt.Errorf("run %s ended %s: %s %s", run.ID, current.Status, current.ErrorCode, current.ErrorMessage)
		default:
			return run.ID, nil
		}
	}
}

func (r *workspaceRunResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceRunModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(plan.WorkspaceID.ValueString())
	if plan.Apply != nil {
		wsID, err := parseID(plan.WorkspaceID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid workspace id", err.Error())
			return
		}
		runID, err := r.execute(ctx, wsID, devopser.RunKindApply, plan.Apply)
		if runID != "" {
			plan.ID = types.StringValue(runID)
		}
		if err != nil {
			resp.Diagnostics.AddError("Apply run failed", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read keeps state as is: a finished run has nothing to refresh.
func (r *workspaceRunResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceRunModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceRunResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state workspaceRunModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workspaceRunResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceRunModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || state.Destroy == nil {
		return
	}
	wsID, err := parseID(state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid workspace id", err.Error())
		return
	}
	if _, err := r.execute(ctx, wsID, devopser.RunKindDestroy, state.Destroy); err != nil {
		resp.Diagnostics.AddError("Destroy run failed", err.Error())
	}
}
