# Memory write preparation outside the runtime lock

Ordinary memory writes now prepare their owned payload and integrity properties
outside the runtime write lock. Publication still holds the runtime lock and
the memory store lock. This reduces the scope of serialization identified in
the [preceding Railway diagnosis](../2026-10-14-memory-put-diagnosis/README.md).
Throughput has not been remeasured for this change.

`MemoryStore` now implements the existing `StagedWriteStore` contract.
Its staged object owns the copied payload, normalized checksums, ETag,
metadata, and guard. Commit transfers the payload once and evaluates write
conditions against the current object. Abort releases an unpublished payload.
Failed commits consume their staged object; abort is idempotent.

Buffered runtime `PutObject` uses the existing stream entry point.
The runtime reserves quota before staging, then rechecks its store epoch,
lifecycle, cancellation, policy, and quota before commit. Reset invalidates
old preparations, and refused writes release their reservations.
Stores without staging support keep the locked fallback.
Guarded saves and durable request replay retain their existing path.
Request materialization and the owned storage copy remain; this change does
not eliminate either copy or weaken payload authentication.

Two regressions were reproduced before the fix on Railway:

- Slow memory-stream preparation blocked unrelated metadata reads.
- Ordinary buffered writes bypassed staging and its reset fence.

[Expected failures](red.log) and [the tested source identity](red-source.json)
were retained. Green coverage also exercises cancellation, close, reset,
policy revocation, competing replacements, deleted buckets, quota release,
payload/metadata ownership, computed checksums, abort, and single-use commit.
Existing test staging helpers were updated to support buffered writes without
blocking notification delivery or pausing unrelated keys.

The final Railway validation passed:

- `make standards`, including race safety, coverage, quality ratchets,
  generated artifacts, documentation, and release surfaces.
- `make test-conformance` for direct memory/filesystem stores and runtime-backed adapters.
- `make test-node test-wasm` and `make test-browser`.
- Protobuf generated-source consistency and private chart rendering checks.

The browser suite passed 33 tests with 2 expected skips and no failures.
Chart verification covered 13 control combinations and 6 desktop/mobile previews.
Public API reference output was unchanged. The comment-line ratchet fell from
4,111 to 4,101; other quality counts and identities were unchanged.

The rebuilt SDK WASM is **6,497,695 bytes**, up **7,543 bytes (about 0.12%)**
from 6,490,152 bytes. SHA-256:
`5d7ac26a55e9bb828d09876ed8af74cf3a2e0f8fed012f379efb4ccbf7b5b52a`.
The exact checked artifact was copied into the tracked SDK distribution.

Final validation deployment: `244bd6e0-d02c-4f2e-b0d9-422cfe89a0cd`.
Source payload SHA-256: `6889966e62281fa26545d48e9e5985523e1214c258e097ea9a9068091247e7dd`.
Validation completed at 2026-10-14 12:04:32.504 UTC.
The deployment was observed `SUCCESS`, then `REMOVED` after verified export.
Both benchmark services are stopped; volumes and retained evidence remain.

- [Final receipt](verification/receipt.json), [standards log](verification/standards.log), and [export verification](export.json).
- [Generated artifact identities](verification/generated-change.json), [quality report](verification/go-quality-report.json), and [browser report](verification/sdk-browser-report.json).
- Exact verification output, including WASM, remains on the Railway results volume
  at `/results/memory-write-staging-244bd6e0-d02c-4f2e-b0d9-422cfe89a0cd/verification`.

Initial validation found an abort-test cleanup error, recorded in
[the initial log](initial-validation.log). The test was corrected to avoid deleting
an object never published. Two superseded builds were cancelled while test-helper
updates were completed. Final validation above is the completed result.

No build, test, or workload ran locally. Local operations edited source,
formatted Go, packed uploads, and verified/copied downloaded artifacts.
The website dataset and publication state were unchanged.
