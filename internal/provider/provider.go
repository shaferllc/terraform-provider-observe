// Package provider is the Terraform provider for Observe (observe.live).
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/shaferllc/terraform-provider-observe/internal/client"
)

type observeProvider struct {
	version string
}

type providerData struct {
	client         *client.Client
	organizationID string
}

// New returns the provider factory for providerserver.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &observeProvider{version: version} }
}

func (p *observeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "observe"
	resp.Version = p.version
}

func (p *observeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [Observe](https://observe.live) monitors, alerting and status pages as code, through the v1 API.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{Optional: true, Sensitive: true,
				MarkdownDescription: "API token with read and write access (Profile → API & data). Defaults to `OBSERVE_TOKEN`."},
			"organization_id": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Organization that resources go in unless they set their own. Defaults to `OBSERVE_ORGANIZATION_ID`."},
			"endpoint": schema.StringAttribute{Optional: true,
				MarkdownDescription: "API base URL. Defaults to `OBSERVE_ENDPOINT`, else `" + client.DefaultEndpoint + "`."},
		},
	}
}

func (p *observeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config struct {
		Token          types.String `tfsdk:"token"`
		OrganizationID types.String `tfsdk:"organization_id"`
		Endpoint       types.String `tfsdk:"endpoint"`
	}
	if resp.Diagnostics.Append(req.Config.Get(ctx, &config)...); resp.Diagnostics.HasError() {
		return
	}
	setting := func(v types.String, env string) string {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
			return v.ValueString()
		}
		return os.Getenv(env)
	}
	token := setting(config.Token, "OBSERVE_TOKEN")
	if token == "" {
		resp.Diagnostics.AddError("Missing API token", "Set token in the provider block or OBSERVE_TOKEN. Create one under Profile → API & data, with read and write access.")
		return
	}
	data := &providerData{
		client:         client.New(setting(config.Endpoint, "OBSERVE_ENDPOINT"), token, p.version),
		organizationID: setting(config.OrganizationID, "OBSERVE_ORGANIZATION_ID"),
	}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *observeProvider) Resources(_ context.Context) []func() resource.Resource {
	return resources()
}

func (p *observeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{newMonitorsDataSource, newOrganizationDataSource, newRegionsDataSource}
}
