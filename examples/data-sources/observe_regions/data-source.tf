data "observe_regions" "all" {}

resource "observe_monitor" "everywhere" {
  url            = "https://example.com"
  uptime_regions = data.observe_regions.all.ids
}
