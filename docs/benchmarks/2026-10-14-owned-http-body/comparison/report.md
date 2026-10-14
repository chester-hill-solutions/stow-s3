# Memory staging and HTTP ownership comparison

| Workflow | Version | Files/s | Payload MiB/s | p99 ms | Server high-water MiB |
|---|---|---|---|---|---|
| put | baseline | 487.425 | 487.425 | 12.43 | 69.004 |
| put | staging | 509.275 | 509.275 | 11.654 | 70.471 |
| put | after | 529.625 | 529.625 | 10.868 | 65.109 |
| quarantine | baseline | 198.65 | 198.65 | 30.591 | 63.969 |
| quarantine | staging | 211.1 | 211.1 | 28.674 | 63.689 |
| quarantine | after | 213.8 | 213.8 | 28.393 | 60.635 |

| Native signed HTTP | Version | MiB/s | Allocated B/op | Allocations/op |
|---|---|---|---|---|
| serial | baseline | 489.023 | 2129535 | 188 |
| serial | staging | 488.705 | 2129998 | 194 |
| serial | after | 540.875 | 1073004 | 189 |
| parallel | baseline | 684.291 | 2129498.5 | 189 |
| parallel | staging | 1438.081 | 2129805.5 | 193 |
| parallel | after | 1723.597 | 1072917 | 189 |

- Six blocked repetitions on shared Railway hosts; intervals condition on this run and exclude broader host variation.
- Twenty-second closed-loop windows; four clients across four Node processes; private HTTP without TLS.
- Quarantine includes a fixed two-millisecond simulated scanner delay and no real DPI scanning.
- Service latency includes client hashing, SDK work, workflow steps, and accepted drain completions; pooled histograms use bounded percentile approximation.
- Process high-water RSS is distinct from cumulative bytes allocated per operation.
- Server CPU sampling includes the start barrier and final worker verification; client CPU includes measured traffic and drain.
- WASM in the comparison image is not executed; rebuilt SDK size and identity are reported by the separate full validation receipt.
- Native signed HTTP benchmarks use the handler and runtime in process without sockets on four Go scheduler CPUs; live HTTP targets have a two-CPU quota.
