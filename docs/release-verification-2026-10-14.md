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

Implementation and release-policy commit
`55fc5282f6b40cac6631e88761ed2fa7c6929fad` passed full Railway qualification:
`make test-all`, `make standards`, generated-output checks, pinned protobuf
regeneration, real browser tests and four native archive builds. Browser results
were 34 passed and two optional skips, with no failures or flaky cases. Tracked
SDK/API/browser/baseline artifacts did not drift. No builds or tests ran locally.

[Release rehearsal 38620413286](https://github.com/chester-hill-solutions/stow-s3/actions/runs/38620413286)
passed full release verification, four native builds/wheels, four packed consumers
on their native Linux/macOS hosts, artifact checks and checksum generation.
It published nothing. All 14 PR CI checks also passed at that implementation commit.

The subsequent changes contain documentation and these receipts only; the
[source comparison](assessment-evidence/2026-10-14-release/source-equivalence.json)
records that the qualified implementation, dependency and workflow paths are unchanged.
See the [Railway receipt](assessment-evidence/2026-10-14-release/railway/status.json),
[qualification log](assessment-evidence/2026-10-14-release/railway/qualification.log),
[rehearsal receipt](assessment-evidence/2026-10-14-release/rehearsal.json) and
[export checksum](assessment-evidence/2026-10-14-release/export.json).

Publication is pending the final merge and tag. The release PR is
[#61](https://github.com/chester-hill-solutions/stow-s3/pull/61). GitHub's `main`
branch requires one approved review and successful Build, Test, Conformance
and Lint checks; those protections were not changed. npm and PyPI remain deferred.

See [v0.3.0 release notes](releases/v0.3.0.md) for installation and the filesystem
upgrade restriction: legacy records remain readable, while older Stow versions
cannot read newly written version-2 records.
