# Stow: product assessment and independent validation

> **Planning disposition, 2026-09-29:** Dated assessment evidence. Implementation status and work ordering live in the canonical plan; earlier product recommendations are subject to ADR 0014. See [the plan](plan.md) and [disposition register](planning-index.md).

> **Product decision, 2026-09-29:** [The canonical storage plan](plan.md) now governs. Stow owns portable working storage; callers own execution, agent turns and sandboxing. The experiments and earlier recommendations below retain their dated context. Runner/executor requirements are deferred; agent continuity will be evaluated through a thin storage integration, not a separate Stow execution product.

Assessment date: 2026-09-29. Tested revision: `5485a3640dbeeb368324d7e2a223bc17d7d23fc5`, fetched from `origin/main`.

## Recommendation

**Follow-up after discussion:** Treat agent continuity as a second first-class hypothesis alongside S3 fixtures. A subsequent [continuity pilot](agent-continuity-pilot-2026-09-29.md) exercised offline inputs, partial work, checkpoint transfer, adoption, credential-free continuation and output diff using two deterministic workers. It succeeded with the documented archive-reference workaround. Fresh-model continuation and cross-host execution remain untested. The testing-focused recommendation below should not be read as abandoning the agent opportunity.

Continue with the existing S3 product. Its most credible initial customer is a developer maintaining an application or library that needs disposable S3 storage in tests and CI. Stow already has substantial working behavior: ordinary SDK access, owned session lifecycle, native packaging, persistence, authenticated requests, and an offline cache.

The next product hypothesis to validate is **portable S3 fixtures**: give a test known object data, run the application with its usual SDK, retain the resulting state when it fails, and reproduce that failure on another machine without live cloud credentials. This connects the existing storage, cache, and checkpoint work to a concrete recurring job. It is a hypothesis, not demonstrated product-market fit or an already complete feature.

The immediate work is closing compatibility and delivery gaps. The fetched README leads with agents and says the local S3 part is not the interesting part. This assessment reaches a different conclusion: the working S3 surface is the clearest place to earn adoption. A broader execution product would require its own customer evidence and an actual process isolation boundary.

## Scope and method

The primary checkout was behind origin and contained ongoing documentation work. It was preserved. Tests ran in an app-managed worktree at `/Users/ladmin/.codex/worktrees/stow-product-assessment/stow`. This report adds documentation and evidence only; the findings have not been fixed.

The assessment combined source and contract review, repository gates, independent native/AWS CLI exercises, fresh local package installs, real browser execution, and current first-party competitor research. Historical findings in `docs/assessment-2026-09-27.md` are explicitly historical and include subsequent repairs; they are not restated as current defects.

Host: macOS 14.5, arm64; pinned Go 1.24.13; Node 25.9.0; repository Python suite on Python 3.10.11; fresh wheel consumer on Python 3.12; installed AWS CLI 2.27.1. Test data was synthetic. Native upstream/cache tests used two local Stow processes, not AWS or R2.

## What is actually built

| Surface | Current behavior | Product boundary |
| --- | --- | --- |
| Native S3 endpoint | Local memory/filesystem storage, SigV4, common object and bucket operations, conditions, ranges, multipart and presigning | An explicitly limited S3 implementation; no versioning, IAM policy system, lifecycle, replication or object lock |
| Go runtime | Embedded storage interface; memory/filesystem implementations and workspace APIs | Useful library, not an execution engine |
| TypeScript/Python sessions | Start a native child process, return endpoint/client/credentials, own cleanup | Natural integration point for test fixtures; wrappers and binary delivery both matter |
| Run-through mode | Read cache, local overrides, explicit live-write consent, durable propagation outbox | Offline operation depends on retained cached bytes; cache TTL/eviction is not archival retention |
| Workspace | Real directory, declared input preparation, immutable checkpoints, diff/restore, archives, registry and adoption | File state; not a full running process or VM checkpoint |
| Workspace storage API | Files and objects can address the same bytes through the embedded API | `serve --backend workspace` is explicitly unavailable from the CLI |
| Browser/Node WASM | Go `js/wasm` storage runtime; browser wrapper adds IndexedDB persistence | Not a generic WASI sandbox; execution on arbitrary edge hosts is unverified |
| Task model | Task/session/attempt/artifact design recorded in ADRs | Does not supply an agent runner or enforce agent CPU/memory/network limits |

There are two different filesystem stories. The ordinary persistent S3 backend uses object records. The workspace backend exposes ordinary files. They should not be described as one interchangeable directory format. Likewise, a workspace checkpoint is not automatically a transferable S3 fixture preserving all object properties.

Scoped API credentials and storage quotas are useful. They do not confine an arbitrary process that can also use the host filesystem, environment or network. Any isolation claim must state the threat model and enforcement mechanism.

## Test results

### Repository gates

| Check | Observed result |
| --- | --- |
| `make test-all` | Passed: Go tests, race checks, four local conformance configurations, 90 Node tests, 69 Python tests, one real WASM integration test |
| Live-provider conformance | Skipped: no configured live endpoint, credentials, bucket or disposable-write consent |
| `make standards` | Failed at file-size gate: `scripts/check-coverage.test.mjs` is 503 lines |
| Gates before that failure | Formatting, vet, race and Go quality checks passed |
| Remaining gates run individually | Coverage, version, install surface, documentation commands, ADR index, script tests, TypeScript quality and generated output all passed |

The coverage gate reported 71.31% (6,825/9,571 statements). Generated package output was reproducible. Passing gates are evidence of the covered contracts, not complete S3 compatibility. The matrix intentionally skips some cases for unsuitable backend configurations; not every named test ran in every configuration.

### Independent behavior checks

Using the real native binary and installed AWS CLI:

- Default upload failed with unsupported `crc64nvme`; subsequent uploads explicitly selected CRC32.
- With that override: byte-exact put/get, range reads, valid presigned GET, rejection of altered signatures and unsigned requests, and rejection of an incorrect `If-Match` all passed.
- High-level `aws s3 cp` uploaded and downloaded a 12 MiB object through multipart successfully.
- Filesystem restart preserved bytes.
- Explicit prewarming populated two keys. Read-through worked. A local override left upstream bytes unchanged by default.
- With upstream stopped, an offline restart served both an untouched warmed object and the local override; an uncached key returned `NoSuchKey`.
- Workspace preparation, checkpoint and diff passed. Relocation of a handoff exposed the archive-path defect below. Adoption worked after explicitly repairing the reference and preserved the file bytes.

### Installation and browser checks

A locally built macOS arm64 Python wheel was installed into a fresh environment outside the repository. It found its bundled executable without a source override, created a session and bucket, round-tripped data with boto3, and removed its owned process/directory on close.

Fresh local npm tarballs for the client and platform binary were installed in a separate consumer. Twenty successive sessions each uploaded and fetched a 1 MiB object and cleaned their data directory. The probe checked returned length and first byte; the separate AWS CLI probe performed full byte equality/digest checks.

| Node timing, 20 successive sessions | Milliseconds |
| --- | ---: |
| Readiness median | 23.69 |
| Readiness p95, nearest-rank | 29.11 |
| Readiness plus first 1 MiB upload, median | 31.58 |
| Readiness plus first upload, p95 | 36.89 |
| First observed readiness / maximum | 386.64 |

The first process was substantially slower. These are local repeated-process observations with cached artifacts, not a cold-install guarantee, Linux CI result or comparison against Docker/competitors.

An actual browser page loaded the built Go WASM and the real IndexedDB adapter. It verified durable commit, rejection of a second live opener of the namespace, close/reopen retention of bytes/content type/metadata, and persistence across a full page reload. This was one browser engine and a second opener in the same page, not a cross-browser or cross-tab stress test.

## Current findings, in recommended order

### 1. Default AWS CLI checksum compatibility breaks the first upload

Reproduction: create a bucket, then use `aws s3api put-object` without specifying a checksum algorithm. Result: `InvalidArgument: unsupported checksum algorithm "crc64nvme"`. `--checksum-algorithm CRC32` allowed the remainder of the exercise.

AWS documents CRC64NVME as the CLI v2 default. This is therefore a mainstream client path, not merely an optional advanced feature. Decide whether to support it or explicitly configure and document the supported client path; broad ordinary-CLI compatibility requires addressing the default. [AWS CLI S3 FAQ](https://docs.aws.amazon.com/cli/latest/topic/s3-faq.html)

### 2. HTTP user-metadata keys change case

Put metadata `purpose=assessment`; head/get through AWS CLI and a fresh boto3 consumer returns `Purpose`. The value and content type survive. AWS specifies lowercase user-metadata keys, so code indexing `metadata["purpose"]` can fail. This is a compatibility defect, not observed byte loss. Inspect `internal/s3api/validators.go` header extraction/emission. [AWS metadata documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingMetadata.html)

### 3. Generated handoff documents retain sender absolute archive paths

Create a handoff with `--archive /sender/transfer/snapshot.tar.gz --output handoff.json`; move the JSON and archive together and remove the original location. Adoption still opens the sender path and fails. Change `archive.path` explicitly to `snapshot.tar.gz`; adoption into another registry succeeds.

The supported reader already resolves relative paths beside the handoff. The writer should produce a relocatable reference when publishing a file bundle. Relevant sources: `cmd/stow-s3/workspace_handoff.go` (`publishHandoffArchive`, path resolution) and `workspace_archive.go` (absolute output path). This was a same-machine relocation test, not a successful cross-machine handoff.

### 4. Bucket scope is available but the default setup does not establish it

In run-through mode without `STOW_BUCKET`, a prewarmed local namespace initially rejects an upstream-only bucket. The local client can create that bucket locally, then read its synthetic upstream object. Namespace bootstrap is therefore not a fixed allowlist.

Setting `STOW_BUCKET=fixtures` successfully blocked the other-bucket read even after local creation. Preserve this distinction: the restriction works when configured. A scoped-access promise should require explicit configuration and corresponding acceptance tests. Selected prewarm keys also do not become an online key-level allowlist. Relevant source: `internal/runthrough/adapter.go`, `upstreamEnabled`.

The README claim that an agent cannot reach anything it was not given is broader than these defaults establish. Credential separation alone also does not provide host process isolation.

### 5. Public delivery does not yet match the tested source product

The online install-surface check returned anonymous HTTP 401 from the configured GitHub npm registry, 404 for `stow-s3` on PyPI, and 200 from the Go proxy. The gate passes because the manifest explicitly declares npm/PyPI unpublished. This is a delivery gap, not a broken gate.

The Go proxy advertised v0.2.0 while GitHub's latest-release endpoint returned v0.1.0. The v0.2.0 Go tag predates the current workspace layer. The successful wheel/tarball tests validate locally built artifacts, not public installation of this revision. Source references: [Go proxy latest](https://proxy.golang.org/github.com/chester-hill-solutions/stow-s3/@latest), [GitHub latest release API](https://api.github.com/repos/chester-hill-solutions/stow-s3/releases/latest), [PyPI package API](https://pypi.org/pypi/stow-s3/json). Registry observations are point-in-time and may change.

### 6. Workspace-to-S3 composition remains unfinished

`stow-s3 serve --backend workspace` deliberately refuses selection and directs users to the embedded API. The ordinary S3 server works; the missing composition prevents promising a ready directory-plus-endpoint workflow through this CLI today. This is acknowledged unfinished delivery rather than a new storage regression.

### 7. The aggregate standards gate is red

The 503-line script test exceeds the file-size rule. Fix the source organization or deliberately revise the applicable policy. Do not advertise a clean `make standards` result based on the other checks passing.

## Market landscape

These are current first-party descriptions, accessed for this assessment. Competitors were researched, not benchmarked or security-audited. Absence from a README is not proof a capability is absent.

| Alternative | What already exists | Implication for Stow |
| --- | --- | --- |
| [local-s3](https://github.com/shyim/local-s3) | Small Go S3 server, filesystem, SigV4, bucket permissions, web UI | “Single binary for local S3” has direct competition |
| [fauxqs](https://github.com/kibertoad/fauxqs) | TypeScript SNS/SQS/S3 emulator; npm/embedded use, memory or SQLite, initialization and reset workflows | JavaScript teams can obtain easy local fixtures plus adjacent AWS services |
| [Moto server](https://docs.getmoto.org/en/latest/docs/server_mode.html) | Python package and threaded HTTP server usable with other SDKs | Docker-free and Python-native testing are established alternatives |
| [GoFakeS3](https://github.com/johannesboyne/gofakes3) | Embedded Go S3 test server and storage backends | Embedded Go by itself is not a differentiator |
| [Adobe S3Mock](https://github.com/adobe/S3Mock/blob/main/README.md) | JVM/library/Docker paths; broader S3 features including versioning | Strong option for compatibility-led workloads; docs say presigned requests are accepted without signature/expiry/verb validation, a concrete place Stow's checked behavior can matter |
| [LocalStack](https://docs.localstack.cloud/aws/developer-tools/snapshots/) | Many AWS services and state snapshot workflows | Portable state is already offered; Stow must win on a specific simpler workflow |
| [rclone serve s3](https://rclone.org/commands/rclone_serve_s3/) | Experimental S3 gateway over local directories/remotes with VFS caching | Directory-to-S3 and remote caching already exist |
| [S3Proxy](https://github.com/gaul/s3proxy) | Java gateway over filesystem, memory and cloud backends, with embedding/middleware | Backend flexibility is not empty market space |
| [Versity Gateway](https://github.com/versity/versitygw/blob/main/README.md) | Native S3/POSIX gateway and multiple backends | Ordinary-files-plus-S3 has a serious established comparator |
| [SeaweedFS](https://github.com/seaweedfs/seaweedfs) | Native single-command local S3 startup within a broader storage system | Production-oriented tools can also have simple local entry points |
| [AgentFS](https://github.com/tursodatabase/agentfs) | SQLite-backed agent files/state/tool records, portable state and experimental sandbox tooling | Agent filesystem portability is already contested and requires a sharper promise |

### There is visible switching pressure, with important limits

LocalStack's March 2026 transition requires authentication for the unified image, including CI. That can introduce friction for projects wanting self-contained test infrastructure. It does not mean LocalStack is unusable or every user wants a replacement. [LocalStack announcement](https://blog.localstack.cloud/localstack-single-image-next-steps/)

A concrete public example is Gardener's etcd-backup-restore project: its issue reports LocalStack changes breaking end-to-end tests and seeks a replacement, subsequently choosing Adobe S3Mock. Its required API list includes versioning, object lock configuration and tagging. Stow does not currently meet that full list. This is evidence of the problem category, and simultaneously a warning against assuming every displaced LocalStack user is a fit. [Project issue and linked resolution](https://github.com/gardener/etcd-backup-restore/issues/1010)

S3rver's repository was archived in September 2025. MinIO's community repository is archived and says it is no longer maintained, while directing users to AIStor Free and Enterprise. This creates reasons to reassess dependencies; it does not establish unmet demand for Stow or imply all MinIO products disappeared. [S3rver](https://github.com/jamhall/s3rver), [MinIO repository](https://github.com/minio/minio)

### Where a gap might remain

The plausible opportunity is the complete small workflow:

1. Install through the language ecosystem already in use.
2. Open a fresh S3 session for a test or application task.
3. Seed deterministic objects, including required metadata, without a shared cloud account.
4. Run the ordinary application SDK against it.
5. Keep a failure fixture or a selected working copy and inspect what changed.
6. Reopen that fixture elsewhere and reproduce the behavior.
7. Clean up reliably when the caller is finished.

Stow has working ingredients for much of this, but storage snapshots, workspace archives and upstream caches are not yet a single proven fixture format/lifecycle. The differentiator would be how reliably and simply this whole job is delivered across languages. No individual item in the list establishes uniqueness.

| Candidate audience | Current fit | What would disqualify it |
| --- | --- | --- |
| Web applications testing uploads, downloads and presigned URLs | Strongest immediate pilot | Needs bucket policy enforcement, notifications or precise unsupported S3 semantics |
| SDK/library maintainers testing common S3 operations | Good pilot | Compatibility matrix materially exceeds Stow's subset |
| Data-processing teams reproducing failures from selected objects | Promising next hypothesis | Data volume, retention, metadata or provenance cannot be preserved economically |
| Browser/offline application developers | Working technical capability; demand unknown | Their storage needs are already simpler with direct IndexedDB/OPFS |
| Agent platform builders | Potential consumer of storage/workspaces | Requires Stow itself to enforce hostile-code isolation |
| Production object-storage operators | Weak current fit | Needs replication, high availability, IAM and broad S3 administration |

## Proposed next steps

### Step 1: make the existing promise dependable

Repair default checksum handling, metadata key casing and relocatable handoff output. Make scoped upstream configuration and security language precise. Restore a green standards gate. Add permanent regressions for these observed boundary failures during implementation.

Release and test one coherent version through the intended public install channels. Demonstrate fresh installs on supported macOS and Linux targets with no repository override. Keep unpublished or unsupported platforms explicit. The clean local packages here are a useful precursor, not that acceptance result.

### Step 2: choose one entry experience

Lead with a runnable local/CI S3 example: open a session, use the existing application client, close it. Show the supported API subset and actual authentication behavior. Make it possible for a developer to reach a successful upload without first learning the workspace registry, task model or mirror-write policy.

Treat the next demonstration as one complete retained-fixture workflow. Specify which object properties survive, how the bundle is addressed and moved, and whether reopening needs any upstream credentials. Reuse existing snapshot/archive mechanisms where their semantics fit; do not silently treat plain files as a complete S3 fixture.

### Step 3: validate repeat use with three independent applications

Suggested pilots, not recruited customers:

- An application with browser uploads/presigned URLs.
- A data processor that reproduces a failure from selected object inputs.
- A Go, TypeScript or Python library with existing S3 integration tests.

Record time to first passing test, setup interventions, required API gaps, parallel-session reliability, CI-to-laptop reproduction, and whether the team keeps using Stow without assistance. Ask what it replaces and what failure is costly enough to motivate switching. A useful decision threshold is successful repeated use in at least two independent applications, including one moved failure fixture, before expanding the task-runner surface. This is a proposed learning threshold, not a statistical PMF measure.

### Commercial question remains open

Local S3 emulation has many free alternatives. The research supports a developer adoption opportunity more strongly than willingness to pay. Potential paid value would need a recurring team problem—shared fixture distribution, retention, access controls, reliable compatibility/support—not simply charging for another local endpoint. Interview and observe those workflows before choosing a hosted control plane or pricing model. No customer interviews, revenue evidence or willingness-to-pay tests were performed here.

## Limits of the conclusion

This assessment did not rerun live AWS/R2 conformance, test Windows/Linux execution, validate arbitrary edge hosts, benchmark competing products, measure large datasets or sustained concurrent workloads, or audit containment against hostile code. Repository records of previous provider testing are historical evidence, not a substitute for a fresh run. The market analysis is grounded in product documentation and one public switching example; it cannot establish market size or product-market fit.

## Evidence and reproduction

Preserved beside this report in `assessment-evidence/2026-09-29/`:

- `native-probe.py` and `native-results.json`: independent local native/cache/workspace exercise. The recorded boolean on the bucket-expansion observation means the behavior was observed, not that it is a security pass.
- `node-probe.mjs` and `node-results.json`: installed-package lifecycle/timing observations.
- `browser-probe.html`, `browser-results.txt`, `browser-proof.png`: real WASM/IndexedDB page and reload evidence. Serve with the tested package's built `dist/` beside the page.
- `test-all.log`, `standards.log`, `remaining-gates.log`: complete gate output.
- `README.md`: setup notes and interpretation limits.

The assessment harnesses are local review artifacts, not additions to the project's permanent test suite. Temporary test data and build consumers remain under `/private/tmp/stow-assessment-*` and `/private/tmp/stow-product-probe-*` for follow-up; no production bucket was used.
