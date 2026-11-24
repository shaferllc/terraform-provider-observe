resource "observe_monitor" "shop" {
  url   = "https://shop.example.com"
  label = "Shop"
  tags  = ["shop", "eu"]

  # Uptime
  uptime_interval_minutes     = 1
  uptime_expected_codes       = "200"
  uptime_check_string         = "Add to cart"
  uptime_alert_after_failures = 2
  uptime_check_headers        = jsonencode({ Accept = "text/html" })

  # Certificate and domain expiry
  ssl_expiring_soon_days    = 14
  domain_monitoring_enabled = true

  # Crawl for broken links and mixed content, weekly
  crawl_monitoring_enabled = true
  crawl_max_pages          = 2000
  crawl_interval_minutes   = 10080
  crawl_ignore_patterns    = ["/admin/*", "/cart"]

  # Lighthouse
  lighthouse_monitoring_enabled = true
  lighthouse_strategy           = "both"
  lighthouse_thresholds         = { min_score = 80, max_lcp_ms = 2500 }
}

# A cron heartbeat: ping heartbeat_ping_url from the job.
resource "observe_monitor" "nightly_backup" {
  url                       = "https://shop.example.com/backup"
  uptime_monitoring_enabled = false
  ssl_monitoring_enabled    = false
  heartbeat_enabled         = true
  heartbeat_cron_expression = "0 3 * * *"
  heartbeat_timezone        = "Europe/London"
  heartbeat_grace_minutes   = 15
}

output "backup_ping_url" {
  value = observe_monitor.nightly_backup.heartbeat_ping_url
}
