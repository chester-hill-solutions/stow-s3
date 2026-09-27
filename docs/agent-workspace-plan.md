# Agent Workspace, Handoff, and Run-Through Plan

**Status:** implementation in progress; baseline checked against `main` at `260bbe82e114a0f9c4c84c51250f402af2a0b1d8`
**Date:** 2026-09-27
**Scope:** remediate production run-through composition; prepare isolated, pre-seeded agent workspaces; checkpoint and hand off their results.
**Relationship:** additive to the accepted decisions in ADRs 0005–0011 and `docs/workspace-contract.md`. This is the current execution order for these workstreams and supersedes the related backlog ordering in `docs/agent-dx-plan.md` §0.11/§10.1. The older plan remains historical context; it is not a second implementation specification.

## 1. Plan validation summary

**Verdict: Adjust before implementation; the direction aligns, but the resume-policy gap and production/test wiring mismatch must be addressed first.** The existing workspace backend, registry, TTL collection, explicit destruction, runtime authority checks, and durable run-through outbox are reusable foundations. The accepted ADRs require real files, same-machine durability, explicit deletion, separate live-write consent, and a handoff reference that carries no S3 secret; this plan preserves those constraints.

The full local matrix was run on this baseline. Go unit tests, race tests, and all four local conformance configurations passed. Python passed 47/47 and WASM passed 1/1. TypeScript passed 81/82; the sole failure compares equivalent macOS `/var` and `/private/var` paths as raw strings. The run-through live-provider case was skipped because no provider was configured. `go vet`, TypeScript typecheck, and TypeScript lint passed. See §3 for the production run-through gap that these green adapter tests do not currently cover.

## 2. What the plan already covers well

- **Workspace rather than opaque object storage:** `internal/storage/workspace` maps keys to real files and keeps metadata in one manifest. The Go API exposes this through `stow.OpenWorkspace`.
- **Durability and safe cleanup:** `stow.Resume`, `stow.Collect`, advisory liveness locks, and ownership-aware `Destroy` already establish a strong same-machine lifecycle. Adoption is not permission to delete.
- **One enforcement point:** Go workspace object operations use the embedded runtime, so quota accounting and `Authority` checks are not duplicated in the workspace backend.
- **Run-through safety mechanisms:** local-only default, explicit live-write consent, a durable outbox, and retry/reconciliation machinery already exist. The work is to make production composition behave as tested and to pin the intended bucket semantics.
- **Broad local evidence:** backend contracts, shared S3 conformance, race coverage, and client lifecycle tests provide good places to extend acceptance rather than invent parallel test frameworks.

## 3. Gaps, conflicts, or redundancies to fix

1. **Run-through tests do not match production wiring.** `cmd/stow-s3/buildStore` opens distinct local and cache stores (`cmd/stow-s3/store.go:23-38`). Most adapter tests use one store for both roles; tests using distinct stores seed both buckets. On an empty production cache, `resolveObject` treats `ErrBucketNotFound` from `cache.HeadObject` as fatal instead of as a cache miss (`internal/runthrough/adapter.go:324-359`). Production runtime quota preflight also calls `HeadObject` before `PutObject` (`internal/runtime/objects.go:17-40`), so a cache lookup error can prevent the write from reaching the adapter/outbox. `ListObjectsV2` similarly requires the local bucket to exist before consulting upstream (`internal/runthrough/adapter.go:442-462`). **Fix:** define local-shadow versus upstream-only bucket behavior, then add production-composition tests with distinct empty stores before changing the code. Do not repeat the older blanket claim that the adapter never works: seeded adapter tests prove it can read and propagate writes in narrower configurations.
2. **Resume can widen access and change resource limits.** `ResumeWith` reads directory, bucket, and TTL from the registry, then reopens without forwarding `Authority`, `MaxBytes`, or `MaxObjects` (`pkg/stow/resume.go:36-61`). Since nil authority means full permission and zero limits choose defaults, a resume may not retain the original workspace’s permissions or bounds. The registry entry currently omits those fields (`internal/storage/workspace/registry.go`). **Fix before adding more resume/handoff clients:** persist the effective policy and limits, or require an explicit resume policy and reject any widening. Old manifests/registry entries must fail closed or migrate with an explicit, tested rule.
3. **“Isolated” must not imply a security sandbox.** A separate directory protects the source checkout from accidental file edits, but an agent process running as the same OS user may still access other permitted paths and the network. The first release promises workspace isolation, not OS-level containment, network denial, or hard disk quotas. State this in the API and user docs. If hard containment is required later, integrate with an OS/container sandbox rather than suggesting Stow alone enforces it.
4. **Workspace quotas do not automatically bound arbitrary filesystem writes.** The agent writes through ordinary filesystem calls, outside the Go runtime’s operation path. The ready-to-work feature must not promise that `MaxBytes`/`MaxObjects` prevent those writes. First release should measure and validate seeded/checkpoint size, detect over-limit state at checkpoints, and report the limitation. Hard prevention needs platform-backed filesystem quotas or an external sandbox and is a separate decision.
5. **The product APIs are uneven.** The workspace and resume API currently ship in Go only; the TypeScript/Python workspace contracts are explicitly pending (`docs/workspace-contract.md:109-123`). The TypeScript `handoff()` is for child S3 processes and returns endpoint credentials (`packages/stow-s3/src/session.ts:236-259`); it must remain distinct from a workspace handoff reference. Additive workspace APIs/CLI commands are needed for JS/Python callers; do not silently change the existing session contract.
6. **Handoff and checkpoint semantics need a boundary.** A workspace ID names mutable live state; a checkpoint ID must name immutable captured state. Same-machine references are local registry references, not network capabilities. Cross-machine transfer is an explicit export/import archive, consistent with ADR 0009’s rejection of a relay or shared daemon.
7. **The TypeScript suite has a platform-sensitive assertion.** `packages/stow-s3/test/integration.test.ts:46-75` compares an absolute path without canonicalizing macOS’s `/var` symlink. Fix the assertion and rerun the full matrix.
8. **Generated artifacts need a pinned local toolchain.** The test run used Go 1.25.6 while `.go-version` pins 1.24.13; rebuilding WASM changed the tracked artifact. The generated diff was restored. Make the local verification instructions select the pinned toolchain, and ensure `check-generated` proves reproducibility from that toolchain.

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

Expected additive interfaces:

- Go: extend `WorkspaceOptions`/resume policy handling; add a prepare/seed input model and checkpoint operations on the existing workspace API.
- CLI: add machine-readable workspace `prepare`, `resume`, `checkpoint`, `diff`, `export`, and explicit `restore` commands to the existing binary. `prepare --json` returns `workspaceId`, root, working directory, checkpoint/base identity, effective limits, and capabilities. It returns no credentials.
- TypeScript and Python: thin wrappers over the supported CLI/binary distribution initially; Go remains the sole implementation of workspace preparation and snapshot semantics. Keep the current S3 session APIs separate.
- On-disk data: version the registry and checkpoint manifest. Migrate known old records or refuse with a clear actionable error; never silently discard policy, limits, file metadata, or lineage.

The new public API is additive, but persisted policy and checkpoint manifests require compatibility decisions before release. Include cross-platform path/symlink rules, max sizes, collision behavior, archive checksums, and error codes in `docs/workspace-contract.md` before implementation lands. No new upstream-write permission is implied by a checkpoint, handoff, or restore.

## 6. Final adjusted plan, summarized

### Phase 0 — Freeze the evidence and unblock the local matrix

1. Keep the passing test baseline above in this plan; record that live-provider conformance was skipped, not passed.
2. Canonicalize the bundled-binary path assertion in `integration.test.ts` and run it on macOS. Do not weaken the resolver behavior to satisfy the test.
3. Make local generated-artifact checks use Go 1.24.13 from `.go-version`; verify `make build-wasm` leaves no diff from a clean checkout.
4. Capture the exact production run-through store wiring in a reusable integration-test helper. Use two distinct stores, an empty cache, a deterministic local fake upstream, and the runtime wrapper.

**Exit:** TypeScript is 82/82; Go vet/typecheck/lint stay clean; generated output matches; the integration fixture demonstrably differs from the adapter unit fixture in the current empty-cache case.

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

- **Phase 0 — complete locally.** The macOS binary path assertion compares canonical paths. Make targets select the exact `.go-version` toolchain. A pinned Go 1.24.13 WASM rebuild changes the tracked package artifact; that generated artifact is included in the implementation diff and must land with its generator inputs. Final `make test-all` passed: Go unit/race/local conformance, TypeScript 84/84, Python 50/50, and WASM 1/1. Live upstream provider tests were skipped without provider configuration. `go vet` and the Go quality ratchet pass. `make standards` reaches the file-size ratchet and fails only on the pre-existing untouched `internal/storage/fs/longkeys_test.go:627`; checkpoint implementation files now satisfy the 500-line limit. TypeScript standards pass after the workspace wrappers were added.
- **Phase 1 — implemented locally.** Empty separate cache buckets behave as cache misses and fill lazily; only locally shadowed buckets enter Stow's namespace. Production-shaped runtime tests cover empty-cache HEAD/GET, pagination, unshadowed-bucket refusal, PUT/copy/delete/multipart propagation through a durable outbox, and restart/retry after a transient failure.
- **Phase 2 — implemented.** Registry entries persist effective authority and normalized byte/object limits. Resume restores them, allows narrowing, rejects widening, refuses legacy entries without policy, and retains the prepared cwd. Tests cover read-only, bounds, widening, old records, and resume identity.
- **Phase 3 — local-input and Git-ref preparation implemented.** Version 1 task manifests accept safe local file/directory inputs and explicit refs from local Git repositories. Preparation fetches only the selected commit into a fresh shallow detached repository inside the Stow-owned root, reports its commit ID, omits dirty/untracked changes and older history, and leaves the source checkout and worktree registrations unchanged. Sensitive-name opt-in, limits checks, fingerprints, actual working-directory selection, JSON CLI output, and TypeScript/Python CLI wrappers are present. Submodule/LFS materialization and broader cross-platform symlink/race hardening remain open.
- **Phase 4 — checkpoint, archive preview, and retention caps implemented.** Checkpoint IDs are distinct from workspace IDs; capture excludes `.stow`/`.git`, records sensitive exclusions, verifies hashes and before/after tree state, emits deterministic add/change/delete diffs, and restores to a new workspace. Gzip-tar export/import uses default size/file caps, validates digests and paths in private staging, and requires sensitive-file opt-in on export/import. `workspace preview` verifies the archive and lists included and sensitive-looking paths without extracting files. Import publication reserves IDs without replacing existing checkpoints; export hashes the exact bytes it streams. Workspace checkpoint payload-byte and count caps persist across resume and reject captures without eviction. Checkpoint and prepare paths reject non-portable platform names. Destroy and successful TTL collection remove owned checkpoints. A full OS-level no-follow guarantee and cross-process quiescence for arbitrary direct filesystem writers remain open.
- **Phases 5–6 — partially complete.** Go API and CLI plus thin TypeScript/Python wrappers are present; wrappers use the installed canonical CLI and do not provide in-process workspace objects. The local install-surface check passes, and a dated two-scenario smoke measurement is recorded in [`agent-workspace-pilot.md`](agent-workspace-pilot.md). MCP tools, published package distribution, site/skill/install surface updates, clean-install pilots, soak coverage, and external adoption proof remain outstanding.

The tracked WASM artifact was rebuilt by `make test-all` with Go 1.24.13 after the checkpoint and path changes; its current SHA256 is `52f94faa2301287907a6d51924c8a7662ad9975159ede24bcb7e9526f5b19f8f`. CLI coverage exercises local-file and Git-ref prepare, checkpoint, diff, restore, handoff, resume, export, preview, and import. Git tests verify the selected commit, one-commit history boundary, omission of dirty/untracked files, source immutability, and failure cleanup. Workspace checkpoint archive tests cover preview integrity, corruption and traversal rejection, and duplicate-import protection; TTL/destroy tests cover checkpoint cleanup. `make check-generated` was not used as a gate because it compares generated artifacts with `HEAD`; this working tree intentionally changes the generator inputs and generated WASM artifact together.

**Explicitly out of scope for this plan:** a remote workspace service, shared daemon, network relay, automatic upstream promotion, arbitrary agent execution, OS-level sandboxing, global internet denial, or a Git replacement. Those require separate contracts and threat models.
