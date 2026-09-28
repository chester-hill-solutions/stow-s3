# Agent Workspace, Handoff, and Run-Through Plan

**Status:** Phases 0–4 are implemented locally; Phases 5–6 are partial. Reconciled with `main` at `155dff287e4b522b80acceacd12a945d2837b37b` and the uncommitted 2026-09-27 repair follow-up below.
**Date:** 2026-09-27
**Scope:** remediate production run-through composition; prepare isolated, pre-seeded agent workspaces; checkpoint and hand off their results.
**Relationship:** additive to the accepted decisions in ADRs 0005–0011 and `docs/workspace-contract.md`. This is the current execution order for these workstreams and supersedes the related backlog ordering in `docs/agent-dx-plan.md` §0.11/§10.1 and the phase ordering in `docs/agentic-dx-10-plan.md`. Those older plans remain product history and architecture context; they are not parallel implementation specifications.

**Sequencing against the deploy-anywhere work:** [`Deploy-Anywhere Plan`](deploy-anywhere-plan.md) is the current execution order for portability, multi-writer safety, and the read-through cache, and it runs in parallel with this one. Where the two touch the same code it is ordered first. Its Phase 1 (concurrent-write detection on propagation) and Phase 3 (incremental cache eviction) are P0 and precede this plan's Phase 5 item 3. The MCP stdio adapter is **pushed, not dropped**: an adapter built over propagation that silently overwrites another writer would be a faster route to that failure rather than a use of the product.

## 1. Plan validation summary

**Verdict: Direction validated; the initial resume-policy and production/test wiring gaps are resolved in the local follow-up.** The existing workspace backend, registry, TTL collection, explicit destruction, runtime authority checks, and durable run-through outbox are reusable foundations. The accepted ADRs require real files, same-machine durability, explicit deletion, separate live-write consent, and a handoff reference that carries no S3 secret; this plan preserves those constraints.

This section describes the original planning baseline and its then-open gaps. The current implementation progress and final validation are recorded in §8; the run-through composition, Python wrapper, handoff output, and cleanup-surface gaps listed below have since been addressed in the local follow-up. Provider tests remain skipped without provider configuration.

## 2. What the plan already covers well

- **Workspace rather than opaque object storage:** `internal/storage/workspace` maps keys to real files and keeps metadata in one manifest. The Go API exposes this through `stow.OpenWorkspace`.
- **Durability and safe cleanup:** `stow.Resume`, `stow.Collect`, advisory liveness locks, and ownership-aware `Destroy` already establish a strong same-machine lifecycle. Adoption is not permission to delete.
- **One enforcement point:** Go workspace object operations use the embedded runtime, so quota accounting and `Authority` checks are not duplicated in the workspace backend.
- **Run-through safety mechanisms:** local-only default, explicit live-write consent, a durable outbox, and retry/reconciliation machinery already exist. The work is to make production composition behave as tested and to pin the intended bucket semantics.
- **Broad local evidence:** backend contracts, shared S3 conformance, race coverage, and client lifecycle tests provide good places to extend acceptance rather than invent parallel test frameworks.

## 3. Gaps, conflicts, or redundancies to fix

1. **Resolved: run-through production wiring.** At the planning baseline, `cmd/stow-s3/buildStore` opens distinct local and cache stores (`cmd/stow-s3/store.go:23-38`). Most adapter tests use one store for both roles; tests using distinct stores seed both buckets. On an empty production cache, `resolveObject` treats `ErrBucketNotFound` from `cache.HeadObject` as fatal instead of as a cache miss (`internal/runthrough/adapter.go:324-359`). Production runtime quota preflight also calls `HeadObject` before `PutObject` (`internal/runtime/objects.go:17-40`), so a cache lookup error can prevent the write from reaching the adapter/outbox. `ListObjectsV2` similarly requires the local bucket to exist before consulting upstream (`internal/runthrough/adapter.go:442-462`). **Resolution:** empty cache buckets now act as misses; production-shaped tests exercise the adapter through distinct stores and durable propagation.
2. **Resolved: resume policy retention.** At the planning baseline, `ResumeWith` reads directory, bucket, and TTL from the registry, then reopens without forwarding `Authority`, `MaxBytes`, or `MaxObjects` (`pkg/stow/resume.go:36-61`). Since nil authority means full permission and zero limits choose defaults, a resume may not retain the original workspace’s permissions or bounds. The registry entry currently omits those fields (`internal/storage/workspace/registry.go`). **Resolution:** the registry persists effective authority and limits; resume allows narrowing and refuses widening or legacy records without policy.
3. **“Isolated” must not imply a security sandbox.** A separate directory protects the source checkout from accidental file edits, but an agent process running as the same OS user may still access other permitted paths and the network. The first release promises workspace isolation, not OS-level containment, network denial, or hard disk quotas. State this in the API and user docs. If hard containment is required later, integrate with an OS/container sandbox rather than suggesting Stow alone enforces it.
4. **Workspace quotas do not automatically bound arbitrary filesystem writes.** The agent writes through ordinary filesystem calls, outside the Go runtime’s operation path. The ready-to-work feature must not promise that `MaxBytes`/`MaxObjects` prevent those writes. First release should measure and validate seeded/checkpoint size, detect over-limit state at checkpoints, and report the limitation. Hard prevention needs platform-backed filesystem quotas or an external sandbox and is a separate decision.
5. **The product APIs are uneven.** At the planning baseline the workspace API shipped in Go only, and TypeScript/Python workspace contracts were pending (`docs/workspace-contract.md:109-123`). Thin TypeScript/Python wrappers and lifecycle CLI commands are now present; in-process workspace handles and MCP remain future work. The TypeScript `handoff()` is for child S3 processes and returns endpoint credentials (`packages/stow-s3/src/session.ts:236-259`); it remains distinct from a workspace handoff reference.
6. **Handoff and checkpoint semantics need a boundary.** A workspace ID names mutable live state; a checkpoint ID must name immutable captured state. Same-machine references are local registry references, not network capabilities. Cross-machine transfer is an explicit export/import archive, consistent with ADR 0009’s rejection of a relay or shared daemon.
7. **Resolved: platform-sensitive assertion.** The original assertion compared an absolute path without canonicalizing macOS’s `/var` symlink. It now compares canonical paths, and the full test matrix passes.
8. **Resolved: generated artifact verification.** Make targets use the `.go-version` toolchain (1.24.13); the tracked WASM output is included with its generator inputs. `check-generated` repeats the package build and compares hashes so legitimate generated changes are allowed while nondeterministic output fails.

## 4. Existing code/product references to reuse or extend

- `internal/runthrough/adapter.go` — cache-miss resolution, bucket visibility, merged listing, mutation/outbox paths. Extend these paths; do not add a second proxy adapter.
- `cmd/stow-s3/store.go` — authoritative production construction of distinct local and cache stores. Integration tests must use this same shape.
- `internal/runtime/objects.go` and `internal/runtime/adapter.go` — quota preflight and runtime facade. Keep accounting at this choke point; test errors through it, not only through the raw run-through adapter.
- `internal/runthrough/file_outbox.go`, `outbox_adapter.go`, and `authority_gate_test.go` — durable write intent, recovery, and no-call-without-authority behavior.
- `internal/storage/workspace` — natural/escaped file layout, manifest, adoption, cross-platform path rules, and file-backed multipart behavior. Add seed/checkpoint metadata here only where it belongs to the workspace contract.
- `pkg/stow/workspace.go`, `pkg/stow/resume.go`, and `internal/storage/workspace/registry.go` — public Go workspace lifecycle. Extend the record and resume rules rather than creating a second registry.
- `docs/workspace-contract.md`, ADR 0007, ADR 0008, ADR 0009, ADR 0005, and ADR 0006 — normative workspace, ownership, liveness, live-write, and reset rules.
- `conformance/corpus` and `internal/storage/backend_contract_test.go` — shared behavior. Add workspace seed/checkpoint cases where they describe storage semantics; keep production adapter composition tests at the run-through/runtime layer.
- `packages/stow-s3/src/session.ts` and `packages/stow-s3-py/src/stow_s3/session.py` — existing disposable S3 session behavior. Preserve it and add separately named workspace interfaces.
- `Makefile`, `.github/workflows/ci.yml`, `.go-version`, and `scripts/check-generated.mjs` — reproducible full-matrix and artifact verification.

## 5. API impact and validation notes

This plan adds APIs and a workspace/checkpoint format; it does **not** change the behavior of existing `Stow.start`, `Stow.connect`, `openStow`, `withStow`, Python `open_session`, or `with_session`. Existing S3 session handoff continues to provide credentials only for its current child-process use. The new workspace handoff returns a local workspace/checkpoint reference and no access key, secret key, AWS environment mapping, or upstream credential.

Original additive interface plan (current delivery status is in §8):

- Go: extend `WorkspaceOptions`/resume policy handling; add a prepare/seed input model and checkpoint operations on the existing workspace API.
- CLI: add machine-readable workspace `prepare`, `resume`, `checkpoint`, `diff`, `export`, and explicit `restore` commands to the existing binary. `prepare --json` returns `workspaceId`, root, working directory, checkpoint/base identity, effective limits, and capabilities. It returns no credentials.
- TypeScript and Python: thin wrappers over the supported CLI/binary distribution initially; Go remains the sole implementation of workspace preparation and snapshot semantics. Keep the current S3 session APIs separate.
- On-disk data: version the registry and checkpoint manifest. Migrate known old records or refuse with a clear actionable error; never silently discard policy, limits, file metadata, or lineage.

The public API remains additive. Persisted policy and checkpoint manifests have compatibility rules in `docs/workspace-contract.md`; broader clean-install validation remains a release task. No new upstream-write permission is implied by a checkpoint, handoff, or restore.

## 6. Final adjusted plan, summarized

### Phase 0 — Freeze the evidence and unblock the local matrix

1. Preserve the full local test baseline and record live-provider conformance as skipped unless provider credentials are configured.
2. Keep the canonicalized bundled-binary path assertion and pinned Go toolchain in the full verification matrix.
3. Verify generated output reproducibility while retaining legitimate generated changes alongside generator inputs.
4. Keep production run-through regressions at the runtime boundary with distinct stores, empty cache, and a deterministic local fake upstream.

**Exit:** full local gates pass; provider/live checks are accurately marked skipped when unavailable; production-shape integration behavior is covered.

### Phase 1 — Remediate run-through at the production composition boundary (P0)

1. Specify bucket semantics for local-only, locally shadowed upstream, and upstream-only buckets. Decide whether create/head/list/delete bucket calls are local namespace management or should reflect the provider. Make the choice explicit in the compatibility contract.
2. Treat an absent cache bucket as an empty cache when an upstream read is authorized. Create cache buckets lazily where safe, or make all cache read/write helpers tolerate absence. Preserve `ErrBucketNotFound` for a genuinely absent local/upstream bucket after the semantic check.
3. Ensure runtime preflight checks do not turn cache misses into operation failures. Quota/usage calculations must distinguish object-not-found, empty-cache-bucket, and real upstream errors without counting remote bytes as already-local usage.
4. Exercise GET, HEAD, list/pagination, PUT, delete, copy, and multipart through the same construction path as `buildStore`. Start with empty local/cache state. Test both local-shadow and upstream-only behavior as decided.
5. For writes, prove: local-only does not call upstream; no write authority does not call upstream; explicit live-write authority plus durable outbox propagates; transient failure leaves a recoverable ordered intent; restart/retry reconciles it. Do not loosen ADR 0005.
6. Add anti-regression cases where tests use distinct local/cache stores and do not pre-create cache buckets by default. Keep the existing small adapter tests, but do not use them as evidence of production composition.

**Exit:** documented run-through behavior passes the integration matrix against a local fake S3 upstream without cloud credentials; local-only and denied-write cases show zero upstream mutation calls; release/live gates remain meaningful.

### Phase 2 — Preserve workspace policy across resume before extending lifecycle (P0)

1. Decide the persisted representation for effective authority, byte/object limits, TTL, source identity, and ownership. Prefer one canonical versioned workspace metadata record; avoid a second hand-maintained policy file.
2. Change `ResumeWith` so it cannot silently reopen with broader authority or different limits. Either restore the persisted policy or require caller-provided options and reject any widening. A missing/older policy must use a documented fail-closed migration.
3. Add tests: read-only remains read-only after resume; limits remain stable; an explicit attempted widening is refused; an old registry entry has a deterministic migration/refusal result; a destroyed/collected workspace cannot be resumed.

**Exit:** resume returns the same ID, bytes, effective access, and bounds; no old workspace is silently granted more operations.

### Phase 3 — Prepare an isolated, ready-to-work workspace (P0)

1. Add a declarative, versioned task manifest with: seed sources, destination paths, workspace root, working directory, limits/TTL, and optional output paths. Keep the first schema small and deterministic.
2. Support local Git input at an explicit ref and selected local file/directory inputs. Use an isolated task tree; never write into the source checkout. Dirty and untracked changes are excluded. No remote clone, ambient credential copy, or automatic source deletion.
3. Fetch only the selected commit into a fresh shallow repository under the Stow-owned task root. Do not register a linked worktree or add a remote; deleting the owned root removes the task repository. Stage additional files by safe copy. Never recursively delete an adopted project.
4. Validate all paths before creating anything: relative destination paths only, no traversal, no destination collisions by default, no writes through symlinks outside the workspace, and no implicit overwrite. Preserve internal project symlinks only under a documented no-follow/containment policy.
5. Create the Stow workspace after staging completes; open it under the existing runtime, store registry policy, and return JSON with the root, actual working directory, workspace ID, base commit/input identities, limits, and capabilities. The host launches the agent only after successful preparation.
6. Support failure cleanup as a transaction: if seeding, manifest creation, or registration fails, remove only the root and worktree Stow created; retain user-owned sources; report cleanup errors alongside the preparation error.
7. State the boundary plainly: task isolation prevents accidental edits to the source tree; it is not an OS security sandbox. Report filesystem-write quota as measured/checked at prepare and checkpoint, not as a hard limit on arbitrary agent file operations.

**Exit:** a coding task starts in a separate working directory containing the selected repo ref, fixtures, and context files at the requested paths; the selected commit is reported; the source checkout remains unchanged; resume returns the same workspace; seed errors leave no owned partial task; macOS/Linux/Windows path cases are covered where supported.

### Phase 4 — Immutable checkpoints and safe handoff (P1)

1. Define a checkpoint as immutable state identified separately from the mutable workspace ID. Record parent checkpoint, seed/base identity, path, file type, size, content digest, and deletion state. Capture project files, but not `.git` internals or Stow metadata; store the base commit identity separately. Do not store credentials or environment values.
2. Capture only on an explicit request. Require a quiescent workspace or detect concurrent changes during capture and fail rather than claim a consistent checkpoint. Write to a temporary owned location, verify hashes, then atomically publish the checkpoint manifest.
3. Add deterministic `diff` for added/changed/deleted files, including untracked files and binary metadata. Ignore Stow’s internal metadata; do not silently omit user files. Provide a machine-readable format and concise human output.
4. Add explicit restore-to-new-workspace first. In-place restore must refuse while another handle/agent is live and must have a clear backup/rollback rule. Never merge or overwrite the user’s source checkout automatically.
5. Add `workspace handoff` as a same-machine local reference to workspace and checkpoint IDs. Follow ADR 0009: no shared daemon/relay. The reference is not an S3 credential. For cross-machine transfer, add explicit export/import archives with checksums, size limits, path validation, and a preview of included paths. Exclude common credential-file patterns by default, require explicit inclusion to override, and do not claim that pattern checks detect secrets embedded in ordinary files.
6. Define checkpoint retention and disk accounting. Checkpoints count against a separate configurable cap; TTL collection only removes Stow-owned inactive task roots and their checkpoints. Adopted workspaces are never collected automatically.

**Exit:** one process can create a checkpoint, another process can resolve the reference and resume/read it without S3 secrets, the diff is repeatable, export/import verifies integrity, and corrupt/oversized/unsafe archives are refused without partial restore.

### Phase 5 — Make the workflow usable from Go, TypeScript, Python, and agents (P1)

1. Keep Go as the in-process implementation. Add CLI commands to prepare, resume, checkpoint, diff, export, and restore with stable JSON output and structured errors.
2. Add TypeScript and Python wrappers over the same CLI contract and bundled binary resolution. Keep the SDKs thin; do not duplicate manifest parsing, path validation, checkpoint semantics, or outbox logic in each language.
3. Add a small MCP stdio adapter only after CLI/API contracts stabilize. Start with prepare/open/resume, list/read/write files (through workspace paths), checkpoint/diff/export. Do not expose admin outbox mutation or upstream writes through default tools.
4. Keep `openStow`/`withStow` and Python sessions as the opt-in S3 compatibility profile. Document the lifecycle difference between disposable memory sessions and persistent workspaces.

**Exit:** one Go, one TypeScript, one Python workflow prepare the same manifest into the same on-disk contract; MCP calls the same operations; no language implementation widens authority or owns independent storage semantics.

### Phase 6 — Distribution, documentation, and adoption proof (P1)

1. Update `README.md`, `site/agent.md`, `site/llms.txt`, and `skills/stow-s3/SKILL.md` to lead with the supported ready-workspace flow and show S3 sessions as an alternative. Link the manifest and security boundary.
2. Publish/install npm and PyPI packages through clean-room CI when owner-side registry setup is complete. Verify the real documented commands without ambient registry config, Go source checkout, or local `STOW_BIN`.
3. Add lifecycle soak coverage: many parallel task workspaces; process death/resume; TTL collection; checkpoint cleanup; no source modifications; no leaked Git objects or partial archives.
4. Pilot with representative coding tasks: a repo task with fixtures, a non-Git file task, an interrupted/resumed task, and an artifact export. Measure time to agent-ready, setup steps, disk use, checkpoint time, and recovery success.

**Exit:** a fresh user can prepare, launch an external agent in the returned cwd, review a diff, resume or export, and clean up using published artifacts and docs; no task requires an undocumented manual directory-copy step.

## 7. Handover items for engineering, enumerated

1. **Run-through composition —** touch `internal/runthrough/adapter.go`, `internal/runtime/objects.go`, `cmd/stow-s3/store.go`, and run-through tests. Add empty-cache production-shape tests first; define bucket semantics; then fix cache-miss and preflight behavior. Verify through runtime + adapter with a local fake upstream and explicit zero-call safety assertions.
2. **Workspace policy on resume —** touch `pkg/stow/resume.go`, `pkg/stow/workspace.go`, `internal/storage/workspace/registry.go`, and workspace metadata/versioning. Persist or require authority and limits without widening. Verify permission, quota, migration, and destroyed-workspace cases.
3. **Prepared workspace contract —** amend `docs/workspace-contract.md`; add a versioned task manifest and Go preparation API/CLI. Verify path containment, duplicate destinations, explicit Git refs, shallow-history boundary, failure cleanup, and unchanged source checkout.
4. **Checkpoint format and handoff —** extend the workspace metadata/registry and add checkpoint/diff/export code. Distinguish mutable workspace IDs from immutable checkpoint IDs; ensure capture refuses concurrent changes, handoff carries no S3 secret, and import rejects traversal/corruption/oversize.
5. **CLI and language adapters —** touch `cmd/stow-s3`, TypeScript package exports, and Python package exports. Make Go/CLI the canonical semantics and keep TS/Python wrappers thin. Verify JSON protocol compatibility and clean package install.
6. **Test/toolchain repair —** update the macOS assertion in `packages/stow-s3/test/integration.test.ts`, pin local Go artifact generation to `.go-version`, and rerun `make test-all`, `make standards`, and clean-install/package checks. Record upstream live tests as skipped unless actually configured and run.
7. **Product docs and pilot —** update the main README, site/agent pages, skill docs, install instructions, and explicit “isolation is not sandboxing” text. Verify from a clean machine and run the pilot workflows before calling the feature ready.

## 8. Implementation progress

- **Phase 0 — complete locally.** The macOS binary path assertion compares canonical paths. Make targets select the exact `.go-version` toolchain. A pinned Go WASM rebuild changes the tracked package artifact; that generated artifact is included in the implementation diff and must land with its generator inputs. `make test-all` and `make standards` pass, covering Go unit/race/local conformance, the TypeScript and Python wrapper suites, WASM, Go vet/race/quality/coverage, the script tests, TypeScript typecheck/lint/ratchets, and generated-artifact reproducibility. Live upstream provider tests are skipped without provider configuration. Coverage and test counts are not recorded here — see the note in [`plan.md`](plan.md) on why.
- **Phase 1 — implemented locally.** Empty separate cache buckets behave as cache misses and fill lazily; only locally shadowed buckets enter Stow's namespace. Production-shaped runtime tests cover empty-cache HEAD/GET, pagination, unshadowed-bucket refusal, PUT/copy/delete/multipart propagation through a durable outbox, and restart/retry after a transient failure.
- **Phase 2 — implemented.** Registry entries persist effective authority and normalized byte/object limits. Resume restores them, allows narrowing, rejects widening, refuses legacy entries without policy, and retains the prepared cwd. Tests cover read-only, bounds, widening, old records, and resume identity.
- **Phase 3 — local-input and Git-ref preparation implemented.** Version 1 task manifests accept safe local file/directory inputs and explicit refs from local Git repositories. Preparation fetches only the selected commit into a fresh shallow detached repository inside the Stow-owned root, reports its commit ID, omits dirty/untracked changes and older history, and leaves the source checkout and worktree registrations unchanged. Sensitive-name opt-in, limits checks, fingerprints, actual working-directory selection, JSON CLI output, and TypeScript/Python CLI wrappers are present. Submodule/LFS materialization and broader cross-platform symlink/race hardening remain open.
- **Phase 4 — checkpoint, archive preview, and retention caps implemented.** Checkpoint IDs are distinct from workspace IDs; capture excludes `.stow`/`.git`, records sensitive exclusions, verifies hashes and before/after tree state, emits deterministic add/change/delete diffs, and restores to a new workspace. Gzip-tar export/import uses default size/file caps, validates digests and paths in private staging, and requires sensitive-file opt-in on export/import. `workspace preview` verifies the archive and lists included and sensitive-looking paths without extracting files. Import publication reserves IDs without replacing existing checkpoints; export hashes the exact bytes it streams. Workspace checkpoint payload-byte and count caps persist across resume and reject captures without eviction. Checkpoint and prepare paths reject non-portable platform names. Destroy and successful TTL collection remove owned checkpoints. A full OS-level no-follow guarantee and cross-process quiescence for arbitrary direct filesystem writers remain open.
- **Phases 5–6 — partially complete.** Go API and CLI plus thin TypeScript/Python wrappers are present; wrappers use the installed canonical CLI and do not provide in-process workspace objects. The local install-surface check passes, and a dated two-scenario smoke measurement is recorded in [`agent-workspace-pilot.md`](agent-workspace-pilot.md). MCP tools, published package distribution, site/skill/install surface updates, clean-install pilots, and external adoption proof remain outstanding. Soak coverage is no longer outstanding: lifecycle soak, parallel workspaces, process death/resume, TTL collection, checkpoint cleanup, source immuteness, and the absence of partial archives are covered and pass under `-race`.
- **Added after this plan was written.** The cache pillar (`prewarm`, `--offline`, cached-key listing) and `workspace list` are not among the six phases; they came out of the later reframing of the product as a detached working environment with two pillars, and they are tracked in the status table in [`plan.md`](plan.md). So is a delta document digest, and a 503 answer when the upstream is unreachable instead of a 500 that blames this server.
- **Added 2026-09-28, by another session.** The six phases above were written before the
  run-through read path was decided. `conformance/runthrough_pair_test.go` now pins the
  decisions: read-through works for keys inside a bucket the local store holds, a mirrored
  write creates the upstream bucket instead of reporting the object missing, and local-only
  writes never reach the upstream. The bucket question is settled in
  [ADR 0012](adr/0012-run-through-serves-only-its-own-buckets.md): a run-through server refuses
  a bucket it was not given, because fetching anything the upstream credential can see would
  hand the agent the credential the product promises it does not hold. `workspace prune` and
  the `max_workspaces` bound also postdate these phases and are tracked in
  [`plan.md`](plan.md).

The tracked WASM artifact is rebuilt with `-s -w` by `make build-wasm` and must land with its generator inputs. Its hash is deliberately not recorded here: `make check-generated` builds the package twice and compares the output hashes, so a tracked artifact that cannot be reproduced fails on its own rather than needing a digest in prose to catch it. CLI coverage exercises local-file and Git-ref prepare, checkpoint, diff, restore, handoff, resume, export, preview, and import. Git tests verify the selected commit, one-commit history boundary, omission of dirty/untracked files, source immutability, and failure cleanup. Workspace checkpoint archive tests cover preview integrity, corruption and traversal rejection, and duplicate-import protection; TTL/destroy tests cover checkpoint cleanup. `make check-generated` now runs two consecutive package builds and compares their output hashes, so legitimate generated changes can remain in the reviewable diff while nondeterministic generation still fails.

**Explicitly out of scope for this plan:** a remote workspace service, shared daemon, network relay, automatic upstream promotion, arbitrary agent execution, OS-level sandboxing, global internet denial, or a Git replacement. Those require separate contracts and threat models.
