# Paired write-preparation throughput comparison

Six paired rounds on one target worker. Student-t interval for mean log throughput ratio, conditional on these rounds; shared-host noise and client limits remain. No sustained-capacity qualification.

Reconstructed immediately preceding write-preparation implementation; keeps usage/recovery consolidation and all earlier changes. Not the historical pre-streaming build.

| Backend | Workload | Before | After | Median paired change | Conditional 95% interval |
|---|---|---:|---:|---:|---:|
| memory | put | 177.65 | 171.05 | -4.314% | -10.616 to 6.794% |
| filesystem | put | 66.5 | 66.475 | 0.038% | -1.695 to 3.909% |
| memory | get | 528.05 | 535.9 | -0.467% | -8.55 to 14.416% |
| filesystem | get | 541.275 | 551.7 | 2.249% | -10.043 to 9.065% |
| memory | quarantine | 104.825 | 104.425 | -1.388% | -4.265 to 3.609% |
| filesystem | quarantine | 55.875 | 54.4 | -3.616% | -19.935 to 14.843% |

PUT and GET rates are MiB/s for 1 MiB objects; quarantine rates are complete files/s. Reads are SHA-256 verified. Quarantine uses a 2 ms simulated scanner.
Private HTTP, no TLS, four clients, six paired 20 s windows, 5 s warm-up, bounded fixtures. Native measurements use ephemeral runner storage; S3 disk uses the target volume.

Managed filesystem saves: 98.29 to 114.884 MiB/s; allocations 7417247.5 to 2166604.5 B/op.
Managed saves use 64 acknowledged keyed replacements per sample, fresh stores and retained receipts. Every sample verifies the final payload.

Competitors were not remeasured. This compares the write-preparation implementation only; it does not attribute the earlier streaming-build HTTP PUT regression.
