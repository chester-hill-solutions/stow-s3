# Checkpoint payload reuse

## Problem

Repeated checkpoints of an unchanged or mostly unchanged tree wrote a full
additional payload. The baseline characterization found that a 256 MiB
unchanged checkpoint added about 268.4 MB of allocated storage, and changing
only 32 MiB still copied and stored all 256 MiB.

## Root cause

Capture compared the current workspace to its parent only after making a full
copy. Each checkpoint had its own complete set of payload paths, even when the
parent already held identical bytes.

## Change

When a parent file has the same path, size, source mode, and SHA-256 digest,
capture opens it without following symlinks, verifies its digest, and attempts a
hard link into the staged child checkpoint. It confirms the staged path refers
to the verified inode, then makes that shared payload read-only. If validation
or linking fails, capture copies and hashes the current source as before. The
final source-tree verification remains in place.

The manifest still records each source file's original mode. Restore applies
that mode to the restored workspace. The payload verifier accepts both the
historical stored mode and the new read-only mode, so existing checkpoints
remain readable. The child has its own complete payload path, and deleting the
parent directory does not break restore.

## Evidence

The before measurements are from the checkpoint characterization on the same
Apple M1 Pro; the after measurements use the same four fixture shapes and the
existing opt-in phase diagnostic. Measurements are one or two runs per case,
not a stable benchmark. `payload read estimate` counts logical bytes: inventory
and final source verification read the workspace, and a reused file is read
once to verify its parent payload. The host reported zero block input
operations, so physical read bytes are unavailable. Process CPU and peak RSS
include fixture creation, any baseline checkpoint, measured capture,
verification, and restore.

| Workload | Capture before → after | Payload copied before → after | Added unique allocated payload storage before → after | Verify before → after | Restore before → after | CPU before → after (user + system) | Peak RSS before → after |
|---|---:|---:|---:|---:|---:|---:|---:|
| Small, many files (4,096 × 16 KiB) | 1.00–1.55 s → 1.07 s | 64 → 64 MiB | 67,796,992 → 67,796,992 B | 153–161 → 160 ms | 1.70–1.73 → 1.82 s | 6.43–7.93 → 6.48 s | 239.7–245.7 → 236.6 MB |
| Large, unchanged (16 × 16 MiB) | 0.65–2.28 s → 0.38 s | 256 → 0 MiB | 268,439,552 → 4,096 B | 137–140 → 147 ms | 1.44–4.51 → 1.16 s | 4.13–5.92 → 3.68 s | 245.5–248.2 → 249.0 MB |
| Few changed (2 of 16 × 16 MiB) | 0.77–2.55 s → 0.43 s | 256 → 32 MiB | 268,439,552 → 33,558,528 B | 141–145 → 140 ms | 0.80–16.75 → 0.93 s | 4.05–5.20 → 3.51 s | 238.3–248.5 → 248.1 MB |
| Modified binary (1 × 128 MiB) | 0.68–0.88 s → 0.46 s | 128 → 128 MiB | 134,221,824 → 134,221,824 B | 70–71 → 70 ms | 0.34–0.55 → 0.48 s | 2.27–2.37 → 2.40 s | 283.0–283.3 → 282.9 MB |

After capture and restore latency should be treated as inconclusive: each is a
single run, while before ranges vary substantially, especially on the 256 MiB
restore cases. The stable result is storage and copy volume. An unchanged
256 MiB child adds only 4 KiB of unique allocated files, and changing 32 MiB
adds about 32 MiB plus manifest and filesystem metadata. The diagnostic also
reports per-path allocated bytes; those still appear as a full payload because
hard links have a directory entry in each checkpoint. Unique allocated bytes
deduplicate filesystem device/inode identities.

## Impact

Unchanged file payloads no longer need new data blocks. Few-changed captures
copy only files whose identity differs. The existing inventory hash, parent
payload hash, and final source verification preserve digest checks. The
modified-binary case remains a full-file copy.

## Tradeoffs

Hard links are local to one filesystem. Since staging and published checkpoints
share the registry filesystem, supported filesystems can reuse payload blocks;
link failures fall back to a normal copy. The read-only mode protects against
accidental writes through checkpoint paths. A privileged or same-user process
can still change permissions or mutate the shared inode; restore and export
continue to hash payloads and detect corruption.

## Compatibility

Checkpoint and archive manifest formats do not change. Each child keeps a
complete `files/` path for every captured file; restore never follows a parent
reference. Archive export still writes the same regular tar entries and hashes
their bytes. Logical retention accounting remains full-size per checkpoint,
regardless of shared physical blocks. Existing stored payload modes remain
accepted.

## Regression coverage

- An unchanged file shares an inode with its parent, and the child restores
  after the parent directory is removed.
- A same-size source edit with its original mtime restored does not qualify for
  reuse; the child receives the changed bytes.
- A corrupt parent payload is rejected as a reuse candidate and the current
  source is copied into the child.
- An unchanged hard-linked payload still counts its full logical size against
  the checkpoint retention limit.
- Existing restore and delta corruption tests explicitly mutate read-only
  payloads before verifying that digest checks reject them.

## Remaining risks

The benchmark is not statistically conclusive for latency, and it did not
measure cold-cache physical reads. Hard-link behavior needs validation on
supported non-POSIX filesystems; the fallback preserves correctness where
linking is unavailable. The existing same-user registry threat model still
applies to local payload mutation.
