# Embedded storage comparison

Exploratory: 3 repetitions, 500ms per Go benchmark, CPU settings 4.

Rates below convert Go's decimal MB/s to binary MiB/s. Reads include SHA-256 verification. Quarantine counts input payload once per completed workflow.
Filesystem rows use ephemeral runner storage. Temporary files and Afero omit the sync guarantees of the atomic-file baseline; Stow also maintains object metadata and admission state.

| Implementation | Workload | Bytes/file | CPU | Payload MiB/s (min–max) | Allocated B/op | Allocations/op |
|---|---|---:|---:|---:|---:|---:|
| stow-memory | put | 1048576 | 4 | 823.956 (814.495–839.567) | 1074666 | 14 |
| stow-memory | get | 1048576 | 4 | 6817.827 (6771.269–6906.977) | 34396 | 10 |
| stow-memory | quarantine | 1048576 | 4 | 636.12 (633.707–666.771) | 1103205 | 47 |
| stow-filesystem | put | 1048576 | 4 | 349.398 (344.324–368.052) | 1112601 | 158 |
| stow-filesystem | get | 1048576 | 4 | 4806.929 (4776.325–4960.279) | 37907 | 44 |
| stow-filesystem | quarantine | 1048576 | 4 | 262.499 (254.307–266.237) | 1156305 | 441 |

Allocation counts describe Go heap allocation per operation; they do not measure retained memory, process RSS or a memory leak.
