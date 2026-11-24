package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func resourceSchema(t *testing.T, name string) schema.Schema {
	t.Helper()
	for _, def := range resourceDefs {
		if def.name == name {
			var resp resource.SchemaResponse
			newResource(def)().Schema(context.Background(), resource.SchemaRequest{}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			return resp.Schema
		}
	}
	t.Fatalf("no resource %s", name)
	return schema.Schema{}
}

func TestEverySchemaIsValid(t *testing.T) {
	for _, def := range resourceDefs {
		s := resourceSchema(t, def.name)
		if diags := s.ValidateImplementation(context.Background()); diags.HasError() {
			t.Errorf("%s: %v", def.name, diags)
		}
	}
}

func TestMonitorAttributesComeFromTheSpec(t *testing.T) {
	s := resourceSchema(t, "monitor")

	interval := s.Attributes["uptime_interval_minutes"].(schema.Int64Attribute)
	if !interval.Optional || !interval.Computed {
		t.Error("uptime_interval_minutes should be optional and read back")
	}
	if url := s.Attributes["url"].(schema.StringAttribute); !url.Required {
		t.Error("url should be required")
	}
	headers := s.Attributes["uptime_check_headers"].(schema.StringAttribute)
	if !headers.Sensitive {
		t.Error("uptime_check_headers can hold credentials and should be sensitive")
	}
	if _, ok := s.Attributes["crawl_ignore_patterns"].(schema.ListAttribute); !ok {
		t.Error("crawl_ignore_patterns should be a list")
	}
	thresholds := s.Attributes["lighthouse_thresholds"].(schema.SingleNestedAttribute)
	if _, ok := thresholds.Attributes["min_score"].(schema.Int64Attribute); !ok {
		t.Error("lighthouse_thresholds.min_score should be a number")
	}
	for _, name := range []string{"ssl_monitoring_enabled", "heartbeat_cron_expression", "dns_record_monitoring_enabled", "status_slug", "uptime_regions"} {
		if _, ok := s.Attributes[name]; !ok {
			t.Errorf("monitor has no %s", name)
		}
	}
}

func TestSecretsTheAPINeverReturnsAreWriteOnly(t *testing.T) {
	s := resourceSchema(t, "notification_channel")
	webhook := s.Attributes["webhook_url"].(schema.StringAttribute)
	if webhook.Computed || !webhook.Sensitive {
		t.Errorf("webhook_url: computed %v sensitive %v, want write-only and sensitive", webhook.Computed, webhook.Sensitive)
	}
	if label := s.Attributes["label"].(schema.StringAttribute); !label.Computed {
		t.Error("label is returned and should be read back")
	}
	if _, ok := s.Attributes["all_events"]; ok {
		t.Error("all_events duplicates event_types and should not be an attribute")
	}

	page := resourceSchema(t, "status_page")
	if _, ok := page.Attributes["monitor_ids"]; !ok {
		t.Error("status_page should call site_ids monitor_ids, as the API returns it")
	}
	steps := resourceSchema(t, "escalation_policy").Attributes["steps"].(schema.ListNestedAttribute)
	if _, ok := steps.NestedObject.Attributes["targets"].(schema.ListNestedAttribute); !ok {
		t.Error("escalation_policy steps should nest their targets")
	}
}

func TestMaintenanceWindowsAreReplacedNotEdited(t *testing.T) {
	title := resourceSchema(t, "maintenance_window").Attributes["title"].(schema.StringAttribute)
	if len(title.PlanModifiers) == 0 {
		t.Error("maintenance_window title should require replacement")
	}
}

func TestValuesRoundTrip(t *testing.T) {
	headers := &shape{kind: "string", json: true}
	v := fromAPI(map[string]any{"b": "2", "a": "1"}, types.StringType, headers)
	if v.(types.String).ValueString() != `{"a":"1","b":"2"}` {
		t.Fatalf("JSON field = %s", v)
	}
	decoded, _ := toAPI(v, headers)
	if decoded.(map[string]any)["a"] != "1" {
		t.Fatalf("toAPI = %v", decoded)
	}
	if !sameMeaning(types.StringValue("{\n  \"a\": \"1\", \"b\": \"2\"\n}"), v, headers) {
		t.Error("JSON with other spacing should mean the same")
	}
	when := &shape{kind: "string", time: true}
	if !sameMeaning(types.StringValue("2026-12-01T22:00:00Z"), types.StringValue("2026-12-01T23:00:00+01:00"), when) {
		t.Error("one instant in two timezones should mean the same")
	}

	listType := types.ListType{ElemType: types.Int64Type}
	ports := fromAPI([]any{float64(22), float64(443)}, listType, &shape{kind: "list", elem: &shape{kind: "int"}})
	if got, _ := toAPI(ports, nil); len(got.([]any)) != 2 || got.([]any)[1] != int64(443) {
		t.Fatalf("ports = %v", got)
	}
	if fromAPI(nil, types.Int64Type, nil).IsNull() != true {
		t.Error("a null number should stay null")
	}
	var _ attr.Value = ports
}
