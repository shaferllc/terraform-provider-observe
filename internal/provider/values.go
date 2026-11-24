package provider

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// toAPI turns a known Terraform value into its JSON form. A JSON-string attribute is decoded so the API gets the
// object itself.
func toAPI(v attr.Value, s *shape) (any, error) {
	if v == nil || v.IsNull() {
		return nil, nil
	}
	switch v := v.(type) {
	case basetypes.StringValue:
		if s != nil && s.json {
			var decoded any
			if err := json.Unmarshal([]byte(v.ValueString()), &decoded); err != nil {
				return nil, fmt.Errorf("not valid JSON: %w", err)
			}
			return decoded, nil
		}
		return v.ValueString(), nil
	case basetypes.Int64Value:
		return v.ValueInt64(), nil
	case basetypes.Float64Value:
		return v.ValueFloat64(), nil
	case basetypes.BoolValue:
		return v.ValueBool(), nil
	case basetypes.ListValue:
		out := []any{}
		for _, e := range v.Elements() {
			item, err := toAPI(e, elemShape(s))
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	case basetypes.MapValue:
		out := map[string]any{}
		for k, e := range v.Elements() {
			item, err := toAPI(e, elemShape(s))
			if err != nil {
				return nil, err
			}
			out[k] = item
		}
		return out, nil
	case basetypes.ObjectValue:
		out := map[string]any{}
		for k, e := range v.Attributes() {
			if e.IsUnknown() || e.IsNull() {
				continue
			}
			var prop *shape
			if s != nil {
				prop = s.props[k]
			}
			item, err := toAPI(e, prop)
			if err != nil {
				return nil, err
			}
			out[k] = item
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported value %T", v)
}

func elemShape(s *shape) *shape {
	if s == nil {
		return nil
	}
	return s.elem
}

// fromAPI turns a JSON value from the API into a Terraform value of type t.
func fromAPI(raw any, t attr.Type, s *shape) attr.Value {
	switch t := t.(type) {
	case basetypes.StringType:
		if raw == nil {
			return types.StringNull()
		}
		if s != nil && s.json {
			encoded, _ := json.Marshal(raw)
			return types.StringValue(string(encoded))
		}
		if str, ok := raw.(string); ok {
			return types.StringValue(str)
		}
		return types.StringValue(fmt.Sprint(raw))
	case basetypes.Int64Type:
		if n, ok := number(raw); ok {
			return types.Int64Value(int64(n))
		}
		return types.Int64Null()
	case basetypes.Float64Type:
		if n, ok := number(raw); ok {
			return types.Float64Value(n)
		}
		return types.Float64Null()
	case basetypes.BoolType:
		if b, ok := raw.(bool); ok {
			return types.BoolValue(b)
		}
		return types.BoolNull()
	case basetypes.ListType:
		items, ok := raw.([]any)
		if !ok {
			return types.ListNull(t.ElemType)
		}
		elems := make([]attr.Value, 0, len(items))
		for _, item := range items {
			elems = append(elems, fromAPI(item, t.ElemType, elemShape(s)))
		}
		return types.ListValueMust(t.ElemType, elems)
	case basetypes.MapType:
		items, ok := raw.(map[string]any)
		if !ok {
			return types.MapNull(t.ElemType)
		}
		elems := map[string]attr.Value{}
		for k, item := range items {
			elems[k] = fromAPI(item, t.ElemType, elemShape(s))
		}
		return types.MapValueMust(t.ElemType, elems)
	case basetypes.ObjectType:
		items, ok := raw.(map[string]any)
		if !ok {
			return types.ObjectNull(t.AttrTypes)
		}
		attrs := map[string]attr.Value{}
		for k, at := range t.AttrTypes {
			var prop *shape
			if s != nil {
				prop = s.props[k]
			}
			attrs[k] = fromAPI(items[k], at, prop)
		}
		return types.ObjectValueMust(t.AttrTypes, attrs)
	}
	panic(fmt.Sprintf("observe: unsupported type %T", t))
}

func number(raw any) (float64, bool) {
	switch n := raw.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case *big.Float:
		f, _ := n.Float64()
		return f, true
	}
	return 0, false
}

// sameMeaning reports whether prior and next say the same thing although they are spelled differently: JSON
// with other spacing or key order, or one instant in another timezone. Prior is then kept, so the plan stays clean.
func sameMeaning(prior, next attr.Value, s *shape) bool {
	p, ok1 := prior.(basetypes.StringValue)
	n, ok2 := next.(basetypes.StringValue)
	if !ok1 || !ok2 || p.IsNull() || p.IsUnknown() || n.IsNull() || s == nil {
		return false
	}
	switch {
	case s.json:
		var a, b any
		return json.Unmarshal([]byte(p.ValueString()), &a) == nil && json.Unmarshal([]byte(n.ValueString()), &b) == nil && reflect.DeepEqual(a, b)
	case s.time:
		a, err1 := time.Parse(time.RFC3339, p.ValueString())
		b, err2 := time.Parse(time.RFC3339, n.ValueString())
		return err1 == nil && err2 == nil && a.Equal(b)
	}
	return false
}

// nullOf is an empty value of type t, for computed attributes the API did not fill in.
func nullOf(t attr.Type) attr.Value {
	switch t := t.(type) {
	case basetypes.StringType:
		return types.StringNull()
	case basetypes.Int64Type:
		return types.Int64Null()
	case basetypes.Float64Type:
		return types.Float64Null()
	case basetypes.BoolType:
		return types.BoolNull()
	case basetypes.ListType:
		return types.ListNull(t.ElemType)
	case basetypes.MapType:
		return types.MapNull(t.ElemType)
	case basetypes.ObjectType:
		return types.ObjectNull(t.AttrTypes)
	}
	panic(fmt.Sprintf("observe: unsupported type %T", t))
}
