# Streaming and quarantine performance regression

All workloads and analysis ran on Railway. Same S3 configuration and CPU limits as the earlier comparison. Competitors were not remeasured.

| Backend / workflow | Before rate | After rate | Ratio | p99 ms before → after | Sampled RSS MiB before → after |
|---|---:|---:|---:|---:|---:|
| stow-memory / put | 197.35 | 174.7 | 0.885× | 38.075 → 42.48 | 68.598 → 66.91 |
| stow-filesystem / put | 66.45 | 66.5 | 1.001× | 93.234 → 101.969 | 33.367 → 28.273 |
| stow-memory / get | 505.6 | 510.95 | 1.011× | 18.415 → 18.973 | 67.195 → 51.676 |
| stow-filesystem / get | 356.95 | 492.7 | 1.38× | 29.105 → 19.354 | 30.203 → 20.988 |
| stow-memory / quarantine | 113.95 | 108.55 | 0.953× | 51.833 → 53.938 | 69.012 → 61.211 |
| stow-filesystem / quarantine | 50.35 | 56.65 | 1.125× | 124.422 → 119.567 | 30.051 → 25.629 |

Rates are files/s; at 1 MiB each, PUT/GET rates also equal payload MiB/s. Quarantine counts incoming payload once.

Exploratory cross-run comparison, shared Railway hardware, warm bounded working set; no scanner, DPI, TLS, power-loss or sustained capacity qualification. Native baseline uses 300ms windows and new native cases use 500ms.
