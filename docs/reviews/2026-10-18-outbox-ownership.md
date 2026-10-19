# Reject invalid outbox ownership credentials — 2026-10-18

## Defect and consequence

Baseline `2a5941a` checks completion credentials only for equality with the stored
owner and fencing token. An ordinary Enqueue has empty owner and token zero.
MarkClaimedSuccess with those defaults therefore succeeds without a Claim and
removes the queued propagation intent. MarkClaimedFailure and renewal can also
change an unclaimed entry. Owned prepared completion can commit or discard an
ordinary unowned Prepare when given the same empty credentials.

The reproduction uses the existing FileOutbox API and temporary journals. Native
protobuf and supported legacy JSON inputs both reproduce. Matching malformed
pairs (empty owner/nonzero token or nonempty owner/zero token) also pass the old
checks. The adapter's ordinary path passes credentials returned by Claim or
PrepareOwned; this is an internal ownership contract defect, not evidence of an
external authentication bypass or a deployed data-loss incident. No upstream
requests are sent by the focused reproduction.

`origin/main` was fetched and remains `219d1dcfff9807b04660a40313b32127e5be2933`.
[Baseline refs](2026-10-18-outbox-ownership/raw/baseline.json) and the previous
[token-exhaustion review](2026-10-18-outbox-token-exhaustion.md) establish completed
work. This change adds no format, dependency or exported API.

## Reproduction before implementation

The final matrix has **42 distinct failing cases and 72 passing controls**, each
run twice normally and twice under race detection against unchanged production.
Each mode therefore records 84 expected failures and 144 passing controls.
[Red receipt](2026-10-18-outbox-ownership/raw/before-final/red-receipt.json),
[normal output](2026-10-18-outbox-ownership/raw/before-final/regression-normal.log) and
[race output](2026-10-18-outbox-ownership/raw/before-final/regression-race.log) retain case-level results.
Harness success at this stage means the expected test exit 1 was observed.

The failures cover seven ownership-checked methods, two input formats and three
invalid matching credential pairs. Tests verify ErrOutboxClaimLost, a zero result,
unchanged journal bytes, unchanged in-memory state and unchanged reopened state.
Not all failures delete data: Release on an already unowned native record is a
semantic no-op, while success and owned discard can remove an intent. The ordinary
unowned acknowledgement is the concrete lost-intent example.

Controls cover wrong-owner/wrong-token/empty credentials against a valid stored
pair; public Claim/PrepareOwned followed by valid completion with future or
expired deadlines; missing-entry and already-committed retries; and generic
unowned success, failure, commit and discard. Existing recovery, crash, peer-handle,
stale-token and caller-buffer retention tests remain in the full suite.

An earlier original-code run contains the same 42 failures and 64 passing controls
before eight generic-API controls were added. Its
[output](2026-10-18-outbox-ownership/raw/initial-before/regression-normal.log) and tested sources are retained.
A bounded SSH transfer refused an oversized local command before sending it; the
initial job consequently ran the previously uploaded controls. After that job was
confirmed terminal, a smaller test-only update installed the final controls and
updated manifest. Production remained unchanged through both red runs. The first
Railway snapshot upload separately failed before build; its failure and successful
retry are retained. Neither is counted as a product failure or successful test.

## Smallest implementation change and contract limits

One private four-argument predicate requires nonempty owner, nonzero token and
exact equality with both stored fields. Seven existing checks use it: claim
renewal/release/success/failure and prepared renewal/commit/discard. Rejected calls
return the existing ErrOutboxClaimLost before mutation or journal replacement.
No decoding, locking, persistence, ID allocation or token allocation is changed.

The deadline remains a recovery gate rather than automatic token revocation.
An expired but still-current owner can renew or finish. Missing Release and owned
DiscardPrepared remain idempotent. An already-committed CommitPrepared still
returns the active entry before credential validation; it does not mutate its
version. Generic unowned APIs remain available for their existing use cases.

The intentional compatibility cost is refusing matches previously accepted with
an empty owner or zero token. New allocation cannot issue these pairs. A historical
owned prepared record with a zero token can still be read, but cannot finish through
owned completion until its invalid state is corrected. This does not infer an
owner, silently assign a token, migrate malformed ownership or reconstruct intents
already removed. Ownership fields are coordination data supplied by trusted
internal callers, not an authentication boundary.

## Railway verification

All formatting, builds, tests, race/conformance checks, quality checks and
benchmarks ran on the existing Railway scratch runner, as UID 10001. Exact
Railway-formatted Go files were copied back and hash checked. No local Go tools or
commit hooks ran. The final focused matrix records **114 cases passing twice in
each mode: 228 passes normally and 228 with race detection**, with no skipped or
failing cases. [Green receipt](2026-10-18-outbox-ownership/raw/final/green-receipt.json) verifies exact test names
and unchanged regression source bytes across final red and green runs.

The first 16-stage implementation run passes focused normal/race, the full Go
suite, broad race (storage, runtime, embedded, run-through, S3 API and public
package), all four conformance profiles, vet, site build/tests, documentation links,
benchmark build and the paired comparison. Its quality gate correctly rejects a
panic in the new test dispatcher's unknown-action default. That default now returns
an error; no test assertion or production behavior is changed to satisfy the gate.
[The initial checks](2026-10-18-outbox-ownership/raw/after/checks.json) and
[quality output](2026-10-18-outbox-ownership/raw/after/quality.log) retain the failure.

After that job finished, the corrected final regression bytes were run against the
original production again, reproducing the same 42 failures twice in both modes.
The final source then passes formatting, focused normal/race and all unchanged
quality/file-size/format gates. [Final checks](2026-10-18-outbox-ownership/raw/final/checks.json),
[commands](2026-10-18-outbox-ownership/raw/final/plan.json), [environment/source hashes](2026-10-18-outbox-ownership/raw/final/environment.json)
and [export receipts](2026-10-18-outbox-ownership/raw/final/export-receipt.json) are retained. The production
and benchmark sources are byte-identical to the earlier full-suite, conformance
and measurement run. Only the unreachable test dispatcher default changed; those
unrelated passing checks and measurements were not repeated. Documentation is
checked again after this report is written on the same final production source.

## Comparative costs

The existing claim/release and owned prepare/discard benchmark exercises the new
predicate without adding benchmark machinery to the repository. It compares
pinned before/after binaries, state and file paths, depths 0 and 128 per queue,
and GOMAXPROCS 1 and 4. Order is ABBA BAAB, four observations per variant/cell,
400 ms per observation: **128 observations** in total. File fixtures use the
results volume, verified ext4 (type 61267), rather than container overlay storage.

| Path | Per queue | Cycle | CPU | Before → after median µs | Time delta | Before → after allocs/cycle |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| file | 0 | claim | 1 | 7061.775 → 6233.596 | -11.73% | 103.0 → 103.0 |
| file | 0 | claim | 4 | 7105.703 → 6463.204 | -9.04% | 103.0 → 103.0 |
| file | 0 | prepare | 1 | 8200.827 → 6418.727 | -21.73% | 89.0 → 89.0 |
| file | 0 | prepare | 4 | 7707.240 → 6484.891 | -15.86% | 89.0 → 89.0 |
| file | 128 | claim | 1 | 8513.015 → 7082.593 | -16.80% | 6,047.0 → 6,047.0 |
| file | 128 | claim | 4 | 8027.219 → 7460.417 | -7.06% | 6,047.0 → 6,047.0 |
| file | 128 | prepare | 1 | 7573.822 → 6571.080 | -13.24% | 6,036.0 → 6,036.0 |
| file | 128 | prepare | 4 | 7461.283 → 7209.861 | -3.37% | 6,036.0 → 6,036.0 |
| state | 0 | claim | 1 | 0.273 → 0.268 | -1.81% | 0.0 → 0.0 |
| state | 0 | claim | 4 | 0.281 → 0.255 | -9.56% | 0.0 → 0.0 |
| state | 0 | prepare | 1 | 0.238 → 0.226 | -4.92% | 1.0 → 1.0 |
| state | 0 | prepare | 4 | 0.242 → 0.233 | -3.74% | 1.0 → 1.0 |
| state | 128 | claim | 1 | 3.534 → 3.457 | -2.18% | 0.0 → 0.0 |
| state | 128 | claim | 4 | 3.583 → 3.564 | -0.50% | 0.0 → 0.0 |
| state | 128 | prepare | 1 | 2.439 → 2.374 | -2.66% | 1.0 → 1.0 |
| state | 128 | prepare | 4 | 2.216 → 2.012 | -9.23% | 1.0 → 1.0 |

[All observations, ranges, iterations and allocated bytes](2026-10-18-outbox-ownership/raw/after/comparison-analysis.json),
[commands, order and binary hashes](2026-10-18-outbox-ownership/raw/after/comparison-runs.json), and the
[pinned baseline binary](2026-10-18-outbox-ownership/raw/before/benchmark-binary.json) are retained. The new
success-path work is two validity comparisons; the helper creates no error,
allocation, retained cache or buffer. Allocations per cycle are unchanged in all
cells. File IO variability and Go map iteration affect wall time; all adverse
cells are retained. These small samples do not establish statistical significance
or a universal latency improvement, and no performance benefit is claimed.

The runner is stopped after verified exports and final documentation checks. The
separate target remains removed and unchanged. Source, commands, upload receipts,
raw red/green output, all comparison observations and teardown are retained with
an artifact manifest.

Next priority is the still-open journal-write cost: repeat decoding/encoding and
full-state cloning at backlog, with durability syncs dominant at small queues.
The previous faster write-cache prototype remains rejected because it retained
large caller-backed strings. Any new experiment must pass the caller-buffer
retention and peer-replacement guards and show a comparative benefit on Railway.
No cache, indexing, checkpoint or storage abstraction change is implemented here.
