terraform {
  required_providers {
    observe = {
      source = "shaferllc/observe"
    }
  }
}

# The token comes from OBSERVE_TOKEN and the organization from OBSERVE_ORGANIZATION_ID when not set here.
provider "observe" {
  organization_id = "01JCX4ZK7N2Q8V3W5Y6A9B0C1D"
}
