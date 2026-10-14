# Railway benchmark environment

The `benchmark` environment was created and read back on October 13, 2026,
in the existing `stow-conformance-scratch` project.
The [benchmark folder](../benchmark/README.md) now defines the environment with Railway IaC.
Its private target and runner services have deployed successfully.
The [first cloud comparison](benchmarks/2026-10-13-railway-performance/README.md)
completed 378 embedded samples and 45 S3 runs with verified data and zero losses.
Both compute deployments were stopped after exporting evidence; the services,
IaC and volume contents remain available.

- Workspace: `wra-sol's Projects` (`457628da-bacc-4a40-aac3-a7141004534d`).
- Project: `8d90188e-0f1e-4c47-9379-9399ae06e6fa`.
- Environment: `3cd200cb-c94f-4e4d-9335-0f6c1b7e9411`.
- [Open the benchmark environment](https://railway.com/project/8d90188e-0f1e-4c47-9379-9399ae06e6fa?environmentId=3cd200cb-c94f-4e4d-9335-0f6c1b7e9411).

The locally linked Railway project is unrelated. Always pass explicit project
and environment IDs; do not rely on `railway status` for benchmark scope.

## Execution layout

The deployed setup uses two measurement lanes:

1. **Embedded comparisons:** the Linux runner container, limited to 4 vCPU and
   4 GiB memory. Stow, Afero and standard-library buffer/file workloads run
   sequentially on its ephemeral filesystem. Compilation and dependency
   installation finish during Railway image build, before measuring.
2. **S3 comparisons:** a target service and a separate generator service in one
   region, communicating over private networking. The verified ceilings are
   2 vCPU / 2 GiB for the target and 4 vCPU / 4 GiB for the generator, one replica
   each. Stow, S3Proxy and MinIO run sequentially on the same target and data volume.
   Measure generator saturation before accepting a target capacity result.

Containers have resource ceilings; fresh placements
can have different CPUs and storage behavior. Record the effective cgroup CPU
quota, CPU model, memory limit, CPU throttling, filesystem and placement for every
run. Repeat reference runs before and after a comparison batch to detect drift.

For filesystem comparisons, explicitly distinguish ephemeral container storage
from Railway persistent volumes. Use the same storage class, capacity and
acknowledgement/durability policy. Separate-service placements do not guarantee
identical physical disks. Use sequential runs on the same sandbox filesystem
when the goal is to isolate implementation overhead.

## First comparison batch

| Workload | Baselines | Important measurements |
|---|---|---|
| Embedded storage | Afero and SQLite memory/filesystem, owned byte buffers, ordinary and synced atomic files | Throughput, Go heap allocations, concurrency, instance startup and first write |
| S3 storage | S3Proxy memory/filesystem, MinIO and RustFS filesystem | PUT/GET rates, latency, errors, CPU, sampled RSS, process startup and first write |
| Node agent storage | Stow WASM, owned Node buffers, built-in Node SQLite | Fresh-process startup, first write, peak process RSS |
| Inspection and retention | Buffer plus temporary-file pipeline | Files/sec, queue wait, rejected arrivals, peak memory, retained disk bytes |
| Read-through cache | Direct upstream reads, simple application cache | Hit/miss latency, upstream bytes, retained memory, offline reads |

Start with 4 KiB, 1 MiB and 8 MiB payloads and concurrency 1, 4 and 16.
Separate memory from persistent-storage results. Match payload copying,
concurrency safety, hash verification, metadata and write acknowledgement.
Pin all implementation versions and image digests before publishing comparisons.

Use MinIO Warp for shared S3 operations. Use the workflow harness for ordered
inspection workloads; an arbitrary mixed-operation test does not model
receive, inspect, retain flagged content and discard clean content.
Report simulated scanner and real scanner tests separately.

## Implemented runner and remaining scope

- The package includes an explicit snapshot of the uncommitted benchmark sources.
- Pinned Linux images and verified competitor downloads build on Railway.
- The embedded suite includes Afero, SQLite and standard-library adapters.
- Fresh Go instances and fresh Node processes have separate startup/first-write lanes.
- The comparison runner reuses the existing load, histogram and SHA-256 verification code.
- Authenticated private controls restart each target in an owned temporary directory.
- Target-side sampling records process CPU/RSS, Linux I/O and cgroup throttling.
- Raw results persist in the runner's result volume and are exported before stopping compute.

The first batch measures bounded PUT, GET and simulated quarantine workflows.
Flagged-file promotion, a real scanner and read-through cache comparisons remain separate work.
The local workflow harness remains available for desktop measurements.
The expanded October 13 batch runs all testing and analysis on Railway and
produces a private website preview before any publication. Go heap allocation
counters exclude SQLite's C allocations; they are separate from process RSS.

The existing [qualification profile](benchmarks/performance-qualification.json)
specifies the longer duration/repetition protocol. A cloud placement alone does
not qualify a sustained capacity claim. Review failures, dropped arrivals,
verification, queueing and resource saturation for each advertised workload.

## Platform references

- [Railway sandboxes](https://docs.railway.com/sandboxes): resource selection,
  regions, snapshots, idle timeouts and VM billing.
- [Railway scaling](https://docs.railway.com/deployments/scaling): container
  resource ceilings and replicas.
- [Cost control](https://docs.railway.com/pricing/cost-control): private networking
  and resource limits.
- [Railway load-testing guide](https://docs.railway.com/guides/load-test-k6):
  remote generators and target metrics.

See the [performance testing guide](performance-testing.md) for current harness
behavior and measurement definitions.
