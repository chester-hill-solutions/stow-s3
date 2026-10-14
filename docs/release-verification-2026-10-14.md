# v0.3.0 release checkpoint — 2026-10-14

The selected distribution is native GitHub downloads for Linux and macOS on
x86_64 and ARM64. npm and PyPI publication are deferred. This supersedes the
October 3 distribution checkpoint for this release; it does not change its
historical observations about registry authentication.

The release includes the storage performance changes, the smaller embedded WASM
runtime, filesystem record compatibility guidance, updated API references and
the latest merged MCP browser dependency fixes. Private benchmark charts and
their evidence are outside the release commit and website changes.

## Publication gates

`scripts/release-policy.json` explicitly selects downloads-only publication.
The release plan rejects a missing policy, tag/package version mismatch or
missing release notes. The release job still requires successful full tests,
standards, generated-output checks, four native builds and wheels, and four
packed consumers on their native Linux/macOS hosts before publishing archives
and `SHA256SUMS.txt` to GitHub.

Registry account/preflight/publication/consumer steps and the associated live
provider requirement run only when registry publication is explicitly enabled.
This release makes no executed-live-provider claim. Those checks are not replaced
by benchmark fixtures or dry-run receipts.

## Validation

The exact release commit is undergoing full code and artifact validation on
Railway. The release rehearsal and native platform consumer matrix run through
the repository's existing GitHub Actions pipeline. No builds or tests run on
the local machine. Publication and final workflow receipts will be recorded
after the checks complete.

See [v0.3.0 release notes](releases/v0.3.0.md) for installation and the filesystem
upgrade restriction: legacy records remain readable, while older Stow versions
cannot read newly written version-2 records.
