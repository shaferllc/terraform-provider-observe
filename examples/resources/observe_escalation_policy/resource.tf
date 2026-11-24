resource "observe_escalation_policy" "default" {
  name         = "Default"
  repeat_times = 2

  steps = [
    { wait_minutes = 5, targets = [{ type = "schedule", id = observe_on_call_schedule.primary.id }] },
    { wait_minutes = 15, targets = [{ type = "channel", id = observe_notification_channel.slack.id }] },
  ]
}
