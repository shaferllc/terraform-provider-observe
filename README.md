# Terraform provider for Observe

Manage [Observe](https://observe.live) monitors, server monitors, alert channels, status pages, maintenance
windows, groups, teams, on-call schedules and escalation policies as code. Docs: https://observe.live/docs/terraform
and [docs/](docs/) (the registry's format).

```terraform
terraform {
  required_providers {
    observe = { source = "observe-live/observe" }
  }
}

provider "observe" {} # OBSERVE_TOKEN, OBSERVE_ORGANIZATION_ID

resource "observe_monitor" "shop" {
  url                     = "https://shop.example.com"
  uptime_interval_minutes = 1
}
```

## How it is built

Resource attributes are not written by hand: they come from the request bodies in
[`internal/provider/openapi.json`](internal/provider/openapi.json), a copy of the API's own description
(`/api/openapi.json`). Fields the API also returns are read back and drift is shown in plans; the rest (secrets)
are write-only. `internal/provider/resources.go` says, per resource, which operations and response schemas to use.

When the API gains a field: `make spec docs`, then `make testacc`.

## Development

- `make test`: unit tests.
- `make testacc`: acceptance tests. Terraform runs against an in-memory fake of the API
  (`internal/provider/fake_api_test.go`); no account needed.
- `make install`, then a `dev_overrides` block in `~/.terraformrc` pointing `observe-live/observe` at `~/go/bin`
  to try a local build.

Releases: see [LISTING.md](LISTING.md).
