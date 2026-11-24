resource "observe_notification_channel" "slack" {
  type        = "slack"
  label       = "#ops"
  webhook_url = var.slack_webhook_url
}

resource "observe_notification_channel" "incident_bot" {
  type        = "webhook"
  label       = "Incident bot"
  webhook_url = "https://hooks.example.com/observe"
  event_types = ["uptime_incident_opened", "uptime_incident_resolved"]
}

variable "slack_webhook_url" {
  type      = string
  sensitive = true
}
