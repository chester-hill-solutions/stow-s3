# Stow benchmark environment

This folder defines and runs storage comparisons on Railway.
The IaC partial manages two private services and two 5,120 MB volumes.

| Service | Role | Resource ceiling |
|---|---|---|
| benchmark-target | Runs one S3 implementation at a time on the same data volume | 2 vCPU, 2 GiB RAM |
| benchmark-runner | Runs embedded comparisons, generates S3 load and saves evidence | 4 vCPU, 4 GiB RAM |

Both services use one replica in US East.
The controls require a random shared `BENCH_CONTROL_TOKEN`.
They have no public domains.

## Compared implementations

- Embedded: Stow memory/filesystem, Afero memory/filesystem, SQLite memory/filesystem, owned Go byte buffers,
  temporary files, and files published with rename and file/directory sync.
- S3: Stow memory/filesystem, S3Proxy memory/filesystem, MinIO and RustFS single-directory storage.

These compare local storage workflows. They do not establish parity in distributed
durability or object-management features.
The temporary-file and Afero filesystem baselines do not make the same publication
or sync guarantees as Stow. Keep those rows separate when interpreting write rates.

Every measured read checks SHA-256 and length.
Writes reuse bounded worker keys and change a sequence prefix.
Quarantine means PUT, verified GET, simulated scanner delay, verdict PUT and two deletes.
Flagged-file promotion, real scanning and cache-hit workloads are not measured by this first batch.

## Reproduce the infrastructure

Use Railway CLI 5.67 or newer and the pinned IaC SDK.
Check `railway --version` from this folder; shell directory hooks can select an older CLI.
Use an absolute CLI path when needed.

```sh
cd benchmark
npm ci --ignore-scripts
railway link --project <project-id> --environment benchmark
railway config plan --out plans/benchmark.json
railway config apply --plan plans/benchmark.json --yes
```

Set the shared `BENCH_CONTROL_TOKEN` securely in Railway before planning.
Review the saved plan before applying it.
The plan should create only the two services and two volumes.
The partial rejects environments other than `benchmark`.
See the [environment record](../docs/benchmarks/railway-environment.json) for the current IDs.

## Package and deploy

Run from the repository root:

```sh
node benchmark/pack.mjs benchmark/context
railway up benchmark/context --path-as-root --no-gitignore --project <project-id> --environment benchmark --service benchmark-target --detach
railway up benchmark/context --path-as-root --no-gitignore --project <project-id> --environment benchmark --service benchmark-runner --detach
```

The package contains explicit source directories and the uncommitted benchmark work.
It excludes credentials, caches and `node_modules`.
The Docker build verifies pinned MinIO and S3Proxy downloads.
Node, Go and Java base images use immutable digests.
The build records binary, harness, source-payload and SDK lockfile hashes.

Verify `SUCCESS` for each submitted deployment before running load.
Run the smoke profile first.
Only run the comparison profile after all implementations pass the smoke checks.
Use the authenticated runner `/run` and `/status` routes through Railway SSH.
The runner writes each job to `/results/<run-id>`.
Download that directory before stopping the deployments.

For example, from the repository root (use the IDs in the environment record):

```sh
railway ssh --project <project-id> --environment <environment-id> --service benchmark-runner -- node --input-type=module -e 'import {remote} from "./benchmark/http.mjs"; console.log(JSON.stringify(await remote("http://127.0.0.1:8082", "/run", {profile:"smoke"})));'
railway ssh --project <project-id> --environment <environment-id> --service benchmark-runner -- node --input-type=module -e 'import {remote} from "./benchmark/http.mjs"; console.log(JSON.stringify(await remote("http://127.0.0.1:8082", "/status")));'
railway service files --project <project-id> --environment <environment-id> --service benchmark-runner download /results/<run-id> ./benchmark/results/<run-id> --json
```

Change `smoke` to `comparison` after the smoke receipt reports completion and acceptance.
The SSH process reads the control token from the runner environment; keep it out of commands and output.
The controller permits one active job. It has a one-hour watchdog.
After an interrupted job, stop the target through its authenticated `/stop` route or stop its deployment before restarting a batch.

## Profiles and evidence

- [Smoke profile](profiles/smoke.json): checks all S3 implementations and embedded baselines.
- [Comparison profile](profiles/comparison.json): three 20-second S3 repetitions with 1 MiB files and four workers.
- [Storage regression profile](profiles/regression.json): repeats the Stow cases after a storage change using the same S3 windows.
- `run.json` records the overall status and embedded command.
- `embedded.txt` contains native Go throughput and allocation results.
- `s3/report.md`, `s3/summary.json` and `s3/suite.json` describe the S3 comparison.
- Raw scenario JSON files include latency, queue, scheduler, CPU, disk and resource samples.

After downloading a complete job, run `node benchmark/analyze.mjs <job-directory>`.
It validates case counts, integrity, failures, disk reserve and matching build identities,
then writes `analysis.json` with native medians, allocations and per-run resource checks.
This analysis is small and does not build binaries or images. The current
competitor and website-preview batch runs analysis and all tests on Railway.

The target is restarted for each repetition.
Readiness requires a successful authenticated S3 list-buckets request, including storage and credential initialization.
Target order rotates across repetitions.
Compilers and dependencies are installed during image build, outside measurement windows.
Embedded benchmarks run before S3 load, on the runner's ephemeral filesystem.
S3 filesystem benchmarks use the target's persistent volume.
Those storage classes and CPU ceilings differ; compare implementations within each lane.

## Agent startup and first write

`agent.txt` records fresh embedded stores inside a running Go host. Each sample
uses an empty prepared directory. Startup includes opening the store and its
namespace/schema; first write includes that startup and the first PUT. Payload
generation, directory provisioning, read verification and cleanup are outside
the measured span. Both 4 KiB and 1 MiB writes are SHA-256 and length verified.
The Go benchmark's calibration sample is omitted from the raw latency samples.
The comparison profile keeps 20 samples per implementation and payload size.

`node-agent.json` records fresh Node processes using the public Stow WASM entry
point, owned Node byte buffers and built-in Node SQLite. The parent clock starts
before process spawn, ends startup on the ready message and first write on its
acknowledgement. It includes Node startup, payload preparation, selected package
imports, WASM loading/compilation where used, and namespace/schema setup. Read
verification and cleanup follow the write acknowledgement. Process peak RSS is
recorded through verification. The comparison keeps ten processes per case and
rotates implementation order. These Node process measurements have their own
chart; Go instance startup and S3 server startup use different boundaries.

Each S3 case starts a fresh server and empty data directory. Startup ends after
an authenticated list-buckets succeeds, with 10 ms readiness polls. First-write
time extends that same clock through bucket creation and a 4 KiB PUT over
loopback on the target. The bootstrap object is verified and removed before
load begins. These measurements include server process startup, with images and
binaries already present; image pulls, deployments, client-host startup and cold
OS caches are unmeasured. Embedded instance startup and server process startup
belong in separate charts.

SQLite uses [go-sqlite3 1.14.52](https://github.com/mattn/go-sqlite3/releases/tag/v1.14.52),
four pooled connections and an adapter lock for consistent owned reads and
serialized mutations. Disk uses WAL and `synchronous=FULL`; memory uses a shared
in-memory database. This measures blob operations, with SQL querying unmeasured.
Go heap allocation counters exclude SQLite's C allocations and do not measure
total or retained memory. Node and S3 process RSS have separate charts.
Native Stow writes include its required ETag hashing, metadata and admission
work. Raw adapters do not reproduce those features; these are product/path
cost comparisons, not measurements of identical guarantees or algorithms.
RustFS uses the [1.0.1 musl package](https://github.com/rustfs/rustfs/releases/tag/1.0.1),
verified against the release SHA-256, with one drive, strict durability, console
disabled and update checks disabled. Distributed recovery and replication are
unmeasured for the S3 products.

Container metadata records host CPU information and cgroup limits separately.
Inspect CPU throttling and generator saturation before accepting throughput.
Host CPU counts and RAM do not describe a container's usable allocation.
Sampling adds overhead and can miss short RSS peaks.
There is no automatic cloud-hardware stability guarantee.

Exit zero means the commands completed; inspect `s3.acceptable` and each run's counters.
Dropped arrivals disqualify a zero-loss claim even when data verification passes.
These profiles are exploratory. Use the [qualification protocol](../docs/performance-testing.md)
before advertising sustained capacity.

Stop both task-owned deployments after exporting results.
The IaC definitions and result volume remain available for the next run.
Do not delete volumes containing evidence during routine cleanup.

## Verification

The Railway Docker build runs the embedded Go race tests, one-iteration native cases
at CPU settings 1 and 2, and the four control/configuration tests before deployment.
The smoke profile then exercises every deployed implementation.
No local Docker or Go build is needed for this workflow.
The small control tests can also run locally:

```sh
npm test --prefix benchmark
```

For full repository validation without a local Go or Docker build, package the
verification image separately and deploy it to the runner:

```sh
node benchmark/pack.mjs benchmark/verify-context --full --verify
railway up benchmark/verify-context --path-as-root --no-gitignore --project <project-id> --environment benchmark --service benchmark-runner --detach
```

This image regenerates protobuf source, builds the SDK WASM, runs `make standards`
and all four `make test-conformance` lanes, plus Node and WASM tests, in the Railway
build. Tests run without root privileges. The [Debian hostname resolver](https://manpages.debian.org/bookworm/libnss-myhostname/nss-myhostname.8.en.html)
resolves the addressing tests' localhost names; native Go uses the cgo resolver. Ratchet
checks use the actual parent baseline files from the source checkout, captured
in temporary cloud Git history. The uploaded source contains no local Git history.
After observing deployment `SUCCESS`, download `/app/verification` using
`railway service files`. Inspect its receipt, log and generated-file manifest;
copy the rebuilt `stow-runtime.wasm` into `packages/stow-s3/dist` when source
changes require it. Stop the runner after export. The verification image exposes
health and evidence but does not contain the benchmark executables; redeploy the
regular benchmark image before running more load.
Use `--no-gitignore` only with these sanitized contexts. Inherited ignore rules
can omit tracked historical plans from a full verification upload; the verifier
checks the source-payload hash before changing files or running gates.

## Product comparison scope

Agent-storage product charts compare the measured S3-shaped stores: Stow,
MinIO, S3Proxy and RustFS. Filesystem and memory results are separate. Startup
and first-write charts use fresh S3 server processes with the same timing
boundaries. SQLite, Afero, raw files and Go/Node buffers remain internal
engineering baselines; they are not equivalent S3 blob-store competitors.

`publish.mjs` exports only S3 peer results for the website. The full diagnostic
batch remains in the evidence directory. To build and verify a revised private
preview without rerunning unchanged storage benchmarks, package with
`node benchmark/pack.mjs benchmark/verify-context --full --site` and upload that
sanitized context to the Railway benchmark runner. This image derives the chart
dataset from the completed comparison, checks the site and documentation links,
and exercises all chart controls in Chromium on Railway. It does not publish
the website.

## Paired write-preparation comparison

Package the controlled image with `node benchmark/pack.mjs benchmark/context --controlled`.
Deploy that same context to both services and verify their submitted deployment IDs.
The image contains two Stow server binaries and two native benchmark binaries.
Other comparison profiles require the regular image.

The preceding implementation is reconstructed using
[the recorded patch](variants/before-write-preparation.patch) and [file hashes](variants/write-preparation.json).
The build rejects source drift before applying the patch in its separate temporary tree.
Both variants pass race tests and native workload smoke checks.
The preceding variant excludes the new empty-reader and checksum-retry regressions;
the build separately confirms its expected checksum-retry failures.
This baseline retains the usage/recovery consolidation and earlier streaming changes.
It cannot attribute the historical streaming-build HTTP PUT regression.

After authenticated S3 smoke checks, start the runner with `profile: "controlled"`.
The [controlled profile](profiles/controlled.json) measures six paired rounds of
memory/filesystem PUT, GET and quarantine with 1 MiB files and four clients.
Matching backends run adjacent windows; variant order reverses on alternate rounds.
Native comparisons also alternate order.
Managed saves use 64 keyed replacements per sample, keeping retained receipts bounded.
Their final payload is verified after timing.

Analyze completed results on Railway with
`node benchmark/analyze-controlled.mjs /results/<run-id>`.
The analyzer verifies both binary identities, configured resource limits, pairing,
successful samples, payload verification, disk headroom and CPU observations.
It records conditional Student-t intervals for mean log throughput ratios.
These intervals describe the six rounds on one worker; sustained capacity remains unqualified.
The SDK and private website dataset remain unchanged by this comparison.
