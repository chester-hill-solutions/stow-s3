# v0.3.2 patch qualification

The measured patch preserves the small-runtime characteristics tested here and
fixes concurrent reader cleanup. This is a local comparison on one Apple M1 Pro,
not a production capacity result. Small latency differences and noisy filesystem
measurements do not establish either a speedup or a general performance guarantee.

## Source and method

- Baseline: `29897c891d39766d856fd3ff9b0e4e80036eb8e3`, exported without working changes.
- Candidate runtime source: `8e8df36`. The run recorded local snapshot `017a377`,
  which adds a measurement harness correction only. That unpublished evidence
  commit was amended to redact local paths before publication; runtime sources
  and measured artifact hashes are unchanged.
- Native builds: Go 1.25.6, `-buildvcs=false -trimpath -ldflags '-s -w'` for both.
- Host: macOS Darwin 23.5.0, ARM64, Apple M1 Pro, 8 logical CPUs, 16 GiB memory;
  Node 25.9.0. See [complete metadata](results/metadata.json) for artifact hashes,
  Chromium version, timestamps and harness identities.
- Native comparisons ran serially in baseline/candidate/candidate/baseline order.
  Each block contains 20 fresh native sessions and three samples per concurrency
  point. OS caches were warm. No build or test jobs ran alongside measurements.
- Persistent browser streams used fresh contexts and workers in a warmed Chromium
  process. Ten trials per version alternated order after excluded warm-ups.
  Each trial wrote/read 4 MiB and checked SHA-256 after read timing.
- Portable capture used 256 files totaling 8 MiB, five captures per block, with
  no modified files between captures. Native workspace RSS used the existing
  memory-only lifecycle harness, including its 500-file, 15.625 MiB workload.

## Results

Native timing/throughput ranges below are the two block medians, not a pooled
percentile. Browser p50 values use the nearest-rank percentile over ten raw trials.

| Measurement | Baseline | Candidate |
| --- | ---: | ---: |
| Fresh native process ready, block p50 | 16.71–16.73 ms | 16.73–16.79 ms |
| Loaded native session RSS, block p50 | 14.00–14.05 MiB | 13.88–13.89 MiB |
| 16-connection upload throughput, block p50 | 460.06–530.91 MiB/s | 482.17–502.43 MiB/s |
| 16-connection download throughput, block p50 | 846.44–952.01 MiB/s | 928.81–971.48 MiB/s |
| Portable capture, block median | 112.89–279.61 ms | 112.27–147.77 ms |
| 500-file prepare peak RSS, two observations | 17.44–17.58 MiB | 17.33–17.36 MiB |
| 500-file checkpoint peak RSS, two observations | 18.44–18.50 MiB | 18.72–18.78 MiB |
| Persistent browser worker ready, p50 | 63.5 ms | 66.9 ms |
| Persistent browser stream write, p50 | 69.69 MiB/s | 71.81 MiB/s |
| Persistent browser stream read, p50 | 218.58 MiB/s | 258.06 MiB/s |
| Content pins left after closing 16 readers concurrently | 15 in every trial | 0 in every trial |
| Embedded WASM size | 4,615,854 bytes | 4,615,854 bytes |

The WASM SHA-256 is identical:
`0926e52ac5859884a311322968d9f112bf31927d491cb16a77cf4207160dbc93`.
The benchmark native binaries differ by 32 bytes. The two changed JavaScript
modules grow by 1,663 uncompressed bytes in total; there are no new dependencies.

Native ready medians are effectively unchanged in this sample. Browser ready
p50 rises by 3.4 ms (5.4%), and checkpoint peak RSS rises by roughly 0.3 MiB.
Neither observation is hidden by a blanket claim of zero overhead. Transfer
throughput shows no consistent large regression; the observed browser gains are
exploratory and should not be presented as a performance promise.

The candidate's first native block also contains a 289.98 ms startup outlier.
Full block distributions remain in the session JSON files. Portable capture
varies considerably with cache/scheduling effects, including within the baseline;
these measurements support keeping the existing architecture, not a claimed
checkpoint speedup.

## Measurement failure and limits

The first attempt at the general workspace lifecycle benchmark failed in the
unchanged baseline's parallel-prepare cleanup with `ENOTEMPTY`. Its partial
results and error are retained in [failed-attempt](failed-attempt/metadata.json)
and [the runner log](failed-attempt/runner.log), with `complete: false`. They are
excluded from the comparison above. The successful comparison uses serial
portable captures and the workspace memory-only harness, which has no parallel
prepare stage. Parallel workspace lifecycle qualification remains incomplete.

RSS is sampled native process memory, not a time-series browser-heap measurement.
The work uses local storage, warm caches and short runs; it does not measure
cold-disk startup, long-running leaks, remote provider latency or sustained load.
The Git change admits the selected tree and fetched metadata before checkout;
final accounting and cleanup still handle metadata generated during checkout.
It is not an OS-enforced disk quota for Git subprocesses.

## Reproduction and raw evidence

Export the baseline with `git archive 29897c8` into a separate directory. Build
both native binaries with the flags above, provide the same installed Node
dependencies to both exports, and use the committed `dist` files. Run:

```sh
node docs/benchmarks/2026-10-15-patch-validation/qualify.mjs \
  /absolute/baseline /absolute/candidate /absolute/new-results-directory
```

The [qualification harness](qualify.mjs) records identities before running.
The [raw browser trials](results/browser.json) preserve all measured samples.
Native evidence is grouped under `results/1-baseline-*`, `2-candidate-*`,
`3-candidate-*`, and `4-baseline-*`, with JSON summaries and corresponding logs.
The successful run has `complete: true` in its metadata. Keep failed runs and
measurement caveats when sharing these results.
Local absolute paths in log files are redacted; numerical results are unchanged.
