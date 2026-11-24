resource "observe_group" "storefront" {
  name        = "Storefront"
  monitor_ids = [observe_monitor.shop.id]
}
