resource "observe_server_monitor" "web1" {
  name                  = "web-1"
  host                  = "web-1.example.com"
  ports_expected_open   = [22, 443]
  ports_expected_closed = [3306]
  ping_enabled          = true
  heartbeat_enabled     = true
  ssl_check_enabled     = true
  ssl_check_port        = 443
}
