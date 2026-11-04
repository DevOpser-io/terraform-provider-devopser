// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

// Package provider is the devopser OpenTofu provider: a stand-in for
// hashicorp/tfe that manages DevOpser Infrastructure Control.
package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

// DefaultHostname matches tfe's default of app.terraform.io.
const DefaultHostname = "app.devopser.io"

var _ provider.Provider = &devopserProvider{}

type devopserProvider struct{ version string }

// New returns the provider factory main.go serves.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &devopserProvider{version: version} }
}

type providerModel struct {
	Hostname     types.String `tfsdk:"hostname"`
	Token        types.String `tfsdk:"token"`
	Organization types.String `tfsdk:"organization"`
}

func (p *devopserProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "devopser"
	resp.Version = p.version
}

func (p *devopserProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages DevOpser Infrastructure Control, as the tfe provider manages Terraform Cloud.",
		Attributes: map[string]schema.Attribute{
			"hostname": schema.StringAttribute{
				Optional:    true,
				Description: "DevOpser hostname. Defaults to DEVOPSER_HOSTNAME, then app.devopser.io.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "An organization API key. Defaults to DEVOPSER_TOKEN, then the CLI config " +
					"credentials for the hostname (written by `tofu login`).",
			},
			"organization": schema.StringAttribute{
				Optional:    true,
				Description: "Accepted for tfe compatibility and ignored: the organization comes from the token.",
			},
		},
	}
}

func (p *devopserProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hostname := firstNonEmpty(cfg.Hostname.ValueString(), os.Getenv("DEVOPSER_HOSTNAME"), DefaultHostname)
	token := firstNonEmpty(cfg.Token.ValueString(), os.Getenv("DEVOPSER_TOKEN"), cliConfigToken(hostname))
	if token == "" {
		resp.Diagnostics.AddError("Missing DevOpser token",
			"Set token in the provider block, DEVOPSER_TOKEN, or run `tofu login "+hostname+"`.")
		return
	}
	// DEVOPSER_ADDRESS (a full URL) is for local development against a dev stack.
	address := firstNonEmpty(os.Getenv("DEVOPSER_ADDRESS"), "https://"+hostname)
	client, err := devopser.NewClient(&devopser.Config{Address: address, Token: token})
	if err != nil {
		resp.Diagnostics.AddError("Could not create the DevOpser client", err.Error())
		return
	}
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *devopserProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewWorkspaceResource,
		NewVariableResource,
		NewWorkspaceSettingsResource,
		NewWorkspaceRunResource,
	}
}

func (p *devopserProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOutputsDataSource,
		NewWorkspaceDataSource,
		NewVariablesDataSource,
		NewGitHubAppInstallationDataSource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// cliConfigToken reads the token `tofu login` stores for hostname, as the tfe
// provider reads Terraform's credentials file.
func cliConfigToken(hostname string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".terraform.d", "credentials.tfrc.json"))
	if err != nil {
		return ""
	}
	var file struct {
		Credentials map[string]struct {
			Token string `json:"token"`
		} `json:"credentials"`
	}
	if json.Unmarshal(data, &file) != nil {
		return ""
	}
	return file.Credentials[hostname].Token
}

// clientFrom unpacks the configured client for a resource or data source.
func clientFrom(data any) *devopser.Client {
	client, _ := data.(*devopser.Client)
	return client
}
