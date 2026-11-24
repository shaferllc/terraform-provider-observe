package provider

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// openapi.json is the API's own description (/api/openapi.json). Resource schemas are built from its request
// bodies, so a new API field becomes a provider attribute by refreshing this file: make spec.
//
//go:embed openapi.json
var openAPIJSON []byte

type openAPI struct {
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]jsonSchema `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	OperationID string `json:"operationId"`
	RequestBody struct {
		Content map[string]struct {
			Schema jsonSchema `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

type jsonSchema struct {
	Ref                  string                `json:"$ref"`
	Type                 any                   `json:"type"`
	Format               string                `json:"format"`
	Description          string                `json:"description"`
	Properties           map[string]jsonSchema `json:"properties"`
	Items                *jsonSchema           `json:"items"`
	AdditionalProperties any                   `json:"additionalProperties"`
	Required             []string              `json:"required"`
	Enum                 []any                 `json:"enum"`
}

func (s jsonSchema) types() []string {
	switch t := s.Type.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			if str, ok := v.(string); ok && str != "null" {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

var spec = func() openAPI {
	var doc openAPI
	if err := json.Unmarshal(openAPIJSON, &doc); err != nil {
		panic("observe: embedded openapi.json: " + err.Error())
	}
	return doc
}()

// requestBody returns the JSON body schema of an operation.
func requestBody(operationID string) jsonSchema {
	for _, ops := range spec.Paths {
		for _, op := range ops {
			if op.OperationID == operationID {
				return op.RequestBody.Content["application/json"].Schema
			}
		}
	}
	panic("observe: no operation " + operationID + " in openapi.json")
}

// componentProperties returns a response schema's property names; "Monitor.settings" reaches one level in.
func componentProperties(path string) map[string]jsonSchema {
	name, nested, _ := strings.Cut(path, ".")
	s, ok := spec.Components.Schemas[name]
	if !ok {
		panic("observe: no schema " + name + " in openapi.json")
	}
	if nested != "" {
		s = s.Properties[nested]
	}
	return s.Properties
}

// shape is the Terraform view of a JSON schema.
type shape struct {
	kind        string // string, int, float, bool, list, map, object
	elem        *shape // list and map elements
	props       map[string]*shape
	required    map[string]bool
	description string
	json        bool // a string holding JSON (free-form objects such as request headers)
	time        bool // an RFC 3339 timestamp
	enum        []string
}

func enumValues(s jsonSchema) []string {
	var out []string
	for _, v := range s.Enum {
		if v != nil {
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}

func shapeOf(s jsonSchema) *shape {
	t := s.types()
	has := func(kind string) bool { return slices.Contains(t, kind) }
	out := &shape{description: s.Description, enum: enumValues(s)}
	switch {
	case has("object") && len(s.Properties) > 0:
		out.kind, out.props, out.required = "object", map[string]*shape{}, map[string]bool{}
		for name, prop := range s.Properties {
			out.props[name] = shapeOf(prop)
		}
		for _, name := range s.Required {
			out.required[name] = true
		}
	case has("object") && !has("array"):
		if extra, ok := s.AdditionalProperties.(map[string]any); ok && (extra["type"] == "string" || extra["enum"] != nil) {
			out.kind, out.elem = "map", &shape{kind: "string"}
		} else {
			out.kind, out.json = "string", true
		}
	case has("object"):
		out.kind, out.json = "string", true
	case has("array"):
		out.kind, out.elem = "list", &shape{kind: "string"}
		if s.Items != nil {
			out.elem = shapeOf(*s.Items)
		}
	case has("integer"):
		out.kind = "int"
	case has("number"):
		out.kind = "float"
	case has("boolean"):
		out.kind = "bool"
	default:
		out.kind, out.time = "string", s.Format == "date-time"
	}
	return out
}

func (s *shape) attrType() attr.Type {
	switch s.kind {
	case "int":
		return types.Int64Type
	case "float":
		return types.Float64Type
	case "bool":
		return types.BoolType
	case "list":
		return types.ListType{ElemType: s.elem.attrType()}
	case "map":
		return types.MapType{ElemType: s.elem.attrType()}
	case "object":
		attrTypes := map[string]attr.Type{}
		for name, prop := range s.props {
			attrTypes[name] = prop.attrType()
		}
		return types.ObjectType{AttrTypes: attrTypes}
	}
	return types.StringType
}

// mode says how an attribute is set.
type mode struct {
	required, optional, computed, sensitive, forceNew bool
}

func (s *shape) attribute(m mode, description string) schema.Attribute {
	keep := m.computed && !m.required
	switch s.kind {
	case "int":
		return schema.Int64Attribute{Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
			PlanModifiers: mods(keep, m.forceNew, int64planmodifier.UseStateForUnknown, int64planmodifier.RequiresReplace)}
	case "float":
		return schema.Float64Attribute{Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
			PlanModifiers: mods(keep, m.forceNew, float64planmodifier.UseStateForUnknown, float64planmodifier.RequiresReplace)}
	case "bool":
		return schema.BoolAttribute{Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
			PlanModifiers: mods(keep, m.forceNew, boolplanmodifier.UseStateForUnknown, boolplanmodifier.RequiresReplace)}
	case "map":
		return schema.MapAttribute{ElementType: s.elem.attrType(), Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
			PlanModifiers: mods(keep, m.forceNew, mapplanmodifier.UseStateForUnknown, mapplanmodifier.RequiresReplace)}
	case "list":
		if s.elem.kind == "object" {
			return schema.ListNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: s.elem.nestedAttributes(false)},
				Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
				PlanModifiers: mods(keep, m.forceNew, listplanmodifier.UseStateForUnknown, listplanmodifier.RequiresReplace)}
		}
		return schema.ListAttribute{ElementType: s.elem.attrType(), Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
			PlanModifiers: mods(keep, m.forceNew, listplanmodifier.UseStateForUnknown, listplanmodifier.RequiresReplace)}
	case "object":
		return schema.SingleNestedAttribute{Attributes: s.nestedAttributes(m.computed), Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
			PlanModifiers: mods(keep, m.forceNew, objectplanmodifier.UseStateForUnknown, objectplanmodifier.RequiresReplace)}
	}
	return schema.StringAttribute{Required: m.required, Optional: m.optional, Computed: m.computed, Sensitive: m.sensitive, MarkdownDescription: description,
		PlanModifiers: mods(keep, m.forceNew, stringplanmodifier.UseStateForUnknown, stringplanmodifier.RequiresReplace)}
}

// nestedAttributes: the object's required properties are required, the rest optional (and filled in by the API
// when computed, as for lighthouse_thresholds).
func (s *shape) nestedAttributes(computed bool) map[string]schema.Attribute {
	out := map[string]schema.Attribute{}
	for name, prop := range s.props {
		required := s.required[name]
		out[name] = prop.attribute(mode{required: required, optional: !required, computed: computed && !required}, describe(prop, ""))
	}
	return out
}

func mods[T any](keep, forceNew bool, useState func() T, replace func() T) []T {
	var out []T
	if keep {
		out = append(out, useState())
	}
	if forceNew {
		out = append(out, replace())
	}
	return out
}

func describe(s *shape, fallback string) string {
	d := s.description
	if fallback != "" {
		d = fallback
	}
	if len(s.enum) > 0 && fallback == "" {
		d = sentence(d) + "One of `" + strings.Join(s.enum, "`, `") + "`."
	}
	if s.json {
		d = sentence(d) + "JSON: use `jsonencode()`."
	}
	return strings.TrimSpace(d)
}

// sentence ends d with a full stop and a space, ready for the next sentence.
func sentence(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if !strings.HasSuffix(d, ".") {
		d += "."
	}
	return d + " "
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
