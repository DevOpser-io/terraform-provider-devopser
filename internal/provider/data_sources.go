// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

// --- devopser_outputs (data "tfe_outputs") ------------------------------------------------

// NewOutputsDataSource is data "devopser_outputs".
func NewOutputsDataSource() datasource.DataSource { return &outputsDataSource{} }

type outputsDataSource struct{ client *devopser.Client }

type outputsModel struct {
	ID                 types.String  `tfsdk:"id"`
	Organization       types.String  `tfsdk:"organization"`
	Workspace          types.String  `tfsdk:"workspace"`
	Values             types.Dynamic `tfsdk:"values"`
	NonsensitiveValues types.Dynamic `tfsdk:"nonsensitive_values"`
}

func (d *outputsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_outputs"
}

func (d *outputsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Root outputs of another workspace (tfe_outputs). Needs an infra:admin key; reads are audited.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true},
			"organization":        schema.StringAttribute{Optional: true, Description: "Accepted for tfe compatibility and ignored."},
			"workspace":           schema.StringAttribute{Required: true, Description: "Workspace name."},
			"values":              schema.DynamicAttribute{Computed: true, Sensitive: true},
			"nonsensitive_values": schema.DynamicAttribute{Computed: true},
		},
	}
}

func (d *outputsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData)
}

func (d *outputsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m outputsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, err := d.client.Workspaces.ReadByName(ctx, m.Workspace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error finding workspace "+m.Workspace.ValueString(), err.Error())
		return
	}
	out, err := d.client.Outputs.Read(ctx, ws.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading outputs of "+ws.Name, err.Error())
		return
	}
	nonsensitive := map[string]any{}
	for name, value := range out.Values {
		if !out.IsSensitive(name) {
			nonsensitive[name] = value
		}
	}
	all, err := toDynamic(out.Values)
	if err != nil {
		resp.Diagnostics.AddError("Error converting outputs", err.Error())
		return
	}
	plain, err := toDynamic(nonsensitive)
	if err != nil {
		resp.Diagnostics.AddError("Error converting outputs", err.Error())
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(ws.ID, 10))
	m.Values = all
	m.NonsensitiveValues = plain
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// toDynamic turns decoded JSON into a dynamic value: objects, tuples, strings,
// numbers, bools; null becomes a dynamic null.
func toDynamic(v any) (types.Dynamic, error) {
	value, err := toAttr(v)
	if err != nil {
		return types.DynamicNull(), err
	}
	return types.DynamicValue(value), nil
}

func toAttr(v any) (attr.Value, error) {
	switch x := v.(type) {
	case nil:
		return types.DynamicNull(), nil
	case string:
		return types.StringValue(x), nil
	case bool:
		return types.BoolValue(x), nil
	case float64:
		return types.NumberValue(big.NewFloat(x)), nil
	case json.Number:
		f, _, err := big.ParseFloat(x.String(), 10, 512, big.ToNearestEven)
		if err != nil {
			return nil, err
		}
		return types.NumberValue(f), nil
	case []any:
		elems := make([]attr.Value, 0, len(x))
		elemTypes := make([]attr.Type, 0, len(x))
		for _, item := range x {
			ev, err := toAttr(item)
			if err != nil {
				return nil, err
			}
			elems = append(elems, ev)
			elemTypes = append(elemTypes, ev.Type(context.Background()))
		}
		t, diags := types.TupleValue(elemTypes, elems)
		if diags.HasError() {
			return nil, diagErr(diags)
		}
		return t, nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		attrs := make(map[string]attr.Value, len(x))
		attrTypes := make(map[string]attr.Type, len(x))
		for _, k := range keys {
			ev, err := toAttr(x[k])
			if err != nil {
				return nil, err
			}
			attrs[k] = ev
			attrTypes[k] = ev.Type(context.Background())
		}
		o, diags := types.ObjectValue(attrTypes, attrs)
		if diags.HasError() {
			return nil, diagErr(diags)
		}
		return o, nil
	default:
		return nil, fmt.Errorf("unsupported output value type %T", v)
	}
}

// diagErr flattens framework diagnostics into one error.
func diagErr(diags diag.Diagnostics) error {
	var parts []string
	for _, d := range diags.Errors() {
		parts = append(parts, d.Summary()+": "+d.Detail())
	}
	return errors.New(strings.Join(parts, "; "))
}

// --- devopser_workspace (data "tfe_workspace") --------------------------------------------

// NewWorkspaceDataSource is data "devopser_workspace".
func NewWorkspaceDataSource() datasource.DataSource { return &workspaceDataSource{} }

type workspaceDataSource struct{ client *devopser.Client }

type workspaceDataModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Organization     types.String `tfsdk:"organization"`
	AutoApply        types.Bool   `tfsdk:"auto_apply"`
	WorkingDirectory types.String `tfsdk:"working_directory"`
	TerraformVersion types.String `tfsdk:"terraform_version"`
}

func (d *workspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *workspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a workspace by name (tfe_workspace).",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true},
			"name":              schema.StringAttribute{Required: true},
			"organization":      schema.StringAttribute{Optional: true, Description: "Accepted for tfe compatibility and ignored."},
			"auto_apply":        schema.BoolAttribute{Computed: true},
			"working_directory": schema.StringAttribute{Computed: true},
			"terraform_version": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *workspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData)
}

func (d *workspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m workspaceDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, err := d.client.Workspaces.ReadByName(ctx, m.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error finding workspace "+m.Name.ValueString(), err.Error())
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(ws.ID, 10))
	m.AutoApply = types.BoolValue(ws.AutoApply)
	m.WorkingDirectory = types.StringValue(ws.WorkingDirectory)
	m.TerraformVersion = types.StringValue(ws.TofuVersion)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// --- devopser_variables (data "tfe_variables") --------------------------------------------

// NewVariablesDataSource is data "devopser_variables".
func NewVariablesDataSource() datasource.DataSource { return &variablesDataSource{} }

type variablesDataSource struct{ client *devopser.Client }

var variableObjectType = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "value": types.StringType,
	"category": types.StringType, "sensitive": types.BoolType, "hcl": types.BoolType,
}

type variablesModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Variables   types.List   `tfsdk:"variables"`
	Terraform   types.List   `tfsdk:"terraform"`
	Env         types.List   `tfsdk:"env"`
}

func (d *variablesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variables"
}

func (d *variablesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	list := func() schema.ListAttribute {
		return schema.ListAttribute{Computed: true, ElementType: types.ObjectType{AttrTypes: variableObjectType}}
	}
	resp.Schema = schema.Schema{
		Description: "A workspace's variables (tfe_variables). Sensitive values are empty.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true},
			"workspace_id": schema.StringAttribute{Required: true},
			"variables":    list(),
			"terraform":    list(),
			"env":          list(),
		},
	}
}

func (d *variablesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData)
}

func (d *variablesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m variablesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wsID, err := parseID(m.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid workspace id", err.Error())
		return
	}
	rows, err := d.client.Variables.List(ctx, wsID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading variables", err.Error())
		return
	}
	var all, tf, env []attr.Value
	for _, v := range rows {
		value := v.Value
		if v.Sensitive {
			value = ""
		}
		obj, diags := types.ObjectValue(variableObjectType, map[string]attr.Value{
			"id": types.StringValue(strconv.FormatInt(v.ID, 10)), "name": types.StringValue(v.Key),
			"value": types.StringValue(value), "category": types.StringValue(string(v.Category)),
			"sensitive": types.BoolValue(v.Sensitive), "hcl": types.BoolValue(v.HCL),
		})
		resp.Diagnostics.Append(diags...)
		all = append(all, obj)
		if v.Category == devopser.CategoryEnv {
			env = append(env, obj)
		} else {
			tf = append(tf, obj)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	elem := types.ObjectType{AttrTypes: variableObjectType}
	m.ID = m.WorkspaceID
	m.Variables = types.ListValueMust(elem, nonNil(all))
	m.Terraform = types.ListValueMust(elem, nonNil(tf))
	m.Env = types.ListValueMust(elem, nonNil(env))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func nonNil(v []attr.Value) []attr.Value {
	if v == nil {
		return []attr.Value{}
	}
	return v
}

// --- devopser_github_app_installation (data "tfe_github_app_installation") ----------------

// NewGitHubAppInstallationDataSource is data "devopser_github_app_installation".
func NewGitHubAppInstallationDataSource() datasource.DataSource {
	return &githubInstallationDataSource{}
}

type githubInstallationDataSource struct{ client *devopser.Client }

type githubInstallationModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	InstallationID types.Int64  `tfsdk:"installation_id"`
}

func (d *githubInstallationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_github_app_installation"
}

func (d *githubInstallationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The organization's DevOpser GitHub App installation, by GitHub account (tfe_github_app_installation).",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Computed: true},
			"name":            schema.StringAttribute{Required: true, Description: "GitHub account (owner) name."},
			"installation_id": schema.Int64Attribute{Computed: true},
		},
	}
}

func (d *githubInstallationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData)
}

func (d *githubInstallationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m githubInstallationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	g, err := d.client.GitHubInstallations.ReadByName(ctx, m.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error finding the GitHub App installation for "+m.Name.ValueString(), err.Error())
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(g.ID, 10))
	m.InstallationID = types.Int64Value(g.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
