resource "observe_on_call_schedule" "primary" {
  name     = "Primary"
  timezone = "Europe/London"

  layers = [
    # Weekly rotation, handing off Mondays at 09:00
    {
      rotation_hours = 168
      handoff_time   = "09:00"
      starts_on      = "2027-01-04"
      user_ids       = ["USER_ID_ANA", "USER_ID_BEN"]
    },
    # Nights are covered by a separate daily rotation, above the weekly one
    {
      rotation_hours    = 24
      handoff_time      = "18:00"
      starts_on         = "2027-01-04"
      restriction_start = "18:00"
      restriction_end   = "08:00"
      user_ids          = ["USER_ID_CAM"]
    },
  ]
}
