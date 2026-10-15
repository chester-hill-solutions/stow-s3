# Reopen enforces the combined object-slot quota

Baseline `d022378` on `codex/core-copy-quality`; latest fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`.

Two committed objects and one missing multipart target incorrectly reopened
under `MaxObjects: 2`. The final scan check bounded committed and reserved object
counts separately. The defect reproduces in both bucket orders on memory,
filesystem and workspace stores. Repeated uploads for that target correctly
reserve one object slot; they are not distinct object slots.

The production fix changes one comparison in `internal/runtime/state.go`:
committed object count must fit in `MaxObjects - reservedObjects`. Subtraction
avoids overflowing the combined sum. The existing reservation check remains.
There is no new scan, allocation, lock, interface, format, dependency or API.
Backend COPY and the byte-budget correction are unchanged.

`internal/runtime/quota_reopen_objects_test.go` adds twenty regression cases:

- Three backends × both bucket orders × caps two, three and four. Two objects
  plus one reserved target must refuse only at cap two. Each target has two
  uploads with stored parts, proving reservations are per target.
- Synthetic metadata reporting `MaxInt64 - 1` committed objects, with one or two
  reserved targets. The first fits; the second refuses without signed overflow.

Seven cases failed per baseline run, repeated twice; see
[original failures](raw/before-tests.log). The correction passes all cases,
including five repetitions under the race detector (**100 cases**). A refused
open preserves both existing objects and both uploads/parts. Reopening at an
adequate cap can publish the reserved target and abort the two uploads without
losing the published object or double-releasing its slot.

## Costs and compatibility

The existing `BenchmarkQuotaReconciliation` ran in standalone baseline/candidate
binaries on Apple M1 Pro, macOS 14.5 ARM64, Go 1.25.6. Serial blocks were baseline,
candidate, candidate, baseline; each block collected three 300 ms samples.
No build or test suite ran concurrently with these measurements.

| Measurement | Before | After |
|---|---:|---:|
| Median reconciliation | 1.342 µs | 1.226 µs |
| Timing range | 1.201–2.084 µs | 1.210–1.282 µs |
| Bytes allocated | 2336 B/op | 2336 B/op |
| Allocation count | 25/op | 25/op |
| Packaged WASM | 4,615,863 bytes | 4,615,872 bytes |

The timings overlap; this is no optimization claim. The production cost is one
additional subtraction per reconciliation and nine packaged WASM bytes, with
no measured allocation increase. [All samples and binary hashes](raw/measurements.json)
and [WASM hashes](raw/wasm.json) are retained. COPY's implementation is unchanged;
its existing cost-contract tests are included in the S3 test suite.

Compatibility changes only for an over-cap reopen: it now returns
`ErrQuotaExceeded`, preserving data. Reopen with enough object capacity to remove
objects or abort uploads. Multiple uploads for an existing or repeated target
keep their previous accounting semantics.

## Validation

Passed full Go tests, relevant runtime/storage/S3/run-through/embedded/public-API
race suites, all four conformance profiles, and the twenty-case regression suite
repeated five times under the race detector. Runtime vet, quality/file-size
ratchets, formatting and diff checks also pass. The WASM artifact was rebuilt;
generated-output verification, the WASM smoke test and seven package tests pass.
See [full tests](raw/full-go.log), [race results](raw/race.log),
[conformance](raw/conformance-summary.log), and the remaining validation logs in
`raw`.

The next authorized change addresses the six clean-checkout documentation-link
false positives. The destinations are declared site routes with existing source
files; the checker currently requires their generated website directories.
It should validate route sources without requiring a build and continue to
refuse missing sources and unknown routes. No further storage fix is included.
