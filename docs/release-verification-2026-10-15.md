# v0.3.2 local release checkpoint — 2026-10-15

The patch is prepared locally on `codex/patch-0.3.2`. Publication has not started.
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
startup block medians remain around 16.7 ms and WASM is byte-identical at
4,615,854 bytes. Browser worker startup p50 rises by 3.4 ms; checkpoint peak RSS
rises by about 0.3 MiB in the tested workload. All candidate browser trials release
their 16 read pins, while the baseline leaves 15. Timing noise and measurement
limits are reported without asserting zero overhead or production capacity.

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
platform execution and hosted wheel/packed-consumer qualification have not run.

## Publication gates still pending

The branch has not been uploaded and there is no v0.3.2 release or hosted release
rehearsal yet. The configured public destination is
`github.com/chester-hill-solutions/stow-s3`. GitHub `main` requires one independent
approved review and successful Build, Test, Conformance and Lint checks. Those
protections remain in place. After authorization to upload the prepared branch,
open the release PR and run the existing release rehearsal with version `0.3.2`.
Merge/tag/publication must follow successful qualification and required review.
