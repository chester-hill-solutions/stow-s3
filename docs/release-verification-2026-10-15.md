# v0.3.2 local release checkpoint — 2026-10-15

The patch is proposed in [PR #77](https://github.com/chester-hill-solutions/stow-s3/pull/77)
on `codex/patch-0.3.2`. No release has been published.
Distribution remains native GitHub downloads for Linux/macOS on x86_64 and ARM64;
npm and PyPI publication are deferred by `scripts/release-policy.json`.

## Scope

The release fixes concurrent stream cleanup, host-edited multipart identity,
preparation/checkpoint admission, Git metadata admission before checkout and
multipart namespace capacity accounting. The namespace fix is already present
in source baseline `29897c8`; the additional runtime changes are in `8e8df36`.
Later commits add qualification evidence and regenerate API references.

The [release notes](releases/v0.3.2.md) explain the one-time re-derivation of
older multipart entries and the possibility of changed ETags/versions or reset
object metadata. Git admission remains distinct from an OS-enforced disk quota.

## Performance evidence

The [benchmark report](benchmarks/2026-10-15-patch-validation/report.md) preserves
raw results and the failed baseline parallel-workspace measurement. Native
startup block medians for the pre-review candidate remain around 16.7 ms and its WASM is byte-identical at
4,615,854 bytes. Browser worker startup p50 rises by 3.4 ms; checkpoint peak RSS
rises by about 0.3 MiB in the tested workload. All candidate browser trials release
their 16 read pins, while the baseline leaves 15. Timing noise and measurement
limits are reported without asserting zero overhead or production capacity.
The review fixes below increase WASM to 4,617,652 bytes, a 1,798-byte (0.04%)
increase. The earlier timing measurements do not qualify this updated artifact.

## Local verification

- The complete Go suite, static checks and complete race suite pass.
- Conformance passes for memory, filesystem, runtime/memory and runtime/filesystem.
- TypeScript: 161 tests pass; Python: 76 tests pass.
- Chromium: 35 tests pass, with two optional checks skipped.
- WASM/agent integration: 28 tests pass, with one optional real-provider check skipped.
- Required standards gates pass. The first `make standards` stopped at a stale
  API reference after earlier gates passed. After regenerating the Go/Python
  references, `make check-api-reference check-scripts check-ts-quality
  check-generated check-mcp-browser` passes. Script tests report 185 passes;
  MCP browser tests report seven passes. No quality baseline was raised.
- Go coverage is 70.25% (11,921/16,969 statements), above the recorded floor.
- Local documentation links, release version/plan and generated output checks pass.
- Four native targets cross-compile, archives and checksums are generated and
  verified, and the native macOS ARM64 binary reports `stow-s3 0.3.2`.
- The main npm package and four platform packages pack locally. A clean npm
  consumer on macOS ARM64 starts its bundled binary and exercises a persistent
  workspace without a `STOW_BIN` override.

The [local build manifest](assessment-evidence/2026-10-15-patch/local-builds.json)
records archive hashes and build revision. These are staged rehearsal artifacts;
the hosted release must rebuild and qualify its exact final revision. Foreign
platform execution and hosted wheel/packed-consumer qualification are recorded
separately below.

## Review fixes

- Retain read-session ownership across Go/WASM until persistent pin release
  succeeds; expose failures and allow an explicit retry.
- Derive live and snapshot metadata through one byte-identity policy without
  making snapshot reads modify the workspace index.
- Use the pending-call map as the worker queue owner. Remove cancelled unsent
  controls immediately while retaining dispatched-call admission until reply.
- Local verification after these fixes: complete Go suite, race tests for runtime,
  workspace storage and workspace facade, 163 Node tests, 35 Chromium tests
  (two configured skips), standalone WASM integration, TypeScript standards,
  Go vet/quality/size gates, API references, documentation links, and reproducible
  generated package output pass. Coverage is 70.23% (11,911/16,959 statements),
  above the recorded floor. No quality baseline was relaxed.

## Publication gates still pending

[PR CI](https://github.com/chester-hill-solutions/stow-s3/actions/runs/38769771240)
and the complete [hosted release rehearsal](https://github.com/chester-hill-solutions/stow-s3/actions/runs/38769813976)
passed at `6ada2c2116f893a9ddbbb8c39fda759bf7a33eec`, before the review fixes.
Those receipts do not qualify the updated revision; it needs fresh hosted CI and
release qualification. The live-provider gate is intentionally skipped under
the downloads-only policy. GitHub `main` still requires one independent approved
review and successful Build, Test, Conformance and Lint checks. Merge, tagging and
publication must follow successful qualification and required review.
