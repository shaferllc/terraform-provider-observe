# Releasing on registry.terraform.io

Published as [`shaferllc/observe`](https://registry.terraform.io/providers/shaferllc/observe) from the public
repository [shaferllc/terraform-provider-observe](https://github.com/shaferllc/terraform-provider-observe). The
Observe app repository (`packages/terraform-provider-observe`) is the source of truth; never commit to the public
repository directly.

## Signing key

Releases are signed with the RSA 4096 key "Shafer LLC Terraform Provider Signing"
(`004240C3A3AA14154E1D9B06E09F640C5EA4B0C0`, expires 2028-11-24), which is registered on the `shaferllc` namespace
in the registry. The private key is in Tom's gpg keyring; its passphrase is in the macOS keychain
(`security find-generic-password -s terraform-provider-signing -a tshafer -w`). Before the key expires, extend it
(`gpg --quick-set-expire <fingerprint> 2y`) and upload the refreshed public key to the registry.

## Releasing

1. In the app repository, on master: `scripts/sync-terraform-repo`. It pushes this folder's history to the public
   repository's `main`, with Tom's GitHub noreply address as author and committer.
2. In a clone of the public repository, tag with the noreply address:
   ```sh
   git -c user.name='Tom Shafer' -c user.email=299464+tshafer@users.noreply.github.com tag -a v0.1.1 -m v0.1.1
   git push origin v0.1.1
   ```
3. Unlock the key for gpg-agent (GoReleaser signs in batch mode), then release:
   ```sh
   export GPG_FINGERPRINT=004240C3A3AA14154E1D9B06E09F640C5EA4B0C0
   security find-generic-password -s terraform-provider-signing -a tshafer -w |
     gpg --batch --pinentry-mode loopback --passphrase-fd 0 --local-user "$GPG_FINGERPRINT" --detach-sign -o /dev/null /etc/hosts
   GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
   ```
   GoReleaser builds every platform, writes `SHA256SUMS`, its signature and the registry manifest, and creates
   the GitHub release. The registry's webhook imports it within minutes.
4. Check with `terraform init` and `source = "shaferllc/observe"` in a clean folder.

## Before each release

- `make spec`: refresh `internal/provider/openapi.json` from https://observe.live/api/openapi.json. New API fields
  become attributes.
- `make docs` and `make testacc` (needs `terraform` on PATH).
- Bump the tag: patch for fixes, minor for new attributes or resources.

## Listing copy

- **Name:** observe
- **Description:** Manage Observe uptime, SSL, crawl, Lighthouse, heartbeat and server monitors, alert channels,
  status pages, maintenance windows, teams, on-call schedules and escalation policies.
- **Category:** Infrastructure Management (Monitoring)
