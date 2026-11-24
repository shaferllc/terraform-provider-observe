resource "observe_status_page" "public" {
  title        = "Acme status"
  slug         = "acme"
  monitor_ids  = [observe_monitor.shop.id]
  accent_color = "#0f766e"
  auto_updates = true
}
