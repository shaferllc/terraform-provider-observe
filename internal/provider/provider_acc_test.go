package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Acceptance tests drive Terraform itself against the fake API. They run with TF_ACC=1 and need a terraform
// binary on PATH (go test ./... skips them).

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"observe": providerserver.NewProtocol6WithError(New("test")()),
}

func providerBlock(url string) string {
	return fmt.Sprintf(`
provider "observe" {
  endpoint        = %q
  token           = "test-token"
  organization_id = %q
}
`, url, fakeOrg)
}

func TestAccMonitorWithStatusPage(t *testing.T) {
	server := newFakeAPI(t)
	config := func(interval int) string {
		return providerBlock(server.URL) + fmt.Sprintf(`
resource "observe_monitor" "shop" {
  url                      = "https://shop.example.com"
  label                    = "Shop"
  tags                     = ["shop", "eu"]
  uptime_interval_minutes  = %d
  uptime_check_headers     = jsonencode({ Accept = "text/html" })
  crawl_monitoring_enabled = true
  crawl_ignore_patterns    = ["/admin/*"]
  heartbeat_enabled        = true
  lighthouse_thresholds    = { min_score = 80 }
}

resource "observe_status_page" "public" {
  title       = "Shop status"
  slug        = "shop"
  monitor_ids = [observe_monitor.shop.id]
}
`, interval)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config(1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("observe_monitor.shop", "uptime_interval_minutes", "1"),
					resource.TestCheckResourceAttr("observe_monitor.shop", "organization_id", fakeOrg),
					resource.TestCheckResourceAttr("observe_monitor.shop", "ssl_monitoring_enabled", "true"),
					resource.TestCheckResourceAttr("observe_monitor.shop", "lighthouse_thresholds.min_score", "80"),
					resource.TestCheckResourceAttr("observe_monitor.shop", "lighthouse_thresholds.max_lcp_ms", "4000"),
					resource.TestCheckResourceAttr("observe_monitor.shop", "heartbeat_ping_url", "https://observe.live/heartbeat/abc"),
					resource.TestCheckResourceAttrPair("observe_status_page.public", "monitor_ids.0", "observe_monitor.shop", "id"),
					resource.TestCheckResourceAttr("observe_status_page.public", "url", "https://observe.live/status/shop"),
					func(*terraform.State) error {
						sent := fakes[server.URL].requests["POST /monitors"][0]
						if sent["organization_id"] != fakeOrg || sent["uptime_check_headers"].(map[string]any)["Accept"] != "text/html" {
							return fmt.Errorf("create body = %v", sent)
						}
						return nil
					},
				),
			},
			{
				Config: config(5),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("observe_monitor.shop", "uptime_interval_minutes", "5"),
					func(*terraform.State) error {
						patches := fakes[server.URL].requests["PATCH /monitors/id1"]
						if len(patches) != 1 || len(patches[0]) != 1 || patches[0]["uptime_interval_minutes"] != float64(5) {
							return fmt.Errorf("PATCH bodies = %v, want only the changed field", patches)
						}
						return nil
					},
				),
			},
			{ResourceName: "observe_monitor.shop", ImportState: true, ImportStateVerify: true},
			{ResourceName: "observe_status_page.public", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: orgScopedID("observe_status_page.public")},
		},
	})
}

func TestAccOnCallAndAlerting(t *testing.T) {
	server := newFakeAPI(t)
	config := providerBlock(server.URL) + `
resource "observe_team" "web" {
  name = "Web"
}

resource "observe_notification_channel" "hook" {
  type        = "webhook"
  label       = "Incident bot"
  webhook_url = "https://hooks.example.com/observe"
  event_types = ["uptime_incident_opened", "uptime_incident_resolved"]
}

resource "observe_on_call_schedule" "primary" {
  name     = "Primary"
  timezone = "Europe/London"
  layers = [{
    rotation_hours = 168
    handoff_time   = "09:00"
    starts_on      = "2026-12-07"
    user_ids       = ["user1", "user2"]
  }]
}

resource "observe_escalation_policy" "default" {
  name         = "Default"
  repeat_times = 1
  steps = [
    { wait_minutes = 5, targets = [{ type = "schedule", id = observe_on_call_schedule.primary.id }] },
    { wait_minutes = 15, targets = [{ type = "channel", id = observe_notification_channel.hook.id }] },
  ]
}

resource "observe_maintenance_window" "deploy" {
  title     = "Deploy"
  starts_at = "2030-01-01T22:00:00Z"
  ends_at   = "2030-01-01T23:00:00Z"
  tags      = ["shop"]
}

resource "observe_server_monitor" "web1" {
  name                = "web-1"
  host                = "web-1.example.com"
  ports_expected_open = [22, 443]
  ping_enabled        = true
}

resource "observe_group" "shop" {
  name = "Shop"
}

data "observe_organization" "current" {}
data "observe_regions" "all" {}
data "observe_monitors" "all" {}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("observe_notification_channel.hook", "destination", "hooks.example.com"),
					resource.TestCheckResourceAttr("observe_notification_channel.hook", "webhook_url", "https://hooks.example.com/observe"),
					resource.TestCheckResourceAttr("observe_on_call_schedule.primary", "layers.0.user_ids.1", "user2"),
					resource.TestCheckResourceAttr("observe_on_call_schedule.primary", "handoff_emails", "false"),
					resource.TestCheckResourceAttrPair("observe_escalation_policy.default", "steps.0.targets.0.id", "observe_on_call_schedule.primary", "id"),
					resource.TestCheckResourceAttr("observe_maintenance_window.deploy", "status", "scheduled"),
					resource.TestCheckResourceAttr("observe_server_monitor.web1", "ports_expected_open.1", "443"),
					resource.TestCheckResourceAttr("data.observe_organization.current", "slug", "acme"),
					resource.TestCheckResourceAttr("data.observe_regions.all", "ids.1", "ap-southeast"),
				),
			},
			{ResourceName: "observe_escalation_policy.default", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: orgScopedID("observe_escalation_policy.default")},
			{ResourceName: "observe_on_call_schedule.primary", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: orgScopedID("observe_on_call_schedule.primary")},
			// The channel's secrets are write-only, so an import reads everything but them.
			{ResourceName: "observe_notification_channel.hook", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"webhook_url"}},
		},
	})
}

func TestAccMissingTokenIsExplained(t *testing.T) {
	t.Setenv("OBSERVE_TOKEN", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      `data "observe_regions" "all" {}`,
			ExpectError: regexp.MustCompile("Missing API token"),
		}},
	})
}

func orgScopedID(name string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		return fakeOrg + "/" + s.RootModule().Resources[name].Primary.ID, nil
	}
}

func TestAccRepeatingMaintenanceWindowSurvivesRollingForward(t *testing.T) {
	server := newFakeAPI(t)
	config := providerBlock(server.URL) + `
resource "observe_maintenance_window" "weekly" {
  title      = "Weekly deploy"
  starts_at  = "2030-01-01T06:00:00Z"
  ends_at    = "2030-01-01T06:30:00Z"
  recurrence = "weekly"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				// Observe moves a repeating window on to its next occurrence; that is not a change to plan.
				PreConfig: func() {
					f := fakes[server.URL]
					f.mu.Lock()
					defer f.mu.Unlock()
					for _, item := range f.items["/organizations/"+fakeOrg+"/maintenance-windows"] {
						item["starts_at"], item["ends_at"] = "2030-01-08T06:00:00+00:00", "2030-01-08T06:30:00+00:00"
					}
				},
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
