# Publishing on registry.terraform.io

The provider is ready to release. The Terraform Registry only publishes from a **public GitHub repository named
`terraform-provider-observe`** with the provider at its root, so this folder has to be pushed to its own repository.
It can't stay a subfolder of the app.

## To do (Tom)

1. **Choose the GitHub owner.** The code uses `observe-live` as a placeholder in three places: `go.mod` (module path),
   `main.go` (`Address: "registry.terraform.io/observe-live/observe"`) and the `source` in `examples/` and
   `README.md`. If the owner is different, replace `observe-live` everywhere with
   `grep -rl observe-live . | xargs sed -i '' 's/observe-live/<owner>/g'`, then run `make docs`.
2. **Create the public repository** `<owner>/terraform-provider-observe` and push this folder as its root:
   ```sh
   cd packages/terraform-provider-observe
   git init && git add . && git commit -m "Terraform provider for Observe"
   git remote add origin git@github.com:<owner>/terraform-provider-observe.git
   git push -u origin main
   ```
   Later releases: copy this folder over (or `git subtree split --prefix packages/terraform-provider-observe`) and push.
3. **Create a GPG signing key** (RSA 4096; the registry does not accept ECC keys) with no passphrase prompt in batch
   mode, or with gpg-agent caching it:
   ```sh
   gpg --full-generate-key            # RSA and RSA, 4096 bits
   gpg --list-secret-keys --keyid-format=long
   gpg --armor --export <KEY_ID>      # the public key, for step 4
   ```
   Keep the private key safe (a password manager); every release is signed with it.
4. **Sign in at registry.terraform.io with GitHub**, add the public key under *User settings → Signing keys* for the
   owner, then *Publish → Provider* and pick the repository. The registry installs a webhook and imports each new
   release tag from then on.
5. **Release** (CI here is not GitHub Actions, so releases run locally; GoReleaser is `brew install goreleaser`):
   ```sh
   git tag v0.1.0 && git push origin v0.1.0
   export GITHUB_TOKEN=<a token with repo scope>
   export GPG_FINGERPRINT=<the key's fingerprint>
   goreleaser release --clean
   ```
   GoReleaser builds every platform, writes the `SHA256SUMS` file and its signature and the registry manifest, and
   creates the GitHub release. The registry picks it up within minutes.
6. **Check** `terraform init` with `source = "<owner>/observe"` in a clean folder.

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
