resource "observe_maintenance_window" "db_upgrade" {
  title       = "Database upgrade"
  description = "Checkout is read-only."
  starts_at   = "2027-01-10T22:00:00Z"
  ends_at     = "2027-01-10T23:00:00Z"
  monitor_ids = [observe_monitor.shop.id]
}

resource "observe_maintenance_window" "weekly_deploys" {
  title      = "Weekly deploy"
  starts_at  = "2027-01-05T06:00:00Z"
  ends_at    = "2027-01-05T06:30:00Z"
  tags       = ["shop"]
  recurrence = "weekly"
  timezone   = "Europe/London"
}
