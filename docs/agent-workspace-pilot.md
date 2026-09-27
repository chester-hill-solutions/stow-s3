# Agent workspace local pilot

**Run date:** 2026-09-27
**Environment:** macOS arm64, local Go CLI, warm build cache
**Purpose:** smoke-check the documented prepare-to-artifact flow and record
single-run timings. These runs used tiny synthetic inputs; they are not external
adoption pilots or performance benchmarks.

## File-input task

Prepared two files (70 seeded bytes) into a new workspace, captured a
checkpoint, exported it, previewed it, and imported it into a second registry.

| Operation | Elapsed |
|---|---:|
| Prepare and return launch descriptor | 0.3163 s |
| Checkpoint | 0.0586 s |
| Export | 0.0107 s |
| Preview | 0.0046 s |
| Import | 0.0090 s |

The checkpoint contained 2 files and 70 payload bytes. The compressed archive
was 474 bytes.

## Git-ref task with fixtures and resume

Prepared a one-commit local Git repository plus a task description and fixture
into a dedicated `repo/` working directory. Closed the prepare process, wrote a
handoff reference, resumed in a separate CLI process, then checkpointed,
exported, previewed, and imported the result.

| Operation | Elapsed |
|---|---:|
| Prepare selected Git ref and fixtures | 0.4366 s |
| Write handoff reference | 0.0617 s |
| Resume after process exit | 0.0640 s |
| Checkpoint | 0.0689 s |
| Export | 0.0123 s |
| Preview | 0.0059 s |
| Import | 0.0101 s |

Preparation reported the selected commit and the expected `repo/` working
directory. The selected shallow repository and fixtures accounted for 24,822
seeded bytes. The checkpoint contained 3 paths and 196 payload bytes; the
compressed archive was 661 bytes. Resume returned the same workspace ID.

## Limits of this evidence

The timings include a new CLI process for each operation, but only one run per
operation and very small files. They do not measure a real agent's time to
understand a task, large-repository setup, sustained parallel workspaces,
cross-platform behavior, or external-user setup. The release pilot still needs
separate Git-repository, non-Git, interrupted/resumed, and artifact-transfer
workloads on clean installs, with repeated timings and disk/recovery measures.
