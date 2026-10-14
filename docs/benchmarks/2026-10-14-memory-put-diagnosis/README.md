# Memory HTTP PUT generator diagnosis

The original single-process Node generator limited the measured memory HTTP PUT rate.
Splitting the same four clients across four generator processes raised median throughput
from **173.200 to 516.225 MiB/s**, using the **identical server binary**.
This is a correction to the measurement setup, not a production performance change.

Six alternating paired repetitions used 1 MiB objects, sixteen retained fixtures,
four total clients, five-second warmups, and twenty-second measured windows.
Every pair favored four generator processes. The paired geometric improvement was
198.400%, with a conditional 95% interval of 187.017% to 210.235%.
These intervals describe this shared Railway environment, not general hardware variation.
Two short smoke windows and two separate instrumented windows also passed.
All sixteen measured windows had zero failed/dropped operations and verified all sixteen
fixtures plus four distinct final writer objects. Maximum observed start skew was 2 ms.
SDK signing, default checksums, per-write SHA-256 verification hashes, headers,
socket concurrency, server quotas, and object counts were preserved.

Generator CPU rose from a median 101.511% to 279.160%; sampled server CPU rose from
83.263% to 169.600% on its two-core quota. Generator quota throttling was zero.
Two four-process windows recorded one server throttled period each (0.016 and 0.013
cumulative seconds); remaining windows recorded none. Server sampling includes the
start barrier and final verification, while client CPU covers measured traffic and drain.

The client Inspector profile attributes about 74.5% of single-process wall-time samples
to `writev`, and about 12.1% to native hash `update`; client GC was about 0.3%.
Inspector samples include idle/native calls and cannot distinguish kernel CPU from
time spent inside those calls. They identify the active stack, not an exact native cause.
Multiprocess scaling and process CPU accounting establish the generator limitation.

The four-process server CPU profile attributes 37.23% of CPU samples to syscalls,
34.48% to MD5 ETag generation, and 14.75% to payload SHA-256 verification.
Allocation profiles locate 98.85% of cumulative allocated bytes in `readOwnedBytes`,
split between request materialization and the owned storage copy. This is cumulative
allocation, not resident memory. GC was a small fraction of sampled CPU.
The mutex profile attributes about 4.6 cumulative seconds of contention to runtime
`Instance.PutObject` over startup, warmup, and the profiling window. This is aggregate
waiting time across goroutines, not 4.6 seconds of wall-clock blocking.
Preparation outside the instance write lock and avoiding redundant body materialization
are follow-up candidates; their speedup has not been measured.

Profiler windows are excluded from throughput statistics. The separate profiling binary
contains a benchmark-only overlay with a loopback profiler, controlled through the
authenticated target service. Production Go source and the SDK WASM artifact were unchanged.
Six harness tests passed in the Railway image build, including split-worker key isolation.
The analyzer attribution test passed separately on the Railway runner.

Both services ran on AMD EPYC 9655P hosts: generator four CPUs / 4 GiB, target two
CPUs / 2 GiB. Traffic used Railway private HTTP without TLS.
Run `7d3dc9b9-11d7-47c8-be81-f39d343982eb` ran from
2026-10-14 04:32:32.256 to 04:39:29.491 UTC.
Source payload SHA-256: `9598f9438b812cff968949a9bd877ddf999cd7074da83f1bb5b2a38a88d1db48`.
Runner deployment: `27c7ece6-0ac7-4e53-8111-fa3ad872a514`.
Target deployment: `ff0f0be6-287e-4809-aa5a-de667d008fac`.
Both were observed `SUCCESS`, then `REMOVED` after verified export.

- [Run receipt and raw measurements](run/run.json), [checked analysis](run/analysis.json), and [paired report](run/report.md).
- [Single-process client profile](run/client-1-0.cpuprofile), [server CPU](run/server-4-profile-top.txt), [server mutex](run/server-4-mutex-top.txt), and [allocation profile](run/server-4-heap-top.txt).
- [Executed harness and identity](run/executed-source), [executed analyzer](run/analyze-diagnosis.mjs), [analyzer test result](run/analysis-tests.txt), and [export verification](export.json).
- [Retained binaries](run/retention.json) remain on the Railway results volume. The exact production source archive is retained from the preceding controlled comparison.

Historical one-process competitor results remain useful as measurements of that setup,
but do not establish each server's maximum capacity. Repeat MinIO, S3Proxy, RustFS,
and Stow with a generator that has sufficient headroom before drawing capacity
comparisons or publishing website bars. No competitor was remeasured in this diagnosis.
No charts were published; the website dataset was unchanged.
