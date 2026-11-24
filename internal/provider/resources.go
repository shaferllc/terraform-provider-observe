package provider

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/observe-live/terraform-provider-observe/internal/client"
)

var str = &shape{kind: "string"}

// monitorTopLevel are the createMonitor fields the Monitor schema returns at its top level; every other field is a
// check setting, returned under settings.
var monitorTopLevel = []string{"url", "label", "tags", "critical", "team_id", "organization_id"}

var resourceDefs = []resourceDef{
	{
		name: "monitor",
		description: "A website monitor. Every check type (uptime, SSL, heartbeat, crawl, Lighthouse, sitemap, robots.txt, domain, " +
			"DNS records, DNS blocklists, reverse monitor, status page) is set with the API's field names, e.g. `uptime_interval_minutes`, " +
			"`crawl_monitoring_enabled`. Settings you leave out keep the values Observe gives them. Tags are stored lowercase, with dashes " +
			"for spaces. The monitor's own status page settings (`status_slug`, `status_page_title`, `status_page_message`, `status_page_show`) " +
			"are kept only while `status_page_enabled` is true with a `status_slug`.",
		createOp: "createMonitor", updateOp: "updateMonitor",
		responses:  []string{"Monitor", "Monitor.settings"},
		collection: "/monitors", getItem: true,
		sensitive: []string{"uptime_check_body", "uptime_check_headers", "uptime_check_json_assertions", "uptime_check_header_patterns", "crawl_request_headers"},
		extra: map[string]extraAttr{
			"heartbeat_ping_url": {shape: str, description: "Ping it from your job while the heartbeat is on; append /starting or /failed"},
			"status_page_url":    {shape: str, description: "The monitor's own status page, when it is on"},
		},
		flatten: func(item client.Object) client.Object {
			flat := client.Object{}
			if settings, ok := item["settings"].(map[string]any); ok {
				for k, v := range settings {
					flat[k] = v
				}
			}
			for _, k := range append([]string{"id"}, monitorTopLevel...) {
				flat[k] = item[k]
			}
			flat["heartbeat_ping_url"] = nested(item, "heartbeat", "ping_url")
			flat["status_page_url"] = nested(item, "status_page", "url")
			return flat
		},
	},
	{
		name:        "server_monitor",
		description: "A server monitor: open and closed ports, ping, heartbeat, SSL on a port and a health URL.",
		createOp:    "createServer", updateOp: "updateServer",
		responses: []string{"Server"}, orgScoped: true, collection: "/servers",
		extra: map[string]extraAttr{"heartbeat_url": {shape: str, description: "POST to it from the server while the heartbeat is on"}},
	},
	{
		name: "notification_channel",
		description: "Where alerts go. Secrets (webhook URLs, tokens, keys) are write-only: the API never returns them, " +
			"so an imported channel needs them set again in configuration.",
		createOp: "createNotificationChannel", updateOp: "updateNotificationChannel",
		responses: []string{"NotificationChannel"}, orgScoped: true, collection: "/notification-channels",
		sensitive: []string{"webhook_url", "bot_token", "routing_key", "api_key", "api_token", "user_key", "headers"},
		skip:      []string{"all_events"},
		descriptions: map[string]string{
			"type": "Channel type, e.g. `email`, `slack`, `discord`, `teams`, `webhook`, `pagerduty`, `opsgenie`, `telegram`, `ntfy`, `pushover`. The API reference lists them all, with the fields each one needs.",
		},
		extra: map[string]extraAttr{
			"destination":    {shape: str, description: "The address, topic or host alerts go to"},
			"signing_secret": {shape: str, description: "webhook: the key for the Observe-Signature header (HMAC-SHA256 of the raw body)", sensitive: true},
		},
	},
	{
		name:        "status_page",
		description: "An additional status page.",
		createOp:    "createStatusPage", updateOp: "updateStatusPage",
		responses: []string{"StatusPage"}, orgScoped: true, collection: "/status-pages",
		rename: map[string]string{"monitor_ids": "site_ids"},
		extra:  map[string]extraAttr{"url": {shape: str, description: "Public URL"}},
	},
	{
		name: "maintenance_window",
		description: "A maintenance window: alerts pause for the monitors it covers. Windows cannot be edited, so any change replaces it; " +
			"destroying it cancels it. A finished window stays in state. A repeating window keeps the first occurrence's " +
			"`starts_at` and `ends_at` in state while Observe moves it on to the next one.",
		createOp:  "createMaintenanceWindow",
		responses: []string{"MaintenanceWindow"}, orgScoped: true, collection: "/maintenance-windows",
		extra: map[string]extraAttr{"status": {shape: str, description: "scheduled, active, completed or cancelled"}},
		gone:  func(item client.Object) bool { return item["status"] == "cancelled" },
		keep: func(prior types.Object) []string {
			if recurrence, ok := prior.Attributes()["recurrence"].(types.String); ok && recurrence.ValueString() != "" {
				return []string{"starts_at", "ends_at"}
			}
			return nil
		},
		keepMissing: func(state types.Object) bool {
			ends, ok := state.Attributes()["ends_at"].(types.String)
			at, err := time.Parse(time.RFC3339, ends.ValueString())
			return ok && err == nil && at.Before(time.Now())
		},
	},
	{
		name:        "group",
		description: "A monitor group, for reports and the dashboard.",
		createOp:    "createMonitorGroup", updateOp: "updateMonitorGroup",
		responses: []string{"MonitorGroup"}, orgScoped: true, collection: "/groups",
	},
	{
		name:        "team",
		description: "A team. Its creator becomes its owner.",
		createOp:    "createTeam", updateOp: "renameTeam",
		responses: []string{"Team"}, orgScoped: true, collection: "/teams",
	},
	{
		name:        "escalation_policy",
		description: "Who is paged for an incident, in what order, until someone acknowledges. One per team, plus one for the whole organization.",
		createOp:    "createEscalationPolicy", updateOp: "updateEscalationPolicy",
		responses: []string{"EscalationPolicy"}, orgScoped: true, collection: "/escalation-policies",
	},
	{
		name:        "on_call_schedule",
		description: "An on-call schedule: rotation layers, later ones above earlier ones. Overrides are made in the dashboard and kept.",
		createOp:    "createOnCallSchedule", updateOp: "updateOnCallSchedule",
		responses: []string{"OnCallSchedule"}, orgScoped: true, collection: "/on-call-schedules",
	},
}

func nested(item client.Object, keys ...string) any {
	var cur any = item
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func resources() []func() resource.Resource {
	out := make([]func() resource.Resource, 0, len(resourceDefs))
	for _, def := range resourceDefs {
		out = append(out, newResource(def))
	}
	return out
}
