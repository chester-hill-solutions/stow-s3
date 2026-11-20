# What a local S3 fixture costs, 2026-11-20

**Scope:** prerequisites and cold-start cost of the local S3 options an agent is
likely to reach for. This is a dated measurement, not a benchmark.

## Method

Five cold starts of `bin/stow-s3 serve --port 0 --data-dir <temp>` on macOS
ARM64, measuring from process start to the `STOW_READY` line. Resident memory is
`ps -o rss` immediately after readiness. No tuning, no warm cache, no network.

## Stow, measured

| Measure | Value |
| --- | --- |
| Cold start to ready | 39, 40, 55, 57, 67 ms; median **55 ms** |
| Idle resident memory | **14.5 MB** |
| Native binary | **21 MB** |
| Daemon, root, image pull | none |
| Listener | loopback by default; remote bind needs explicit consent |

## The alternatives

| Option | Prerequisite | What it proves | Durable workspace |
| --- | --- | --- | --- |
| LocalStack or MinIO in Docker | A running Docker daemon, an image pull, usually a privileged daemon | Real S3 over HTTP | No |
| `moto` or an in-process mock | A Python test dependency, no listener | Whatever the mock implements | No |
| Live provider | Credentials, network, cost, blast radius | Real S3 | No |
| Stow | One binary | Real S3 over HTTP, checked by a shared corpus | Yes, with checkpoints and handoff |

The Docker numbers are deliberately absent. `docker --version` reports 24.0.6 on
this machine, but the daemon is not running, and publishing a start time measured
by starting Docker Desktop would describe this laptop rather than the option.
The prerequisite row is the part that matters and it is a fact, not a
measurement.

## What follows

An agent that needs an S3 fixture is choosing between a daemon and a binary. The
55 ms and 14.5 MB figures are small enough that the fixture can start per test
process, and the absence of a daemon is what makes that possible in a sandbox.
They do not claim throughput, conformance to a live provider, or behaviour under
load; those are separate measurements with their own records.
