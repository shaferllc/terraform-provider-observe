package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/shaferllc/terraform-provider-observe/internal/client"
)

type baseDataSource struct{ data *providerData }

func (d *baseDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData != nil {
		d.data = req.ProviderData.(*providerData)
	}
}

// observe_monitors

type monitorsDataSource struct{ baseDataSource }

type monitorModel struct {
	ID             types.String `tfsdk:"id"`
	URL            types.String `tfsdk:"url"`
	Label          types.String `tfsdk:"label"`
	Tags           []string     `tfsdk:"tags"`
	Critical       types.Bool   `tfsdk:"critical"`
	OrganizationID types.String `tfsdk:"organization_id"`
	TeamID         types.String `tfsdk:"team_id"`
}

type monitorsModel struct {
	OrganizationID types.String   `tfsdk:"organization_id"`
	TeamID         types.String   `tfsdk:"team_id"`
	Search         types.String   `tfsdk:"search"`
	Tag            types.String   `tfsdk:"tag"`
	Monitors       []monitorModel `tfsdk:"monitors"`
}

func newMonitorsDataSource() datasource.DataSource { return &monitorsDataSource{} }

func (d *monitorsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitors"
}

func (d *monitorsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monitors the token can see, optionally filtered.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Only this organization's monitors"},
			"team_id":         schema.StringAttribute{Optional: true, MarkdownDescription: "Only this team's monitors"},
			"search":          schema.StringAttribute{Optional: true, MarkdownDescription: "Part of the URL or label"},
			"tag":             schema.StringAttribute{Optional: true, MarkdownDescription: "Only monitors with this tag"},
			"monitors": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"id":              schema.StringAttribute{Computed: true},
				"url":             schema.StringAttribute{Computed: true},
				"label":           schema.StringAttribute{Computed: true},
				"tags":            schema.ListAttribute{Computed: true, ElementType: types.StringType},
				"critical":        schema.BoolAttribute{Computed: true},
				"organization_id": schema.StringAttribute{Computed: true},
				"team_id":         schema.StringAttribute{Computed: true},
			}}},
		},
	}
}

func (d *monitorsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config monitorsModel
	if resp.Diagnostics.Append(req.Config.Get(ctx, &config)...); resp.Diagnostics.HasError() {
		return
	}
	query := url.Values{}
	for name, v := range map[string]types.String{"organization_id": config.OrganizationID, "team_id": config.TeamID, "search": config.Search} {
		if v.ValueString() != "" {
			query.Set(name, v.ValueString())
		}
	}
	items, err := d.data.client.List(ctx, "/monitors", query)
	if failed(&resp.Diagnostics, "Listing monitors", err) {
		return
	}
	config.Monitors = []monitorModel{}
	for _, item := range items {
		m := monitorModel{
			ID: types.StringValue(fmt.Sprint(item["id"])), URL: stringOrNull(item["url"]), Label: stringOrNull(item["label"]),
			Critical: types.BoolValue(item["critical"] == true), OrganizationID: stringOrNull(item["organization_id"]), TeamID: stringOrNull(item["team_id"]),
			Tags: []string{},
		}
		tags, _ := item["tags"].([]any)
		for _, tag := range tags {
			m.Tags = append(m.Tags, fmt.Sprint(tag))
		}
		if tag := config.Tag.ValueString(); tag != "" && !containsFold(m.Tags, tag) {
			continue
		}
		config.Monitors = append(config.Monitors, m)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// observe_organization

type organizationDataSource struct{ baseDataSource }

type organizationModel struct {
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`
	Plan types.String `tfsdk:"plan"`
	Role types.String `tfsdk:"role"`
}

func newOrganizationDataSource() datasource.DataSource { return &organizationDataSource{} }

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An organization you belong to: by `id` or `slug`, else the provider's `organization_id`, else your only one.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Optional: true, Computed: true},
			"slug": schema.StringAttribute{Optional: true, Computed: true},
			"name": schema.StringAttribute{Computed: true},
			"plan": schema.StringAttribute{Computed: true},
			"role": schema.StringAttribute{Computed: true, MarkdownDescription: "Your role: owner, admin, member or viewer"},
		},
	}
}

func (d *organizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config organizationModel
	if resp.Diagnostics.Append(req.Config.Get(ctx, &config)...); resp.Diagnostics.HasError() {
		return
	}
	items, err := d.data.client.List(ctx, "/organizations", nil)
	if failed(&resp.Diagnostics, "Listing organizations", err) {
		return
	}
	id, slug := config.ID.ValueString(), config.Slug.ValueString()
	if id == "" && slug == "" {
		id = d.data.organizationID
	}
	var match []client.Object
	for _, item := range items {
		if (id == "" && slug == "") || fmt.Sprint(item["id"]) == id || (slug != "" && item["slug"] == slug) {
			match = append(match, item)
		}
	}
	if len(match) != 1 {
		resp.Diagnostics.AddError("Finding organization", fmt.Sprintf("%d organizations match; set id or slug.", len(match)))
		return
	}
	org := match[0]
	resp.Diagnostics.Append(resp.State.Set(ctx, &organizationModel{
		ID: stringOrNull(org["id"]), Slug: stringOrNull(org["slug"]), Name: stringOrNull(org["name"]), Plan: stringOrNull(org["plan"]), Role: stringOrNull(org["role"]),
	})...)
}

// observe_regions

type regionsDataSource struct{ baseDataSource }

func newRegionsDataSource() datasource.DataSource { return &regionsDataSource{} }

func (d *regionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_regions"
}

func (d *regionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The regions uptime checks run from now: the values `uptime_regions` takes.",
		Attributes:          map[string]schema.Attribute{"ids": schema.ListAttribute{Computed: true, ElementType: types.StringType}},
	}
}

func (d *regionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var reply struct {
		Data []struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if failed(&resp.Diagnostics, "Listing regions", d.data.client.Do(ctx, http.MethodGet, "/regions", nil, &reply)) {
		return
	}
	ids := []string{}
	for _, region := range reply.Data {
		ids = append(ids, region.Key)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &struct {
		IDs []string `tfsdk:"ids"`
	}{IDs: ids})...)
}

func stringOrNull(v any) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(fmt.Sprint(v))
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
