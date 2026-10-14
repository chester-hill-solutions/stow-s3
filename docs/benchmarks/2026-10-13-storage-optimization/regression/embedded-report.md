# Embedded storage comparison

Exploratory: 3 repetitions, 500ms per Go benchmark, CPU settings 4.

Rates below convert Go's decimal MB/s to binary MiB/s. Reads include SHA-256 verification. Quarantine counts input payload once per completed workflow.
Filesystem rows use ephemeral runner storage. Temporary files and Afero omit the sync guarantees of the atomic-file baseline; Stow also maintains object metadata and admission state.

| Implementation | Workload | Bytes/file | CPU | Payload MiB/s (min–max) | Allocated B/op | Allocations/op |
|---|---|---:|---:|---:|---:|---:|
| stow-memory | put | 1048576 | 4 | 770.035 (743.628–771.961) | 1075259 | 14 |
| stow-memory | get | 1048576 | 4 | 7195.11 (7023.716–7427.588) | 34372 | 9 |
| stow-memory | quarantine | 1048576 | 4 | 625.553 (623.941–639.515) | 1103492 | 46 |
| stow-filesystem | put | 1048576 | 4 | 336.351 (333.672–338.84) | 2186931 | 320 |
| stow-filesystem | get | 1048576 | 4 | 3775.673 (3655.891–3972.759) | 38190 | 43 |
| stow-filesystem | quarantine | 1048576 | 4 | 225.201 (197.086–237.513) | 2284533 | 1043 |

Allocation counts describe Go heap allocation per operation; they do not measure retained memory, process RSS or a memory leak.
