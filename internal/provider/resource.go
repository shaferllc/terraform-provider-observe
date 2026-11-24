package provider

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observe-live/terraform-provider-observe/internal/client"
)

// resourceDef describes one API resource; its attributes come from the create operation's request body in the
// spec. Fields the response schemas also list are read back (Optional + Computed); the rest are write-only
// (Optional; the API never returns them, so Terraform keeps what you set).
type resourceDef struct {
	name        string // observe_<name>
	description string
	createOp    string
	updateOp    string   // empty: no edits, every change replaces it
	responses   []string // component schemas read back, e.g. "Monitor", "Monitor.settings"
	orgScoped   bool     // paths start /organizations/{organization_id}
	collection  string   // e.g. "/servers" (after the organization) or "/monitors"
	getItem     bool     // GET collection/{id} works; otherwise read by listing
	rename      map[string]string
	skip        []string
	sensitive   []string
	extra       map[string]extraAttr // read-only attributes filled by flatten
	// keep names attributes to leave as they are in state on a read (a repeating maintenance window's
	// times, which Observe moves on to the next occurrence).
	keep func(prior types.Object) []string
	// descriptions replace the spec's for these attributes.
	descriptions map[string]string
	flatten      func(client.Object) client.Object
	// gone: the API still lists it, but it is over (a cancelled maintenance window).
	gone func(client.Object) bool
	// keepMissing: it dropped out of the list but still exists (a finished maintenance window).
	keepMissing func(types.Object) bool
}

type extraAttr struct {
	shape       *shape
	description string
	sensitive   bool
}

type field struct {
	name, api string
	shape     *shape
	readable  bool
	required  bool
	sensitive bool
}

type genericResource struct {
	def    resourceDef
	fields []field
	types  map[string]attr.Type
	data   *providerData
}

var (
	_ resource.Resource                = (*genericResource)(nil)
	_ resource.ResourceWithImportState = (*genericResource)(nil)
	_ resource.ResourceWithConfigure   = (*genericResource)(nil)
)

func newResource(def resourceDef) func() resource.Resource {
	return func() resource.Resource {
		r := &genericResource{def: def, types: map[string]attr.Type{"id": types.StringType, "organization_id": types.StringType}}
		body := requestBody(def.createOp)
		readable := map[string]bool{}
		for _, component := range def.responses {
			for name := range componentProperties(component) {
				readable[name] = true
			}
		}
		apiToTF := map[string]string{}
		for tf, api := range def.rename {
			apiToTF[api] = tf
		}
		for _, api := range sortedKeys(body.Properties) {
			if api == "organization_id" || slices.Contains(def.skip, api) {
				continue
			}
			name := api
			if tf, ok := apiToTF[api]; ok {
				name = tf
			}
			f := field{name: name, api: api, shape: shapeOf(body.Properties[api]), readable: readable[name] || readable[api],
				required: slices.Contains(body.Required, api), sensitive: slices.Contains(def.sensitive, api)}
			r.fields = append(r.fields, f)
			r.types[name] = f.shape.attrType()
		}
		for name, extra := range def.extra {
			r.types[name] = extra.shape.attrType()
		}
		return r
	}
}

func (r *genericResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.def.name
}

func (r *genericResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	forceNew := r.def.updateOp == ""
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "ID", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"organization_id": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Organization ID; defaults to the provider's `organization_id`",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
	}
	for _, f := range r.fields {
		m := mode{required: f.required, optional: !f.required, computed: f.readable && !f.required, sensitive: f.sensitive, forceNew: forceNew}
		description := describe(f.shape, r.def.descriptions[f.name])
		if !f.readable && !f.required {
			description = sentence(description) + "Write-only: the API does not return it."
		}
		attrs[f.name] = f.shape.attribute(m, description)
	}
	for name, extra := range r.def.extra {
		attrs[name] = extra.shape.attribute(mode{computed: true, sensitive: extra.sensitive}, extra.description)
	}
	resp.Schema = schema.Schema{MarkdownDescription: r.def.description, Attributes: attrs}
}

func (r *genericResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.data = req.ProviderData.(*providerData)
	}
}

func (r *genericResource) orgID(obj types.Object) string {
	if v, ok := obj.Attributes()["organization_id"].(types.String); ok && !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return r.data.organizationID
}

func (r *genericResource) collectionPath(obj types.Object) (string, error) {
	if !r.def.orgScoped {
		return r.def.collection, nil
	}
	org := r.orgID(obj)
	if org == "" {
		return "", fmt.Errorf("set organization_id on the resource, or on the provider (OBSERVE_ORGANIZATION_ID)")
	}
	return "/organizations/" + org + r.def.collection, nil
}

func (r *genericResource) body(plan, prior types.Object, onlyChanges bool) (map[string]any, error) {
	body := map[string]any{}
	planAttrs := plan.Attributes()
	var priorAttrs map[string]attr.Value
	if onlyChanges {
		priorAttrs = prior.Attributes()
	}
	for _, f := range r.fields {
		v := planAttrs[f.name]
		if v.IsUnknown() || (!onlyChanges && v.IsNull()) {
			continue
		}
		if onlyChanges && v.Equal(priorAttrs[f.name]) {
			continue
		}
		value, err := toAPI(v, f.shape)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.name, err)
		}
		body[f.api] = value
	}
	if !r.def.orgScoped {
		if _, ok := r.types["organization_id"]; ok && !onlyChanges {
			if org := r.orgID(plan); org != "" {
				body["organization_id"] = org
			}
		}
	}
	return body, nil
}

// merge lays the API's reply over base (the plan or the prior state).
func (r *genericResource) merge(base types.Object, item client.Object) types.Object {
	if r.def.flatten != nil {
		item = r.def.flatten(item)
	}
	attrs := map[string]attr.Value{}
	for k, v := range base.Attributes() {
		attrs[k] = v
	}
	if id, ok := item["id"]; ok && id != nil {
		attrs["id"] = types.StringValue(fmt.Sprint(id))
	}
	if org, ok := item["organization_id"].(string); ok && org != "" {
		attrs["organization_id"] = types.StringValue(org)
	} else if v := attrs["organization_id"]; v.IsUnknown() || v.IsNull() {
		attrs["organization_id"] = types.StringValue(r.orgID(base))
	}
	var keep []string
	if r.def.keep != nil {
		keep = r.def.keep(base)
	}
	for _, f := range r.fields {
		if cur := attrs[f.name]; slices.Contains(keep, f.name) && !cur.IsUnknown() && !cur.IsNull() {
			continue
		}
		raw, present := item[f.api]
		if !present {
			raw, present = item[f.name]
		}
		cur := attrs[f.name]
		if f.readable && present {
			next := fromAPI(raw, r.types[f.name], f.shape)
			// Credentials come back null to tokens that cannot edit the monitor: keep what was set.
			if !(f.sensitive && next.IsNull()) && !sameMeaning(cur, next, f.shape) {
				attrs[f.name] = next
			}
		}
		if attrs[f.name].IsUnknown() {
			attrs[f.name] = nullOf(r.types[f.name])
		}
	}
	for name := range r.def.extra {
		attrs[name] = fromAPI(item[name], r.types[name], r.def.extra[name].shape)
	}
	return types.ObjectValueMust(r.types, attrs)
}

func (r *genericResource) fetch(ctx context.Context, state types.Object) (client.Object, error) {
	collection, err := r.collectionPath(state)
	if err != nil {
		return nil, err
	}
	id := state.Attributes()["id"].(types.String).ValueString()
	if r.def.getItem {
		return r.data.client.Data(ctx, http.MethodGet, collection+"/"+id, nil)
	}
	return r.data.client.Find(ctx, collection, id)
}

func (r *genericResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan types.Object
	if resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...); resp.Diagnostics.HasError() {
		return
	}
	collection, err := r.collectionPath(plan)
	body, err2 := r.body(plan, plan, false)
	if failed(&resp.Diagnostics, "Creating "+r.def.name, err, err2) {
		return
	}
	item, err := r.data.client.Data(ctx, http.MethodPost, collection, body)
	if failed(&resp.Diagnostics, "Creating "+r.def.name, err) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, r.merge(plan, item))...)
}

func (r *genericResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state types.Object
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	item, err := r.fetch(ctx, state)
	if client.IsNotFound(err) {
		if r.def.keepMissing != nil && r.def.keepMissing(state) {
			return
		}
		resp.State.RemoveResource(ctx)
		return
	}
	if failed(&resp.Diagnostics, "Reading "+r.def.name, err) {
		return
	}
	if r.def.gone != nil && r.def.gone(item) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, r.merge(state, item))...)
}

func (r *genericResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	body, err := r.body(plan, state, true)
	if failed(&resp.Diagnostics, "Updating "+r.def.name, err) {
		return
	}
	var item client.Object
	if len(body) == 0 {
		item, err = r.fetch(ctx, state)
	} else {
		collection, cerr := r.collectionPath(state)
		if failed(&resp.Diagnostics, "Updating "+r.def.name, cerr) {
			return
		}
		item, err = r.data.client.Data(ctx, http.MethodPatch, collection+"/"+state.Attributes()["id"].(types.String).ValueString(), body)
	}
	if failed(&resp.Diagnostics, "Updating "+r.def.name, err) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, r.merge(plan, item))...)
}

func (r *genericResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state types.Object
	if resp.Diagnostics.Append(req.State.Get(ctx, &state)...); resp.Diagnostics.HasError() {
		return
	}
	collection, err := r.collectionPath(state)
	if failed(&resp.Diagnostics, "Deleting "+r.def.name, err) {
		return
	}
	err = r.data.client.Do(ctx, http.MethodDelete, collection+"/"+state.Attributes()["id"].(types.String).ValueString(), nil, nil)
	if err != nil && !client.IsNotFound(err) {
		failed(&resp.Diagnostics, "Deleting "+r.def.name, err)
	}
}

// ImportState takes "<organization_id>/<id>" for organization resources (or "<id>" with the provider's
// organization_id), and "<id>" for monitors.
func (r *genericResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	org, id, found := strings.Cut(req.ID, "/")
	if !found {
		org, id = r.data.organizationID, req.ID
	}
	if id == "" || (r.def.orgScoped && org == "") {
		resp.Diagnostics.AddError("Importing "+r.def.name, "Use "+r.importID()+".")
		return
	}
	attrs := map[string]attr.Value{}
	for name, t := range r.types {
		attrs[name] = nullOf(t)
	}
	attrs["id"] = types.StringValue(id)
	if org != "" {
		attrs["organization_id"] = types.StringValue(org)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, types.ObjectValueMust(r.types, attrs))...)
}

func (r *genericResource) importID() string {
	if r.def.orgScoped {
		return "<organization_id>/<id>, or <id> with the provider's organization_id"
	}
	return "<id>"
}

func failed(diags *diag.Diagnostics, summary string, errs ...error) bool {
	for _, err := range errs {
		if err != nil {
			diags.AddError(summary, err.Error())
			return true
		}
	}
	return false
}
