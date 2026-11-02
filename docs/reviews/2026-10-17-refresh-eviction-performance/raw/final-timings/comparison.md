| CPU | Workload | Median before → after | Change | B/op before → after | Allocations before → after | Saves/op | GETs/op |
|---|---|---:|---:|---:|---:|---:|---:|
| 1 | oversized memory/stable | 2.530 → 0.541 μs | -78.60% | 2,968.0 → 456.0 | 31 → 9 | 1 → 1 | 2 → 1 |
| 1 | oversized memory/changed | 3.527 → 1.282 μs | -63.64% | 4,376.0 → 1,544.0 | 47 → 19 | 2 → 1 | 2 → 1 |
| 1 | oversized file/stable | 3.703 → 3.148 ms | -14.97% | 7,657.0 → 5,141.0 | 85 → 62 | 1 → 1 | 2 → 1 |
| 1 | oversized file/changed | 6.757 → 3.395 ms | -49.75% | 13,751.0 → 6,229.0 | 154 → 72 | 2 → 1 | 2 → 1 |
| 1 | control cache-hit | 0.748 → 0.743 μs | -0.78% | 1,208.0 → 1,208.0 | 12 → 12 | 0 → 0 | 0 → 0 |
| 1 | control head-evicted | 3.887 → 3.722 ms | -4.23% | 7,360.0 → 5,141.0 | 79 → 62 | 1 → 1 | 1 → 1 |
| 4 | oversized memory/stable | 2.607 → 0.548 μs | -78.99% | 2,975.0 → 457.0 | 31 → 9 | 1 → 1 | 2 → 1 |
| 4 | oversized memory/changed | 3.950 → 1.447 μs | -63.37% | 4,387.0 → 1,547.0 | 47 → 19 | 2 → 1 | 2 → 1 |
| 4 | oversized file/stable | 3.244 → 3.131 ms | -3.46% | 7,720.5 → 5,210.5 | 85 → 63 | 1 → 1 | 2 → 1 |
| 4 | oversized file/changed | 6.336 → 3.263 ms | -48.51% | 13,903.5 → 6,268.0 | 155 → 73 | 2 → 1 | 2 → 1 |
| 4 | control cache-hit | 0.811 → 0.814 μs | +0.35% | 1,208.0 → 1,208.0 | 12 → 12 | 0 → 0 | 0 → 0 |
| 4 | control head-evicted | 3.248 → 3.056 ms | -5.92% | 7,367.5 → 5,144.5 | 80 → 63 | 1 → 1 | 1 → 1 |

Adverse: CPU 4 BenchmarkRefreshVersionControl/cache-hit: +0.35%
