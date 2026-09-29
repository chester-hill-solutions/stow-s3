# Portable working storage: implementation evidence

**Date:** 2026-09-29. **Candidate:** unpublished 0.3.0 development tree based on
`5485a3640dbeeb368324d7e2a223bc17d7d23fc5` (see current Git history for exact base).
Local runtime evidence is macOS arm64; Linux amd64 also compiles, without a Linux runtime claim. This report supplements the
[canonical plan](plan.md); it does not certify a release or product fit.

## Implemented slices

| Discovery slice | Implementation and local evidence |
| --- | --- |
| P01 / S0-6 | Split oversized coverage tests without removing cases or widening quality allowances. |
| P02 / S0-1 | Relative handoff archive references; moved unedited bundle round trips with sender data removed. Typed bundle export/adopt reuse the archive implementation. |
| P03–P05 / S0-7, S3-2 | Shared capture/cleanup locks; read-only registry lookup; bounded cancellation; synchronized publication; durable request receipts and resolve/replay; injected failure and lost-reply regressions. |
| P06–P07 / S0-1 | Normalized metadata at HTTP/storage/SDK boundaries; independent CRC64NVME vectors and actual default AWS CLI upload. CRC64NVME multipart is explicitly unsupported. |
| P08 / S0-5 | Configurable body and concurrent-handler limits through CLI/readiness/wrappers; default 16 handlers and 1,024 outstanding multipart uploads; HTTPS upstream policy, explicit local HTTP support and redirect-origin confinement. |
| P09 / S1-1 | Borrowed same-runtime facade; drain-before-release lifetime; native/TypeScript/Python serving; seeded/host-write usage refresh and shared workspace conformance. |
| P10–P11 / S1-2, S1-3 | Bounded v2 logical snapshot, metadata-aware diff, archive/import/restore, empty buckets and escaped keys; Git provenance and explicit local-source reconstruction; file v1 remains supported. |
| P12 / S1-4 | Versioned registry policy, serialized publication admission, corrupt-accounting refusal and descendant-protected deletion. Go API only for policy/deletion. |
| P13 / S2 | Aligned 0.3.0 packages; npmjs target; exact artifact identity on retry; preflight/consumer order; separate revision-bound provider receipts; packed native npm/wheel consumers on macOS arm64. No publication performed. |
| P14–P15 / S3 | Official MCP SDK 1.8.0 / Go 1.25.6; real stdio negotiation for 2025-11-25 and 2026-07-28; scoped typed tools, paging and limits; pinned OpenCode caller with persisted admission/save state and bounded retry. |
| P16 / S4 | Native deterministic continuity: lose capture reply, resolve original checkpoint, relocate bundle unchanged, remove sender data, adopt in a fresh registry, continue fixture. Same host, no model. |

Additional S0 regressions cover explicit upstream scope after local bucket creation,
root identity/ancestor-symlink replacement on reads and capture, special input
refusal, and expanded shared SDK protocol cases. Case mappings live in the
[S3 compatibility contract](compat-contract.md).

## Agent integration evidence

Installed OpenCode **2.0.16**, corresponding source commit
`3a103fe0aff726a4edc7492f03f7b88195d9e4c9`, was exercised through admission, wait,
terminal-result inspection and known-file-tool quiescence. An isolated nonexistent
provider produced a real failed terminal outcome; the full owned-process caller
saved it and stopped its process. This proves the failure/save path, not successful
model execution. No provider credentials were inspected and no model call was made.

Controller regressions cover no next prompt before save, permanent/transient save
failures, bounded retry/backoff, ambiguous reply resolution, restart without prompt
replay, caller-state containment and cleanup ordering. Recovery resolves storage
receipts without starting OpenCode. If agent shutdown cannot be confirmed, claims
remain held instead of pretending cleanup succeeded. Force-killing the caller can
still leave a process and stale lock; explicit host cleanup is required.

MCP uses storage APIs directly, not a second checkpoint store. Configured registry,
workspace and transfer roots bound its API authority. Receiver team values cannot
widen registry scope, and adoption consumes the exact validated document/archive
handles. Post-publication errors report unknown outcome, and partial imports remain
visible with an error. Host-controlled destination roots remain a requirement.

## Validation record

Focused tests completed throughout implementation include Go race tests for capture,
publication, portable state, handoff, registry policy, runtime/facade, rooted reads,
transport and MCP; TypeScript/Python serving contracts; actual native stdio MCP;
OpenCode caller tests and deterministic native continuity. A real default AWS CLI
single-object CRC64NVME upload passed with isolated configuration and local target.

Final aggregate **`make test-all` passed**, including Go/unit/race, four local
conformance profiles, 92 Node tests, 74 Python tests, the WASM harness and 19 caller
tests. **`make standards` passed** after the final bounded-copy change: formatting,
vet, race, quality/size/coverage ratchets, version/install/command/ADR checks, script
tests, TypeScript quality and generated-artifact reproducibility. No quality
allowance was widened. `git diff --check` also passed.

Earlier attempts caught a newly added Node corpus assertion bug and overlapped an
unfinished source edit; both were superseded by the successful final runs. The
rejected hardlink optimization is absent from the verified candidate. Live-provider
tests requiring credentials remain skipped; local passes do not close that gate.
The [validation receipt](assessment-evidence/2026-09-29/implementation/validation.json)
records log hashes and selected source hashes for this uncommitted candidate.

## Cost measurements

The repeatable harness is `scripts/benchmark-portable.mjs`. It measures native
process launch plus portable capture, hashes and durable publication, excluding
model/network time. Random file payloads are retained in full; checkpoint retention
is logical payload accounting, not a deduplicated physical-storage promise.

Initial measurement under concurrent test load:

| Fixture | Repetitions | Median capture | Retained file bytes |
| --- | --- | --- | --- |
| 256 files / 8 MiB | 5 | 450 ms | 42,526,894 |
| 4,096 files / 64 MiB | 3 | 30,996 ms | 206,946,350 |

The larger result was rejected as unsuitable for a turn barrier. Phase profiling
found repeated directory enumeration consumed 30.77 of 33.33 seconds. Reusing the
already enumerated exact paths reduced the logical scan from 14.77 seconds to
208 ms and verification from 16.00 seconds to 201 ms. Checks for case, identity,
content changes and corruption remain in place.

Parallel file flushes alone did not materially improve the original result
(31,483 ms median); they remain bounded to 16 and preserve the file/directory/
publication barriers. A parent-hardlink experiment reduced repeated storage but
made capture slower on this host, so it was removed. The rejected profile is
retained as evidence, not a supported feature. Final measurements, after test-generated disk load stopped:

| Final fixture | Repetitions | Median | Observed range |
| --- | --- | --- | --- |
| 256 files / 8 MiB | 5 | 129 ms | 124–185 ms |
| 4,096 files / 64 MiB | 5 | 1,738 ms | 1,530–11,098 ms |

Bounded parallel copying (eight files) improved the later serial-copy 2,825 ms
median on the large fixture. The 11,098 ms first-capture outlier remains visible;
these timings are not a worst-case guarantee. Workloads were measured sequentially,
not under a controlled hardware laboratory. Five retained large captures reference
344,910,614 bytes, with no sharing or compression guarantee.

Raw measurements and the rejected sharing experiment are in
[the implementation receipts](assessment-evidence/2026-09-29/implementation/).
Durability acknowledgement remains intact; a larger timeout is not a performance fix.

## Open gates and limits

- Successful real-model work, real interruption/cleanup and continuation on a second
  host have not been verified. The OpenCode example remains experimental.
- Public npm/PyPI account setup, actual publication, clean published consumers and
  fresh AWS/R2/custom-provider receipts are external release gates.
- Rooted read/capture confinement does not make arbitrary host writes safe or isolate
  a hostile process. This product is storage, not an OS sandbox.
- Full-copy checkpoints, transfer throughput, maximum object size, peak RSS and
  concurrent capture cost need workload-specific evidence. Quotas are not memory
  ceilings. Enhanced deltas and self-contained Git base bundles remain deferred.
- Two independent teams' repeated use remains the product acceptance gate.

See [usage](portable-workspace-usage.md) and the
[OpenCode example](../examples/opencode/README.md) for runnable entry points.
