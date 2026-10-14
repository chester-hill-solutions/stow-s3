# Owned HTTP body handoff — Railway evidence

Completed 2026-10-14. All builds, tests, benchmarks, and numerical analysis ran on Railway. Website charts remain unpublished. Both benchmark deployments were subsequently confirmed `REMOVED`.

HTTP PUT now transfers its privately materialized request buffer into write preparation through a once-only internal ownership contract. Request cache and replay references are cleared at transfer. Caller-owned buffers and ordinary readers retain defensive copies. Authentication, checksum validation before publication, actual-content ETags, metadata ownership, quota enforcement, and lifecycle checks remain enforced. Stream writes reject a declared size that differs from the transferred body size.

## Results

The comparison separates the immediately preceding baseline, memory staging outside the runtime lock (`staging`), and staging plus HTTP buffer handoff (`after`). All live results below use the memory backend and 1 MiB objects.

| Live workflow | Version | Files/s and payload MiB/s | p99 workflow latency, ms | Process high-water RSS, MiB |
| --- | --- | ---: | ---: | ---: |
| PUT | baseline | 487.425 | 12.430 | 69.004 |
| PUT | staging | 509.275 | 11.654 | 70.471 |
| PUT | after | 529.625 | 10.868 | 65.109 |
| Quarantine | baseline | 198.650 | 30.591 | 63.969 |
| Quarantine | staging | 211.100 | 28.674 | 63.689 |
| Quarantine | after | 213.800 | 28.393 | 60.635 |

Values are medians across six windows per version and workflow. Effects use paired geometric mean changes, rather than ratios of those medians. Conditional 95% intervals describe this run on shared hosts.

| Change | PUT throughput | PUT p99 | PUT high-water RSS |
| --- | ---: | ---: | ---: |
| Memory staging alone | +4.530% [1.011, 8.172] | −7.342% [−12.449, −1.938] | +2.097% [0.657, 3.557] |
| HTTP handoff alone | +3.523% [0.913, 6.199] | −6.575% [−11.715, −1.135] | −7.163% [−8.232, −6.082] |
| Both changes | +8.213% [4.831, 11.703] | −13.434% [−18.712, −7.813] | −5.216% [−6.974, −3.425] |

The signed HTTP handler allocation benchmark fell from 2,129,998 to 1,073,004 allocated bytes per 1 MiB PUT: **49.624% less allocation** from the handoff. This measures cumulative bytes allocated per operation, distinct from RSS. Native serial throughput improved 10.388% [3.678, 17.533] and parallel throughput improved 20.252% [15.890, 24.779] from the handoff. Native benchmarks invoke the authenticated handler and runtime in process without sockets, with four Go scheduler CPUs; they are separate from the live two-CPU target measurements.

Quarantine handoff throughput (+0.271%, interval [−2.240, 2.848]) and p99 (−0.990%, interval [−7.225, 5.665]) were inconclusive. Its high-water RSS fell 4.594% [−6.342, −2.814]. Both changes together improved observed quarantine throughput 7.248% [4.694, 9.866], but the generator approached its CPU quota, so this does not establish maximum server capacity. Quarantine includes a fixed 2 ms simulated scanner delay and no actual DPI scan.

Fresh-process readiness and first-write observations show no established startup improvement. These use a 4 KiB loopback write and exclude container startup and WASM boot.

## Protocol and validation

Railway target: 2 CPU / 2 GiB. Generator: 4 CPU / 4 GiB. Observed CPU: AMD EPYC 9655P. Private HTTP without TLS; four clients across four generator processes; 16 fixtures; 5-second warmup followed by 20-second closed-loop windows. Six balanced version permutations produced 36 live windows and six smoke windows, all with zero failures or drops. Integrity checks verified fixtures and final writer objects; quarantine left no workflow objects. Eighteen native invocations produced 36 serial/parallel samples. Pooled workflow histograms determine p99; latency includes client hashing and SDK/workflow work. Maximum measured worker start skew was 3 ms.

The allocation regression failed before the handoff at 2,132,193 B/op, then passed after it. Storage/runtime/API race checks passed for all three variants; older versions skip only the new allocation bound assertion. Full final validation passed standards, conformance for direct and runtime memory/filesystem stores, Node/WASM tests, browser tests, generated protobuf checks, and private chart rendering checks. Browser result: 33 passed, two skipped, zero unexpected failures. Public API reference changes: none.

The rebuilt SDK WASM is 6,504,060 bytes, **6,365 bytes (about 0.10%) larger** than staging alone. SHA-256: `d41c489a93c7a349de69c5bb415b04f4de3964bdacb12fdde8557d9d6eed9283`. The exact validated binary is installed in the SDK distribution. WASM performance was not measured in this native comparison.

Two superseded attempts failed evidence packaging: copied documentation contained links relative to its original tree, and a macOS source archive contained AppleDouble files. Generated documentation is now retained as an archive; AppleDouble entries were removed while preserving actual source bytes. Final validation and comparison completed successfully.

## Evidence and identities

- [Raw run receipt](comparison/run.json), [checked analysis and paired intervals](comparison/analysis.json), [generated tables](comparison/report.md), and [exact analysis source](comparison/analyze-owned.mjs).
- [Final validation receipt](verification/receipt.json), [validation export checksum](validation-export.json), and [comparison export checksum](comparison-export.json).
- [Allocation regression log](red.log), [compiled variant identities](comparison/variants.json), and [retention receipt](comparison/retention.json).
- [Baseline source archives](comparison/baseline-source-archives.tar.gz), [exact after native source](comparison/after-native-source.tar.gz), and [archived generated documentation](verification/rendered-documentation.tar.gz).

Run ID: `c80e35c3-f6e3-4020-91af-ae8ad6c8e95b`; UTC 13:18:06–13:36:34 on 2026-10-14. Comparison source payload SHA-256: `178ca240d23a9f260a1bafc5404521d0e1efd1ee0bc96d0d19d5d6e03fc38f55`. Exact after production source projection: `ed958b131d418fcc55930686752127519be43a7192e603126daaf85ff5cb4b04`. The after source archive was unpacked and verified against the compiled source on Railway. Local exports were checksum verified; native binaries remain on the Railway results volume, with paths and digests in the retention receipt.

Final full-validation deployment: `2237f150-6918-471c-b5c7-ad8dfce2cbe8`. Benchmark target: `8f49fc2d-2f4c-4714-8030-ad94811acc0b`; runner: `322bffea-01e1-4213-99a6-b2903e9d3843`. Both benchmark deployments reported `SUCCESS` during execution and `REMOVED` after shutdown. The [environment receipt](../railway-environment.json) records completion and stopped compute.
