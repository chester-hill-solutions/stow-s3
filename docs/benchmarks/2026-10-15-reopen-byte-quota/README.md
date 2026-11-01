# Reopen enforces the combined byte quota

Follow-up: the object-count finding at the end of this report is corrected in
[the next independent change](../2026-10-15-reopen-object-quota/README.md).

Baseline: `4ed7863` on `codex/core-copy-quality`, based on latest fetched
`origin/main` `219d1dcfff9807b04660a40313b32127e5be2933`.

## Defect and correction

A store containing a six-byte object and a six-byte multipart part reopened under
an eight-byte cap. Reconciliation checked committed bytes and reserved part bytes
separately against `MaxBytes`, although normal admission counts their sum.

The production correction changes one comparison in
`internal/runtime/state.go`: committed bytes must fit in
`MaxBytes - reservedBytes`. The existing part-scan guards already bound the
nonnegative reservation by `MaxBytes`; subtraction avoids overflowing a sum.
No new allocation, scan, lock, dependency, interface, format or API is introduced.
COPY continues to use the existing backend operation.

`internal/runtime/quota_reopen_test.go` adds 14 cases:

- Memory, filesystem and workspace, each with twelve total bytes at caps eight,
  eleven, twelve and thirteen. Only the first two caps must refuse.
- An advertised object of `MaxInt64 - 1` bytes plus a part of one or two bytes.
  The first fits exactly; the second must refuse without signed overflow. These
  synthetic metadata cases do not allocate enormous payloads.

Before the correction, seven cases failed per run, reproduced twice. Afterward,
all cases pass. Refusal preserves the object and upload. Reopening the same store
at an adequate cap retains both the object bytes and part identity, still charges
parts against new writes, and can complete the original multipart payload after
freeing the old object's space. See [before failures](raw/before-tests.log) and
[repeated race results](raw/after-tests.log).

## Comparative measurements

Apple M1 Pro, macOS 14.5 ARM64, Go 1.25.6. Standalone baseline/candidate test
binaries use the same benchmark code. Runs were serial, ordered baseline,
candidate, candidate, baseline, with three samples per benchmark per block
(**six samples per version**). No build or validation suite ran concurrently.
Binary hashes and host details are in [metadata](raw/metadata.json); all numerical
samples, ranges and medians are in [measurements](raw/measurements.json).

| Benchmark | Before median | After median | Allocations before → after |
|---|---:|---:|---|
| Reconcile one object + one upload | 1.363 µs | 1.337 µs | 2336 → 2336 B; 25 → 25 allocs |
| Conditional COPY, native memory, 64 MiB | 7.318 µs | 7.529 µs | 8409–8410 → 8409–8411 B; 61 → 61 allocs |
| Conditional COPY, base-only memory, 64 MiB | 7.368 µs | 7.490 µs | 8409–8410 → 8409–8410 B; 61 → 61 allocs |
| Conditional COPY, native filesystem, 64 MiB | 205.3 ms | 213.1 ms | 61800–64669 → 61858–67362 B; 226–229 → 227–228 allocs |

Reconciliation uses `BenchmarkQuotaReconciliation`, 300 ms per sample. Memory
COPY uses existing `BenchmarkConditionalCopy` at 200 ms per sample; filesystem
COPY uses three timed copies per sample. Existing HTTP request logging remains
enabled in both versions. The `*.bench.txt` files in `raw` retain every benchmark
result with iteration counts; repetitive request logs were omitted from these
transcripts. Filesystem sync timings and logging allocations vary substantially.
For example, filesystem medians differ by 3.8%, with overlapping ranges of
157.9–302.2 ms before and 189.3–255.9 ms after. Native memory also has an after
outlier of 25.35 µs. These are noisy measurements, not an exact-latency parity
claim or an optimization claim. There is no observed material regression and no
payload-sized allocation was introduced.

The only additional runtime work is one subtraction per reconciliation. The
packaged WASM was rebuilt and grows from 4,615,854 to 4,615,863 bytes (**+9**);
[hashes](raw/wasm.json) identify both artifacts. The compatibility change is
intentional: a reopen whose combined data exceeds the requested cap now returns
`ErrQuotaExceeded`. Reopen with sufficient capacity to recover or remove data;
the refused open does not prune objects or uploads.

## Validation

- Full Go suite and race suites for runtime, storage, S3, run-through, embedded
  API and native public API.
- New 14-case regression suite repeated ten times under the race detector.
- All four conformance profiles: memory, filesystem, runtime/memory and
  runtime/filesystem.
- Runtime vet, Go quality and file-size ratchets, formatting and diff checks.
- Rebuilt WASM smoke test, seven packaged runtime tests, and generated artifact
  verification.

The [raw evidence](raw/full-go.log), [race output](raw/race.log),
[conformance summary](raw/conformance-summary.log) and packaged-runtime logs in
`raw` record these checks. The existing full doc-link check still reports six
missing built website targets in unchanged `site/agent.md`; new report links pass.

## Next finding — object-count admission across buckets

A separate native filesystem reproduction creates an upload for a missing target
in bucket `aaa` and two committed objects in bucket `zzz`, within an original
three-object cap. Reopening at `MaxObjects: 2` incorrectly succeeds: scanning the
first bucket reserves one slot before the later bucket adds two objects, and the
final check compares the two subtotals separately. The current byte correction
does not affect this object-count check.

This was reproduced twice with the corrected `state.go` in the temporary checkout.
The [probe source](raw/next-object-quota-probe.go.txt),
[failure output](raw/next-object-quota-probe.log), and
[source hashes](raw/next-probe-source.json) are retained. It is an admission-contract
violation; no data loss was demonstrated. No object-count fix is included here.
The next change should enforce the combined object-slot budget, test both bucket
orders and repeated targets, and preserve uploads when an open is refused.
