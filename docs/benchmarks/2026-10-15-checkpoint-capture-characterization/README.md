# Checkpoint capture characterization

## Problem

Every file checkpoint hashes the included workspace files, copies each file
into a new checkpoint directory, hashes the copy, then hashes the source tree
again before publication. This duplicates I/O and storage when a later
checkpoint has few changes.

## Root cause

Checkpoint payloads are stored as complete files under each checkpoint ID.
The capture path does not compare payloads against its parent checkpoint, so
it copies the full workspace regardless of how many files changed.

## Measurement

The existing opt-in `TestCheckpointPhaseProfile` diagnostic was extended to
select the four requested workload shapes. Each command below was timed on an
Apple M1 Pro using `/usr/bin/time -l`; the test logs phase timings, verification
and restore latency, and checkpoint-directory sizes. Capture, recovery and
process metrics are one or two runs per scenario, not a statistically stable
benchmark.

| Workload | Files and bytes | Changed for measured capture | Capture time range | Payload read estimate per capture | Payload copied | Added checkpoint storage (logical / allocated) | Verify range | Restore range | Process CPU (user + system) | Peak RSS range |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Small, many files | 4,096 × 16 KiB = 64 MiB | First capture | 1.00–1.55 s | 192 MiB | 64 MiB | 67,793,056 / 67,796,992 B | 153–161 ms | 1.70–1.73 s | 6.43–7.93 s | 239.7–245.7 MB |
| Large, mostly unchanged | 16 × 16 MiB = 256 MiB | 0 of 16 | 0.65–2.28 s | 768 MiB | 256 MiB | 268,438,382 / 268,439,552 B | 137–140 ms | 1.44–4.51 s | 4.13–5.92 s | 245.5–248.2 MB |
| Few changed | 16 × 16 MiB = 256 MiB | 2 of 16 (32 MiB) | 0.77–2.55 s | 768 MiB | 256 MiB | 268,438,382 / 268,439,552 B | 141–145 ms | 0.80–16.75 s | 4.05–5.20 s | 238.3–248.5 MB |
| Modified binary | 1 × 128 MiB | Entire file | 0.68–0.88 s | 384 MiB | 128 MiB | 134,218,105 / 134,221,824 B | 70–71 ms | 0.34–0.55 s | 2.27–2.37 s | 283.0–283.3 MB |

The three source-read passes are the inventory/hash, copy/hash, and final
source verification. `payload read estimate` is therefore 3 × the input size;
the copied payload is the full input size. The host reported zero block input
operations for these runs, so these read counts describe logical payload bytes,
not physical disk traffic. Allocated storage uses each file's filesystem block
count; logical storage sums file lengths and includes manifest/receipt overhead.

Capture and restore timings vary sharply between runs, especially for the two
256 MiB cases. Treat those latency ranges as inconclusive. The stable finding
is storage and copy volume: an unchanged 256 MiB capture adds another 268.4 MB
of allocated checkpoint files, and changing only 32 MiB still copies and stores
the full 256 MiB. File verification costs roughly 140 ms for the 256 MiB tree.
Peak RSS and CPU are for the whole test process, including fixture creation,
baseline capture where applicable, measured capture, verification and restore;
they are not capture-only measurements.

## Change

No capture behavior is changed in this report. The existing diagnostic now
profiles all four workloads and records the phases and storage totals.

## Impact

Payload reuse is worth evaluating for mostly-unchanged and few-changed trees:
the current implementation writes a complete additional checkpoint even when
no file changed. The modified-binary case remains a full-file operation.

## Tradeoffs and proposed design

A reuse candidate can be selected only when parent and current path, size,
SHA-256 digest, and mode match. Mtime alone must not establish identity. The
parent payload must itself pass digest verification before reuse, and the
normal final source verification must remain. A proposed self-contained local
implementation is to hard-link a verified parent payload into the new
checkpoint directory; the child's link survives deletion or collection of the
parent directory. Filesystems that cannot create the link must fall back to a
verified copy. The mode saved in the manifest remains the restored workspace
mode; the stored payload link must be read-only so Stow does not mutate a
shared inode through one checkpoint path.

This design keeps each checkpoint's manifest and file paths complete. Archive
format and archive contents need no version change: export reads and hashes
each manifested file and writes the same regular tar entry, independent of
hard-link relationships. Retention accounting must continue to charge each
checkpoint's full logical payload size; deduplicated physical blocks do not
raise the workspace's logical retention allowance. Restore continues to
verify each file by digest and reads the child checkpoint's own path, so it
does not resolve a parent reference. These compatibility and retention
decisions are recorded before any reuse implementation.

## Compatibility and remaining risks

The diagnostic changes no public API, archive, retention, or capture behavior.
The proposed hard-link design still requires validation on supported filesystems
and Windows. A hard link shares an inode, so payload permissions and all copy,
delete, corruption, export, restore, and parent-removal paths need regression
coverage before implementation. No checkpoint reuse code is included here.
