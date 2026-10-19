# v0.3.2 release preparation — 2026-10-18

Distribution remains native GitHub downloads for Linux and macOS on amd64 and
arm64. npm and PyPI publication remain deferred by the unchanged
[release policy](../scripts/release-policy.json). The
[release notes](releases/v0.3.2.md) disclose the outbox protobuf migration and four
confirmed correctness issues that remain, including workspace create-only
overwrite on an unreadable destination.

## Railway preparation

The authorized scratch deployment `22d4d547-8587-4784-9c32-f29684ee5c71` reproduced
the existing documentation inventory's `ENOBUFS` failure before the fix. A real
Git-index fixture over 1 MiB fails with the old buffer (six controls pass, one
regression fails), then all seven tests pass with the bounded 16 MiB capture.
[Original](assessment-evidence/2026-10-18-release/railway/initial/doc-inventory-baseline.log),
[regression before](assessment-evidence/2026-10-18-release/railway/final/large-inventory-red.log),
[after](assessment-evidence/2026-10-18-release/railway/final/large-inventory-green.log).

All nine final expected-outcome steps pass: red/green inventory regression,
repository links, native build and formatting, version/install/site claims,
command references, generated-output verification, release plan and source receipt.
These preparation checks do not substitute for full release CI or native consumer
checks. [Checks](assessment-evidence/2026-10-18-release/railway/final/checks.json),
[generated output](assessment-evidence/2026-10-18-release/railway/final/generated.log).

At artifact retrieval, 14,367 uploaded candidate files match the final Railway source receipt,
including all Go/module/package inputs and the regenerated WASM. The three
excluded differences are explicit scratch-only runner harness replacements;
none is copied into the release. New preparation reports, receipts and the README checkpoint link are added
after that comparison and are checked by release CI. The scratch runner is
[verified removed](assessment-evidence/2026-10-18-release/runner-stop-status.json);
the target service remains unchanged and stopped.
[Comparison](assessment-evidence/2026-10-18-release/source-equivalence.json),
[full source hashes](assessment-evidence/2026-10-18-release/railway/final/qualified-source.json),
[artifact export](assessment-evidence/2026-10-18-release/generated-receipt.json),
[evidence hashes](assessment-evidence/2026-10-18-release/SHA256.json).

## Publication gates

The existing release workflow requires full tests, race/conformance, standards,
generated output, four native builds and wheels, four packed consumers, and archive
checksums before publication. A workflow-dispatch rehearsal publishes nothing.
The user authorized these required GitHub checks; no local builds or tests run,
and benchmarks remain Railway-only. Main's required approved review and status
checks remain in force. This release makes no executed-live-provider claim.

The prior implementation-specific Railway regression and performance evidence
remains under `docs/reviews/`; see the bounded
[correctness register](reviews/2026-10-18-correctness-closure.md). Qualification
limits there continue to apply.
