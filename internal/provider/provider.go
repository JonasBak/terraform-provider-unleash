// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	unleash "github.com/Unleash/unleash-server-api-go/client"

	"github.com/Masterminds/semver"
	"github.com/fatih/structs"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure ScaffoldingProvider satisfies various provider interfaces.
var _ provider.Provider = &UnleashProvider{}

// ScaffoldingProvider defines the provider implementation.
type UnleashProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// ScaffoldingProviderMofunc (p *UnleashProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {del describes the provider data model.
type UnleashConfiguration struct {
	BaseUrl       types.String `tfsdk:"base_url"`
	Authorization types.String `tfsdk:"authorization"`
}

func (p *UnleashProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "unleash"
	resp.Version = p.version
}

func unleashClient(ctx context.Context, config *UnleashConfiguration, diagnostics *diag.Diagnostics) *unleash.APIClient {
	base_url := strings.TrimSuffix(configValue(config.BaseUrl, "UNLEASH_URL"), "/")
	authorization := configValue(config.Authorization, "AUTH_TOKEN")
	mustHave("base_url", base_url, diagnostics)
	mustHave("authorization", authorization, diagnostics)

	if diagnostics.HasError() {
		return nil
	}

	tflog.Debug(ctx, "Configuring Unleash client", structs.Map(config))
	tflog.Info(ctx, "Base URL: "+base_url)
	unleashConfig := unleash.NewConfiguration()
	unleashConfig.Servers = unleash.ServerConfigurations{
		unleash.ServerConfiguration{
			URL:         base_url,
			Description: "Unleash server",
		},
	}
	unleashConfig.AddDefaultHeader("Authorization", authorization)

	logLevel := strings.ToLower(os.Getenv("TF_LOG"))
	isDebug := logLevel == "debug" || logLevel == "trace"
	unleashConfig.HTTPClient = httpClient(isDebug)
	client := unleash.NewAPIClient(unleashConfig)

	return client
}

func (p *UnleashProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Unleash base URL (everything before `/api`)",
				Optional:            true,
			},
			"authorization": schema.StringAttribute{
				MarkdownDescription: "Authhorization token for Unleash API",
				Optional:            true,
				Sensitive:           true,
			},
		},
		MarkdownDescription: `Interface with [Unleash server API](https://docs.getunleash.io/reference/api/unleash). This provider implements a subset of the operations that can be done with Unleash. The focus is mostly in setting up the instance with projects, roles, permissions, groups, and other typical configuration usually performed by admins.

You can check a complete example [here](https://github.com/Unleash/terraform-provider-unleash/tree/main/examples/staged) under stage_4 folder.`,
	}
}

func configValue(configValue basetypes.StringValue, env string) string {
	if configValue.IsNull() {
		return os.Getenv(env)
	}
	return configValue.ValueString()
}

func mustHave(name string, value string, diagnostics *diag.Diagnostics) {
	if value == "" {
		diagnostics.AddError(
			"Unable to find "+name,
			name+" cannot be an empty string",
		)
	}
}

func checkIsSupportedVersion(version string, diags *diag.Diagnostics) {
	minimumVersion, _ := semver.NewVersion("5.6.0-0") // -0 is a hack to make 5.6.0 pre release version acceptable
	v, err := semver.NewVersion(version)
	if err != nil {
		diags.AddError(
			fmt.Sprintf("Unable read unleash version from string %s", version),
			err.Error(),
		)
		return
	}

	if v.Compare(minimumVersion) < 0 {
		diags.AddError(
			"Unsupported Unleash version",
			fmt.Sprintf("You're using version %s, while the provider requires at least %s", version, minimumVersion),
		)
		return
	}
}

func versionCheck(ctx context.Context, client *unleash.APIClient, diags *diag.Diagnostics) {
	unleashConfig, api_response, err := client.AdminUIAPI.GetUiConfig(ctx).Execute()

	if !ValidateApiResponse(api_response, 200, diags, err) {
		return
	}

	if !unleashConfig.VersionInfo.IsLatest {
		diags.AddWarning("You're not using the latest Unleash version, consider upgrading",
			fmt.Sprintf("You're using version %s, the latest unleash-server is %s, while the latest enterprise version is: %s", unleashConfig.Version, *unleashConfig.VersionInfo.Latest.Oss, *unleashConfig.VersionInfo.Latest.Enterprise))
	}

	checkIsSupportedVersion(unleashConfig.Version, diags)
	tflog.Info(ctx, fmt.Sprintf("Found a supported Unleash version: %s", unleashConfig.Version))
}

func (p *UnleashProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config UnleashConfiguration

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values are now available.
	client := unleashClient(ctx, &config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, "Unable to prepare client")
		return
	}

	versionCheck(ctx, client, &resp.Diagnostics)

	if resp.Diagnostics.HasError() {
		return
	}

	// Make the Inventory client available during DataSource and Resource
	// type Configure methods.
	resp.DataSourceData = client
	resp.ResourceData = client
	tflog.Info(ctx, "Configured Unleash client", map[string]any{"success": true})
}

func (p *UnleashProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewUserResource,
		NewProjectResource,
		NewApiTokenResource,
		NewRoleResource,
		NewProjectAccessResource,
	}
}

func (p *UnleashProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewUserDataSource,
		NewProjectDataSource,
		NewPermissionDataSource,
		NewRoleDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &UnleashProvider{}
	}
}
