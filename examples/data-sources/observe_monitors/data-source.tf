data "observe_monitors" "shop" {
  tag = "shop"
}

output "shop_monitor_urls" {
  value = [for m in data.observe_monitors.shop.monitors : m.url]
}
