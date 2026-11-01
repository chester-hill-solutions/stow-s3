# Withdraw the generic COPY snapshot fallback — October 15, 2026

The implementation introduced by `959f3b5` is withdrawn. It replaced a backend
COPY with GET→PUT for conditioned copies on stores without native conditional
COPY, causing a severe allocation and latency regression. Those measurements
should have prevented that implementation being accepted.

Production sources and the bundled WASM are restored exactly to `b56ee81`, keeping
the preceding fix that preserves the run-through adapter's source conditions.
The existing benchmark harness is retained, and a deterministic cost regression
test is added. No dependency, public API or format change is introduced.

## Contract boundary

Built-in memory, filesystem and workspace stores implement conditional COPY and
check the version they copy. Those implementations and their coherence tests are
unchanged.

The existing base Store interface supplies coherent unconditional COPY; it does
not supply atomic source conditions. Its documented fallback checks an observed
HEAD, then calls backend COPY. Another writer can change that source between the
two calls. Restoring that path also restores its documented limitation.

**The generic custom-store atomicity fix and its captured-size quota claim are
withdrawn. This change does not claim those races are fixed.** A stronger guarantee
requires a separate capability/contract decision. It cannot be silently emulated
by a substantially more expensive payload-copying fallback.

The snapshot-helper-specific tests added in `959f3b5` are removed with that helper.
Their source and failing probes remain in Git history and the
[withdrawn report](../2026-10-15-copy-source-snapshot/README.md). Existing tests
from `b56ee81` are unchanged, including source preconditions and native coherence.

## Deterministic regression

`TestCopyKeepsBackendCopyWithoutPayloadIO` covers direct HTTP, run-through and
runtime entry points; base and conditional stores; and unconditional, ETag-only
and date-only COPY. A counted store asserts one backend COPY and no GET/PUT payload
emulation. The source is 1 MiB; the test uses call counts rather than timing limits.

Against `959f3b5`, six of the 18 cases fail in each of two runs: conditioned base
store copies issue GET=1, PUT=1 and backend COPY=0. After restoration all 18 cases
pass in each run. See [before](raw/cost-before.txt) and [after](raw/cost-after.txt).
The earlier condition-preservation, date-routing and date/ETag precedence tests
also pass in the same final focused run.

## Fresh comparative measurements

Three versions were measured serially in the order
baseline/regressed/restored/restored/regressed/baseline:

- Baseline: `b56ee81`.
- Regressed: `959f3b5`, reusing the recorded standalone binaries after verifying
  their hashes against the previous report.
- Restored: production sources identical to `b56ee81`, plus the new test and the
  retained measurement harness.

Host: Apple M1 Pro, 16 GiB RAM, macOS 14.5 ARM64, Go 1.25.6. No builds or tests ran
alongside the measurements. Benchmark functions and fixture methods were the same
for all versions. [Metadata](raw/metadata.json), the
[runner](raw/measure.py), [parsed samples](raw/summary.json) and raw outputs are
retained. Test binaries are not committed.

The 4 KiB adapter benchmark has three 300 ms samples per block. Latencies are
medians of six samples per version; allocation counts are constant within each
profile/version.

| Path | Original ns/op | Regressed ns/op | Restored ns/op | Original / regressed / restored B/op | Allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| Base store, conditioned | 1038.25 | 9548.00 | 1337.50 | 688 / 6002 / 688 | 8 / 18 / 8 |
| Base store, unconditional | 872.35 | 782.05 | 869.85 | 464 / 464 / 464 | 5 / 5 / 5 |
| Conditional store, conditioned | 938.35 | 858.15 | 1001.05 | 480 / 480 / 480 | 6 / 6 / 6 |
| Conditional store, unconditional | 782.00 | 837.35 | 773.15 | 464 / 464 / 464 | 5 / 5 / 5 |

Allocation parity with the original is restored. Timing varies even between the
original and restored byte-identical production sources. The small samples do
not establish exact latency parity or a native speedup; all timings are retained,
including the restored conditioned microbenchmark median above the original.

Large-object samples dispatch conditioned COPY through HTTP and the runtime.
Each fresh process runs three timed copies plus calibration. Ranges are the two
block observations per version. Allocation timing excludes fixture setup and GC.

| Workload | Original | Regressed | Restored |
| --- | ---: | ---: | ---: |
| Base memory; 1 MiB, mean latency | 20–33 µs | 1.86–2.18 ms | 32–80 µs |
| Base memory; 1 MiB, allocated bytes/op | 12,314–12,400 | 1,070,322–1,070,458 | 12,314–12,320 |
| Base memory; 64 MiB, mean latency | 34–43 µs | 138–207 ms | 41–45 µs |
| Base memory; 64 MiB, allocated bytes/op | 12,320–12,400 | 275,493,349–275,493,464 | 12,314–12,400 |
| Native memory; 64 MiB, allocated bytes/op | 12,320 | 12,400 | 12,314–12,405 |
| Base filesystem; 64 MiB, mean latency | 277–407 ms | 621–803 ms | 268–270 ms |
| Base filesystem; 64 MiB, allocated bytes/op | 61,928–64,616 | 269,677–272,597 | 61,792–64,666 |

Restored base memory COPY allocates approximately the same bytes for 1 MiB and
64 MiB payloads, retaining shared immutable backend data. It no longer inherits
the whole-object allocation growth of PUT. Native memory COPY retains 65 measured
allocations per operation across all versions.

For the base memory 64 MiB sample, whole-process peak RSS is 140.73–140.88 MiB
original, 485.55–525.39 MiB regressed, and 140.80–140.98 MiB restored. These process
peaks include fixture setup, calibration fixture lifetimes and allocator retention;
they are not isolated single-copy heap deltas or hard process-memory guarantees.

The WASM artifact returns from 4,625,923 to 4,615,854 bytes. Its SHA-256 matches the
original exactly:
`0926e52ac5859884a311322968d9f112bf31927d491cb16a77cf4207160dbc93`.
No tracked production source differs from `b56ee81`; retained code differences
are the test and benchmark harness. Historical evidence remains available.

## Verification

Passed:

- Complete Go suite: [output](raw/go-tests.txt).
- Four conformance profiles: [output](raw/conformance.txt).
- Race tests for HTTP, all storage backends, runtime and run-through:
  [output](raw/race.txt).
- Focused cost, condition-preservation and source-date regressions twice:
  [output](raw/cost-after.txt).
- Vet, formatting, whitespace, Go-quality and file-size checks with unchanged
  baselines.
- Rebuilt WASM byte comparison and real runtime test: [output](raw/wasm.txt).
- Seven embedded/WASM-host/stream/asset tests: [output](raw/node.txt).
- Reproducible generated package output: [output](raw/generated.txt).

Further atomicity for base-only custom stores remains an unresolved contract
question. The preserved tests cover built-in atomic COPY and observed-source
fallback checks; the new test prevents another hidden GET→PUT expansion.
