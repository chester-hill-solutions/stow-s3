# Pre-code discovery and engineering handover

> Historical pre-code record. Implementation followed on the same date; see
> [current evidence](implementation-2026-09-29.md) and [the canonical plan](plan.md).
> The findings and "not performed" statements below describe the discovery pass.

## 1. Plan validation summary

**Verdict: Adjust before implementation.** Source discovery against Stow `5485a36`
supports the storage-first direction, but exposes prerequisites hidden by the old
feature labels. This report expands [canonical S0–S3](plan.md), not a new backlog.
The [storage design](storage-portability-design.md) and [MCP detail](mcp-storage-integration-plan.md)
contain the proposed contracts. Designs here are recommendations for implementation,
not shipped guarantees or newly accepted ADRs.

Evidence levels:

- **Reproduced earlier:** checksum failure, metadata mismatch and relocated-handoff
  failure in the September 29 assessment; its logs remain the execution evidence.
- **Inspected now:** source paths, call ordering, schemas, workflow configuration,
  OpenCode v2.0.16 source and official protocol/distribution documentation.
- **Not yet reproduced:** newly identified concurrency, crash/cancellation, quota
  accounting and transfer edge cases. These need focused regression probes first.
- **Not performed:** product implementation, model calls, package publishing,
  credential inspection, provider mutations or new full implementation test runs.

The installed OpenCode binary reports `v2.0.16` using isolated temporary XDG paths.
The matching public tag resolves to `3a103fe0aff726a4edc7492f03f7b88195d9e4c9`.
Pinned source references and local download hashes are recorded in
[the source manifest](assessment-evidence/2026-09-29/pre-code-source-manifest.json).
The lifecycle findings are static analysis, not a successful agent integration.

Documentation verification after this pass: ADR index/digests (14 decisions),
command-surface checks (12 documents), 315 local link targets, source-manifest JSON
parsing and `git diff --check` passed. Downloaded public source matched all 20
pinned Git blob identities. Full implementation suites were not rerun.

## 2. What the plan already covers well

- One Go storage implementation, existing capture/archive/delta machinery and thin
  callers provide the right reuse boundary.
- The chosen OpenCode integration, separate workspace and explicit save failures
  give the first pilot a concrete shape.
- Ordinary saved context avoids introducing an execution database or transcript
  format before a consumer needs it.
- Published consumers, provider evidence and repeat use are distinct acceptance
  obligations; existing local passes are retained with their limitations.

## 3. Gaps, conflicts, or redundancies to fix

### Storage capture and publication

| Finding from source | Consequence | Destination |
| --- | --- | --- |
| Handle capture uses `checkpointMu`; external capture uses `AcquireCapture`. | Mixed calls do not share exclusion around retention checks. | S0-7: common gate. |
| `AcquireCapture` converts every `tryLock` error into `Held=false`; caller does not check it. | Unexpected I/O failure can silently disable serialization. | S0-7: distinguish contention/unsupported/error; fail reliable profile when lock unavailable. |
| Publication writes manifest and renames staging without syncing payloads/manifest/directories. | Atomic visibility alone does not establish power-loss durability. | S0-7: specify commit and durability boundary before acknowledgements. |
| Cancellation does not flow through every scan/read or immediately before publication. | A timed-out caller may still have a published checkpoint. | S0-7/S3-2: cancellation phases plus reconciliation. |
| Collection/checkpoint cleanup does not share capture exclusion. | External capture can race destruction or receipt removal. | S0-7/S1-4: shared mutation ordering and liveness. |
| Prepare counts entries then registers; imports/deltas bypass capture retention admission. | A hard standing cap needs a registry transaction across publication paths. | S1-4. |
| Retention skips `.checkpoint-*` staging but not `.import-*`; registry enumeration skips invalid entries. | Incomplete/corrupt state can distort accounting. | S1-4: published-state enumeration and explicit corrupt-state refusal. |
| Prepare opens runtime before seeding; external file writes bypass runtime accounting. | Sharing a runtime is necessary but insufficient for correct later quota checks. | S1-1: reconcile usage at defined boundaries. |

These are code-path findings. Concurrent reproducers and fault-injection checks
must establish their actual behavior before changes are marked as fixes.

### Portable state

The root `.stow` exclusion drops escaped-key and secondary-bucket **payloads** as
well as metadata. File checkpoints remain useful, but cannot be advertised as
complete object snapshots. A metadata-only extension would not repair this.

Other required decisions: metadata-only mutation detection; invalidating stale
checksums after host edits; permission bits currently masked by `0755`; Git
provenance returned from prepare but not persisted; and imported checkpoints whose
source workspace identity cannot serve as a new destination workspace's parent.
The storage design supplies proposed resolutions and explicit format boundaries.

### OpenCode lifecycle

The installed v2 source differs from the public `@opencode-ai/sdk` examples read
earlier. Its prompt endpoint acknowledges durable input admission; a separate
experimental wait endpoint waits for the local execution coordinator to settle.
Wait does not reserve the next turn or prevent a different client from admitting
work. The v2 plugin session-hook interface has no documented public settled/save
barrier. The recommended pilot is therefore a caller-controlled, version-pinned
integration with exclusive prompt admission. See the MCP plan's pinned sources.

An OpenCode process merely using the prepared directory also does **not** hold a
Stow liveness claim: the prepare CLI closes its handle before returning. The
integration must own a storage lifecycle claim for the duration of the run, or use
an explicitly no-collection profile until that claim is available.

### Release/distribution

The workflow targets GitHub's npm registry. Public packages there still require
an access token to install, so changing visibility cannot satisfy account-free
installation. The recommended public destination is npmjs for the main package and
all platform carriers; scope ownership/publisher setup remains an external
prerequisite. [GitHub's npm registry documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry)

Current release checks run publication availability before publishing, check broad
existence rather than every candidate artifact/version, and pass one provider
configuration to a workflow whose labels combine R2/custom. They cannot prove the
current plan's first-publication or three-provider acceptance without restructuring.

## 4. Existing code/product references to reuse or extend

| Area | Source and reuse seam |
| --- | --- |
| Capture | [checkpoint.go](../pkg/stow/checkpoint.go), [external capture](../pkg/stow/checkpoint_external.go), [capture core](../pkg/stow/checkpoint_capture_core.go), [capture lock](../internal/storage/workspace/capture_lock.go) |
| Atomic persistence | [atomicfile](../internal/atomicfile), `publishCheckpoint`; extend directory publication, do not build a second checkpoint store. |
| Workspace facade | [workspace.go](../pkg/stow/workspace.go), [runtime adapter](../internal/runtime/adapter.go), [S3 server](../internal/s3api/server.go); adapt `w.Runtime.inner`, borrow its lifetime. |
| Object layout | [layout](../internal/storage/workspace/layout.go), [manifest](../internal/storage/workspace/manifest.go), [resolution](../internal/storage/workspace/resolve.go); logical snapshot reader belongs beside these. |
| Transport | [archive](../pkg/stow/checkpoint_archive.go), [import](../pkg/stow/checkpoint_import.go), [delta apply](../pkg/stow/delta_apply.go), [handoff CLI](../cmd/stow-s3/workspace_handoff.go) |
| Prepare/policy | [prepare](../pkg/stow/prepare.go), [Git prepare](../pkg/stow/prepare_git.go), [workspace limit](../pkg/stow/workspace_limit.go), [registry](../internal/storage/workspace/registry.go) |
| Metadata/checksum | [HTTP validators](../internal/s3api/validators.go), [request parsing](../internal/s3api/request.go), [checksum core](../internal/storage/checksum.go), [upstream adapter](../internal/runthrough/upstream.go) |
| Release | [release workflow](../.github/workflows/release.yml), [live workflow](../.github/workflows/live.yml), [install check](../scripts/check-install-surface.mjs), [publication helper](../scripts/publish-if-absent.mjs), [version check](../scripts/check-version.mjs) |

## 5. API impact and validation notes

### Repair slices that preserve existing entry points

**Handoff.** The producer writes an absolute archive path; the reader already
resolves a relative one. For file output, emit a reference relative to the handoff
document. Keep old absolute references readable. Without a document location,
stdout output remains explicitly local unless the caller supplies a portable base.
Refuse a portability claim across incompatible path volumes. The positive test
currently rewrites the generated reference; replace that workaround with an actual
move of unedited producer output. Validate workspace/checkpoint membership before
both archive and reference-only export paths.

**Metadata.** Current HTTP extraction stores full prefixed header names; SDK
upstream responses provide bare names, while upstream writes pass the internal map
straight into SDK metadata. Response `Header.Set` also canonicalizes casing.
Introduce explicit HTTP/internal/SDK conversions. Preserve current persisted/API
maps through a compatibility normalizer in the repair slice; the new portable
format can define lowercase bare user keys unambiguously. Detect conflicting
duplicate forms; do not silently choose one. Cover PUT/HEAD/GET, copy/replace,
multipart, reopen and upstream→cache→offline reads through real SDK metadata maps.

**Checksums.** CRC64NVME is absent from the shared compute/header/upstream mappings.
Start with verified single-object request shapes and independent vectors, including
bad checksums and partial reads. Multipart checksum types require a separate
explicit acceptance slice; CRC64NVME represents a full-object checksum and cannot
be treated as an arbitrary composite algorithm. [AWS checksum guidance](https://docs.aws.amazon.com/AmazonS3/latest/userguide/checking-object-integrity-upload.html)
Keep the exact implementation/dependency choice contingent on variant/vector review.

**Request limits.** Enforcement already exists; wire one validated option through
native configuration, ready capabilities and wrappers. Proposed semantics: absent
uses the existing default, positive sets a bound, zero/negative explicit overrides
are rejected; do not create an accidental unlimited request mode. Review signed
chunked/multipart body accounting before applying the threshold to encoded bytes.

**Endpoint policy.** Proposed default: HTTPS, plus HTTP only for literal loopback
development endpoints; any broader HTTP access requires explicit configuration.
Validate before constructing a credential-bearing upstream client, reject URL
userinfo/fragments, and prevent redirects from forwarding credentials to an
unapproved origin. Preserve deterministic local upstream tests. This is a planned
compatibility change and needs contract/release-note updates.

### New bounded contracts

- A versioned portable object checkpoint; v1 remains an explicitly file-only format.
- Shared capture locking, typed errors and a capture request/resolve API. Request
  receipts are storage-operation metadata, not agent turns or execution attempts.
- Read-only scoped workspace descriptors; inspection must neither create a registry
  nor claim an already-live workspace.
- A shared typed handoff service extracted from CLI composition, with partial-outcome
  reporting. Initial export/adopt calls are not automatically retried.
- Standing registry policy and coordinated publication/deletion admission.

New format readers must refuse unsupported content without silently downgrading.
Existing APIs stay additive. Any changed default or on-disk schema needs explicit
contract/version documentation before its implementation is merged.

### Release contract changes

Separate preflight/artifact checks from exact-version post-publication consumer
checks. A first release cannot be required to exist before it is published. Inspect
all platform artifacts/wheels and run supported native consumers; cross-building a
wheel is not proof that it executes on its target.

Handle partial publication deliberately: distinguish missing version from auth/
network failure; compare existing immutable artifacts with candidate identities
before skipping; report missing ecosystems. Pin the PyPI publishing action to a
reviewed immutable revision. Reuse the version checker, select a new version and
keep existing tags immutable. Provider gates consume revision-bound results from
separate AWS/R2/custom runs rather than treating a configured endpoint as success.

## 6. Final adjusted plan, summarized

1. Repair standards and relocatable handoff; add focused regressions for newly found
   capture/lifecycle/admission gaps before relying on automatic saves.
2. Repair metadata conversions and checksum support through the existing core;
   define supported request limits, endpoint policy and compatibility scenarios.
3. Establish shared locking, publication/durability and request reconciliation.
4. Add the borrowed same-runtime facade and usage reconciliation, then portable
   object checkpoint/transport support and declared Git reconstruction profiles.
5. Complete registry policy and repeat-capture measurements; restructure publication
   gates and public registry delivery in parallel with storage work.
6. Build the local MCP adapter and caller-controlled OpenCode example on those
   contracts, then prove second-host continuation and failure handling.

No estimate is inferred from source-line counts. The largest uncertainty is the
portable state contract and writer coordination, not exposing tools over stdio.

## 7. Handover items for engineering, enumerated

Each slice maps to the existing canonical queue. “Verify” below describes future
implementation acceptance, not checks performed during this discovery pass.

| Slice | Canonical item / touchpoint | Change and acceptance | Dependency |
| --- | --- | --- | --- |
| P01 | S0-6 / coverage script tests | Split oversized test file without changing assertions or baselines; script discovery and standards pass. | None |
| P02 | S0-1 / handoff producer/tests | Relative file-output reference, early identity checks; unedited bundle moves and adopts from unrelated cwd. | None |
| P03 | S0-7 / capture/lifecycle locks | One capture gate, fail-closed lock errors, read-only ID validation; mixed capture/cleanup races have regressions. | None |
| P04 | S0-7 / publication | Cancellable scans/copies; payload/manifest/directory sync and explicit commit boundary; fault-injected interruption outcomes. | P03 |
| P05 | S3-2 / capture request API | Atomically co-published receipt, same-key replay and resolve, typed errors; lost reply and restart return original result. | P04; retention/cleanup contract |
| P06 | S0-1 / metadata adapters | One boundary conversion policy; real SDK maps agree after copy/multipart/cache/reopen. | None; precedes portable metadata |
| P07 | S0-1/S0-4 / checksum core | CRC64NVME vectors/single-object paths; explicitly scoped multipart follow-up and range behavior. | Recorded CLI request shape |
| P08 | S0-5 / config/server/wrappers | Effective request bound and transport policy with signed/multipart/redirect cases. | Contract decisions above |
| P09 | S1-1 / workspace facade | Borrow existing runtime, drain before releasing liveness; same quota state including seeded/host-written files. | P03; usage-reconciliation probe |
| P10 | S1-2 / snapshot reader/schema/diff | Logical file/object/bucket inventory, v2 dispatch, metadata-aware capture and comparison. | P03/P04/P06 |
| P11 | S1-2/S1-3 / transport/restore | Archive/import/restore preserve all selected object payloads; reject enhanced deltas until supported; Git profiles and destination lineage explicit. | P10/P02 |
| P12 | S1-4 / registry policy | Transactional prepare/import/delta/capture admission, protected deletion and correct staging accounting. | P03/P04; policy schema |
| P13 | S2-1–S2-3 / release | Public npm destination, preflight versus publication verification, immutable partial retries, provider receipts. | Account setup external; candidate requires storage gates |
| P14 | S3-1 / caller example | Pin OpenCode v2 API, exclusive admission plus idle/known-writer barrier and live workspace claim. | P09 storage lifetime; runtime spike before guarantee |
| P15 | S3-2 / MCP adapter | Scoped schemas/results over shared Go APIs and typed handoff; negotiation and restart checks. | P05/P11; SDK/toolchain selection |
| P16 | S3-3/S4-1 / end-to-end example | Fresh-context Linux continuation, failure/retry/cancellation/unknown outcome, Git/non-Git cases. | P09–P15; published supported artifacts |

Remaining uncertainty is explicit: OpenCode runtime behavior with the selected
profile; platform path guarantees; representative checkpoint deadline/lock cost;
MCP SDK dependency/toolchain validation; public publisher account configuration;
and provider credentials/evidence. These can be investigated independently and do
not authorize widening Stow into an executor.
