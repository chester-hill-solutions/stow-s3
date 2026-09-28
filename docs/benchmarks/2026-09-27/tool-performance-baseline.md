# Local tool performance baseline

**Measured:** 2026-09-27, Toronto local time (timestamps are UTC in the raw files)
**Machine:** Apple M1 Pro, 8 CPU cores, 16 GiB RAM; macOS 14.5 arm64; Node v25.9.0; pinned Go 1.24.13
**Purpose:** baseline startup, S3 request, concurrency, workspace lifecycle, and memory costs for the current local implementation.

Reproduce with `make benchmark`. The workspace RSS profile is separate because macOS high-water RSS uses `/usr/bin/time -l`:

```sh
node packages/stow-s3/scripts/benchmark-workspace.mjs --memory-only
```

Raw measurements:

- [Single-session lifecycle](session-lifecycle.json) — 30 runs, 1 MiB payload.
- [Session memory sweep](session-memory.json) — 10 runs at each of 0, 1, 2, 4, and 8 MiB.
- [Connections and parallel sessions](concurrency.json) — five runs per point, 1 MiB payload.
- [Workspace lifecycle and parallel preparation](workspace-lifecycle.json) — seven lifecycle runs per workload and five concurrency runs per point.
- [Workspace CLI RSS](workspace-memory.json) — one native CLI high-water sample per workload.

## S3 session lifecycle

One local memory-backed server, one TypeScript SDK client, and a 1 MiB object. Latencies include normal process startup where stated. Values are p50 / p95 milliseconds across 30 sequential fresh sessions.

| Measurement | p50 | p95 |
|---|---:|---:|
| Server ready | 15.92 ms | 16.85 ms |
| Create bucket | 2.26 ms | 2.63 ms |
| First 1 MiB PUT request | 6.06 ms | 7.09 ms |
| Process start through completed first PUT | 24.06 ms | 25.83 ms |
| 1 MiB GET including body read | 2.43 ms | 3.05 ms |
| Loaded server RSS observation | 18.48 MiB | 20.52 MiB |
| Shutdown | 1.30 ms | 2.06 ms |

The first-upload figure includes server startup, bucket creation, and the first PUT. It is the closest measure here to a fresh SDK session becoming useful.

## Other S3 operations

These measurements run after the first-upload/read memory sample. Values are p50 / p95 request latency across the same 30 fresh sessions.

| Operation | Payload / workload | p50 | p95 |
|---|---|---:|---:|
| HEAD object | 1 MiB object | 1.16 ms | 1.74 ms |
| LIST objects | One-object bucket | 0.90 ms | 1.41 ms |
| COPY object | 1 MiB source to a second key | 0.87 ms | 1.18 ms |
| DELETE object | Copied object | 0.60 ms | 0.85 ms |
| Create multipart upload | Empty upload | 0.69 ms | 1.17 ms |
| Upload part 1 | 5 MiB | 25.10 ms | 30.02 ms |
| Upload part 2 | 1 MiB final part | 5.29 ms | 7.60 ms |
| Complete multipart upload | 6 MiB total | 1.82 ms | 3.51 ms |
| Delete multipart result | Completed object | 0.82 ms | 2.84 ms |
| Full HEAD/LIST/COPY/DELETE/multipart suite | Sequential, 6 MiB multipart | 37.43 ms | 46.04 ms |

## Connections to one server

Each connection is an independent keep-alive HTTP agent limited to one socket. Every point sends two batches of 1 MiB PUTs and GETs. Five runs per point; latency percentiles combine the individual requests. Throughput is the p50 across runs.

| Sockets | PUT latency p50 / p95 | PUT MiB/s p50 | GET MiB/s p50 | Loaded server RSS p50 |
|---:|---:|---:|---:|---:|
| 1 | 6.08 / 12.35 ms | 167.11 | 369.57 | 21.31 MiB |
| 4 | 14.84 / 19.25 ms | 213.28 | 689.72 | 38.77 MiB |
| 8 | 25.77 / 35.71 ms | 232.97 | 694.78 | 55.94 MiB |
| 16 | 44.97 / 61.95 ms | 252.59 | 918.61 | 89.44 MiB |

More connections increase aggregate throughput, but with diminishing returns: going from 8 to 16 adds about 8% PUT throughput while p50 PUT latency rises about 75% and loaded RSS rises by 33.5 MiB. For this local memory server, 8 connections is already near the PUT throughput knee. This is a local loopback result, not a network endpoint capacity claim.

## Independent sessions in parallel

Each session has its own server and uploads one 1 MiB object concurrently. Five runs per point.

| Parallel sessions | Start through first upload p50 | PUT latency p50 | Aggregate PUT MiB/s p50 | Combined loaded RSS p50 |
|---:|---:|---:|---:|---:|
| 1 | 24.70 ms | 6.69 ms | 149.21 | 19.38 MiB |
| 4 | 35.63 ms | 10.75 ms | 358.22 | 73.73 MiB |
| 8 | 44.88 ms | 16.20 ms | 438.24 | 146.64 MiB |

Eight independent sessions sustain about 438 MiB/s of aggregate local PUTs on this machine, at roughly 18.3 MiB loaded RSS per session after upload. Startup stays under 45 ms p50 for the eight-session batch.

## Memory scaling by session payload

Ten sessions per size; RSS is sampled after the 0–8 MiB object has been stored and read, before the extra S3 operation suite. It is an observed loaded-process sample, not an OS-sampled peak during every allocation.

| Payload | Loaded RSS p50 | Ready p50 |
|---:|---:|---:|
| 0 MiB | 10.73 MiB | 15.54 ms |
| 1 MiB | 18.88 MiB | 15.60 ms |
| 2 MiB | 24.22 MiB | 15.83 ms |
| 4 MiB | 33.64 MiB | 15.87 ms |
| 8 MiB | 53.22 MiB | 16.07 ms |

The fitted increase is **5.31 MiB of observed RSS per MiB stored**, above a 10.73 MiB empty-server floor. Startup is nearly flat across the tested object sizes. The existing 16 MiB session default should therefore be understood as an object quota, not a memory ceiling.

## Workspace handoff lifecycle

Operations run through the TypeScript wrapper, which starts a fresh native CLI process for each call. Seven runs per workload. The agent-shaped case matches the repository's existing WASM working set (24 files, 597,007 bytes); the scale case is 500 mixed source/binary files totaling 16 MiB.

| Workload | Prepare p50 / p95 | Handoff p50 | Resume p50 | Checkpoint after edit p50 | Diff p50 | Export p50 | Import p50 | Restore p50 | Destroy p50 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Tiny, 2 files / 70 B | 68 / 139 ms | 65 ms | 64 ms | 66 ms | 6 ms | 11 ms | 12 ms | 61 ms | 65 ms |
| Agent-shaped, 24 files / 0.57 MiB | 81 / 88 ms | 65 ms | 63 ms | 76 ms | 6 ms | 21 ms | 18 ms | 74 ms | 75 ms |
| Scale, 500 files / 15.63 MiB | 240 / 461 ms | 71 ms | 71 ms | 373 ms | 7 ms | 144 ms | 292 ms | 342 ms | 247 ms |

`Destroy` is for the original prepared workspace; the separately restored workspace is also destroyed and is recorded in the JSON. Handoff here writes a 167-byte same-machine reference and does not copy files or transfer data across machines.

| Workload | Root on disk | Registry with two checkpoints | Root + registry | Compressed export |
|---|---:|---:|---:|---:|
| Tiny | 1,364 B | 3,469 B | 4,833 B | 549 B |
| Agent-shaped | 598,301 B | 1,205,143 B | 1,803,444 B | 267,999 B |
| Scale | 16,385,294 B | 32,948,365 B | 49,333,659 B | 1,750,048 B |

The registry footprint with two full checkpoints is about twice the seeded payload; root plus registry is about three times payload. Retention and checkpoint count therefore matter more to disk usage than the tiny handoff reference. Workspace CLI high-water RSS was 10.66 MiB for agent-shaped prepare and 11.64 MiB for its checkpoint; the 500-file/16 MiB case was 14.84 MiB and 14.94 MiB respectively. These RSS values are one sample each from macOS `/usr/bin/time -l`.

## Parallel workspace preparation

Five runs per point, each preparing identical agent-shaped inputs into distinct roots while sharing a registry. Preparation commands are launched concurrently through the TypeScript wrapper.

| Concurrent workspaces | Batch ready p50 / p95 | Per-prepare latency p50 / p95 | Workspaces/s p50 | Seeded bytes total |
|---:|---:|---:|---:|---:|
| 1 | 87 / 119 ms | 87 / 119 ms | 11.43 | 0.57 MiB |
| 4 | 224 / 252 ms | 216 / 246 ms | 17.85 | 2.28 MiB |
| 8 | 398 / 417 ms | 366 / 404 ms | 20.11 | 4.56 MiB |

Sharing a registry remained correct at these concurrency levels. Throughput grows with concurrency but levels off, which points to local CLI starts, filesystem work, and registry coordination as the next places to profile if parallel workspace setup is a priority.

## Scope and limits

- S3 request benchmarks use the memory backend and loopback only. They do not measure AWS S3, R2, run-through propagation, WAN latency, or provider throttling.
- Workspace files are generated fixtures on local APFS; the scale mix uses mostly compressible source text plus binary fixtures. It is not a clone of a large production repository.
- Workspace latency percentiles use seven samples, so p95 is effectively the maximum observation. Concurrency points use five batch samples; treat these as baseline signals, not service-level guarantees.
- Session loaded RSS is sampled after operations; the separate workspace process RSS uses OS high-water reporting. These memory numbers use different measurement methods.
- The prepared-workspace runs cover sequential lifecycle and concurrent prepare. They do not measure many concurrent checkpoint/export operations, disk contention from long-lived workspaces, or agent wall-clock task time.
- Provider-backed and clean-install benchmarks remain outstanding. This report establishes local machine baseline only.
