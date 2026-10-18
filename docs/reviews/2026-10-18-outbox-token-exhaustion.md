# Refuse exhausted outbox fencing tokens — 2026-10-18

## Defect and operational consequence

Baseline `cb8dc49` already refuses ID-sequence exhaustion, but its shared fencing
allocator incremented uint64 without checking the limit. Claim and PrepareOwned
could therefore issue token zero after the maximum value. Reload raises the
counter from stored lease fields; a retained maximum-valued token can repeatedly
restore the counter to the limit and cause repeated zero tokens.

The deterministic reproduction uses a valid temporary journal with a terminal
anchor carrying the maximum token and an ordinary target intent. Claim target
with owner `same-worker`, release it, reopen with a peer handle, and claim it again
with the same owner. Both leases receive zero. A completion from the first lease
then matches the second lease's owner/token and removes the current intent.
Both native protobuf and supported legacy JSON reproduce this loss of fencing.
The test sends no upstream requests; no deployed incident or object-data corruption
is asserted. Existing formats can represent the maximum value, so this check is
not a test that performs 2^64 acquisitions.

`origin/main` was fetched and remains `219d1dcfff9807b04660a40313b32127e5be2933`.
[Exact baseline refs](2026-10-18-outbox-token-exhaustion/raw/baseline.json) and
[stale-acknowledgement output](2026-10-18-outbox-token-exhaustion/raw/before/regression-normal.log) are retained.
The rejected write-cache optimization remains rejected; its ownership/resource
regressions remain in the full suite.

## Test-first evidence

Before implementation changes, tests run twice normally and twice under race
detection against the original allocator. **32 distinct cases fail twice per
mode: 64 failures normally and 64 under race. Thirty controls pass twice per mode.**
[The red receipt](2026-10-18-outbox-token-exhaustion/raw/before/red-receipt.json) verifies names and repetitions.
Each mode records four accepted stale acknowledgements and four removed current
intents: two formats, twice each. Harness success means the expected test exit 1
was observed, not that the regression tests passed.

The matrix covers native/JSON inputs; the persisted counter and maxima recovered
from active claim, active prepared-token and prepared records; Claim/PrepareOwned;
the last legal token; concurrent last-token contenders using peer handles; and
shared-state atomicity. Counter and map/lease snapshots, journal bytes and reopen
must survive refused allocation unchanged. Pure-state cases also expose ID/token
consumption that filesystem transaction cloning could otherwise hide.

Controls keep invalid-owner/lease and unresolved-key errors, ineligible claims,
ID-allocation failure atomicity, and existing maximum-token owner renewal/release/
completion usable. The first run also failed one over-strict JSON/missing-entry
control: a successful no-op claim is allowed to migrate JSON to native format.
That fixture now permits migration while checking unchanged semantic state and
counters. [Initial output](2026-10-18-outbox-token-exhaustion/raw/initial-before/regression-normal.log) and
[initial test source](2026-10-18-outbox-token-exhaustion/raw/initial-before/source-outbox_token_exhaustion_controls_test.go.txt)
remain retained. No implementation was changed to satisfy that control, and no
quality baseline was raised.

## Focused change and preserved contracts

Replace the mutating increment helper with a private checked calculation of the
next fencing token. At the limit it returns an error through existing returns.
Claim checks availability only after existing eligibility/held-lease checks, then
updates the counter when granting ownership. PrepareOwned retains invalid-lease
and unresolved-key precedence, checks capacity before assigning an ID, and commits
the counter with the new prepared entry. A failed ID assignment does not consume
a computed token. No new sentinel, exported API, dependency, format, index, lock,
checkpoint or storage abstraction is added.

The last representable token remains valid once. Subsequent ownership allocations
fail closed rather than wrap. They return empty results without changing the
original journal or state. Existing owners do not need a new token to renew or
finish; these operations remain usable at exhaustion. The existing mutex and
shared filesystem lock serialize checking and committing across peer handles.
Claim/prepared fields, Attempted reconciliation, per-key ordering, ID semantics,
legacy migration, authoritative reads and durable publication remain intact.

Compatibility cost: an exhausted journal can still open and existing work can
finish, but new ownership cannot be allocated. There is no automatic reset that
could reuse a stale token. Already removed intents cannot be reconstructed, and
a previously wrapped counter without a retained maximum marker cannot be inferred
from the current snapshot. This is a deliberate refusal of previously unsafe
behavior, not a new capacity-management API. Existing lease/key sentinels retain
precedence. At the simultaneous token-limit/invalid-ID boundary, a generic token
exhaustion error may precede the generic ID error; no error-string API is promised.

## Validation

All **62 cases pass twice normally and twice with race detection**, 124 passes per
focused mode. Regression and benchmark source bytes match the corrected original
red run. [Green receipt and hashes](2026-10-18-outbox-token-exhaustion/raw/after/green-receipt.json) verify this.
All **16 final Railway stages pass**: formatting; focused normal/race twice; full Go;
broad storage/runtime/embedded/run-through/S3/public-package race tests; four
conformance profiles (memory, filesystem, runtime-memory, runtime-filesystem); vet;
site build/tests; documentation links; quality/file-size/format gates; benchmark
build and interleaved comparison. [Exact commands/results](2026-10-18-outbox-token-exhaustion/raw/after/checks.json)
retain every stage.

All Go formatting, builds, tests, race/conformance, code/documentation checks and
benchmarks run on the authorized Railway scratch deployment. Go 1.25.6, Node
25.9.0, 4 CPU/4 GiB cgroup, UID 10001. Benchmark TMPDIR is under ext4 `/results`,
verified before timing. [Filesystem receipt](2026-10-18-outbox-token-exhaustion/raw/after/fixture-filesystem.json)
and environment records retain actual paths/limits. Local work is reads, editing,
Git, packaging, hashing and exported-data analysis. Exact Railway-formatted source
is copied back rather than formatted or tested locally.

## Comparative measurements

The same benchmark source runs original/fixed binaries in ABBA BAAB order, eight
processes, GOMAXPROCS 1/4, 400-ms minimum per case: four observations per variant/cell,
**128 observations** total. Baseline binary/source hashes are pinned before uploading
the implementation. Backend `state` isolates the existing private state transitions;
`file` includes public calls, full reload/clone/encoding and durable file/directory
syncs. This is not a comparison of backend service guarantees.

Each operation is Claim+Release or PrepareOwned+DiscardPreparedOwned: two calls.
Depth is retained entries in each queue; depth 128 means 256 retained intents.
Claim adds one fixed target and primes its Attempted marker outside timing. Seeding,
verification and target cleanup are untimed. These are serial synthetic cycles,
not end-to-end S3 throughput or latency guarantees. Allocation bytes/counts are
cumulative per cycle, not retained heap/RSS. The token decimal width and iteration
counts can differ between observations. Samples remain small; no significance or
universal speedup is claimed. Every adverse cell is retained.

| Backend | Depth per queue | Cycle | CPU | Before → after µs | Change | Allocs before → after |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| file | 0 | claim | 1 | 7,880.732 → 6,787.309 | -13.87% | 103.0 → 103.0 |
| file | 0 | claim | 4 | 7,114.851 → 7,497.210 | +5.37% | 103.0 → 103.0 |
| file | 0 | prepare | 1 | 7,366.208 → 6,851.364 | -6.99% | 89.0 → 89.0 |
| file | 0 | prepare | 4 | 7,803.712 → 8,761.529 | +12.27% | 89.0 → 89.0 |
| file | 128 | claim | 1 | 9,138.730 → 8,782.334 | -3.90% | 6,047.0 → 6,047.0 |
| file | 128 | claim | 4 | 7,880.667 → 8,435.274 | +7.04% | 6,047.0 → 6,047.0 |
| file | 128 | prepare | 1 | 7,546.008 → 7,930.392 | +5.09% | 6,036.0 → 6,036.0 |
| file | 128 | prepare | 4 | 9,139.799 → 7,685.824 | -15.91% | 6,036.0 → 6,036.0 |
| state | 0 | claim | 1 | 0.270 → 0.273 | +1.07% | 0.0 → 0.0 |
| state | 0 | claim | 4 | 0.269 → 0.274 | +1.75% | 0.0 → 0.0 |
| state | 0 | prepare | 1 | 0.244 → 0.250 | +2.36% | 1.0 → 1.0 |
| state | 0 | prepare | 4 | 0.254 → 0.256 | +1.02% | 1.0 → 1.0 |
| state | 128 | claim | 1 | 3.946 → 3.783 | -4.14% | 0.0 → 0.0 |
| state | 128 | claim | 4 | 3.930 → 3.939 | +0.24% | 0.0 → 0.0 |
| state | 128 | prepare | 1 | 2.668 → 2.628 | -1.48% | 1.0 → 1.0 |
| state | 128 | prepare | 4 | 2.285 → 2.183 | -4.44% | 1.0 → 1.0 |

[All observations, ranges, iterations and allocated bytes](2026-10-18-outbox-token-exhaustion/raw/after/comparison-analysis.json),
[commands/order and binary hashes](2026-10-18-outbox-token-exhaustion/raw/after/comparison-runs.json), and the
[pinned baseline binary receipt](2026-10-18-outbox-token-exhaustion/raw/before/benchmark-binary.json) are retained.
The new success-path work is a boundary comparison and checked result handling;
the helper allocates an error only on exhaustion. No new retained cache or buffer
is introduced. Provider IO variability and map iteration can dominate wall time;
adverse values are not classified away or omitted.

The final report and outbox documentation are checked on the same source with the
site build/tests and documentation-link checker. The runner is stopped after verified
exports, and the separate target service remains removed and unchanged. Source,
commands, upload/hash receipts, the first control failure, all red/green output and
teardown are retained with the artifact manifest.

Next correctness investigation: completion APIs compare owner/token fields but may
accept empty-owner/zero-token credentials on an unclaimed record. Reproduce this
through the existing API before changing it. No next fix is implemented here.
The measured journal-write performance work remains open; any reuse experiment
must pass the caller-buffer retention guard before adoption.
