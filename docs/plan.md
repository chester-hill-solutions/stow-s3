# Stow execution plan

**Canonical status and ordering:** 2026-09-27. This is the single current execution plan for workspace/handoff and deploy-anywhere work. The linked specialist plans retain detailed rationale, acceptance criteria, and design notes; if their status or ordering conflicts with this document, this document governs. Historical product plans are not separate backlogs.

**Parallel axis:** [`foss-readiness-plan.md`](foss-readiness-plan.md) covers installability, the conformance matrix's missing backend, and the contribution surface. It is not a competing backlog: it holds no workspace or handoff work, and where the two touch — the conformance matrix and the workspace backend — this document still governs ordering.

The three defects it named on 2026-09-28 have all been resolved since, and the decision behind one of them is now [ADR 0012](adr/0012-run-through-serves-only-its-own-buckets.md). Read-through on an unseeded local bucket and `mirrorWrites` never creating the upstream bucket are both fixed and both pinned by `conformance/runthrough_pair_test.go`; the workspace backend is in the matrix via the workspace contract. The read-through fix is deliberately **not** applied to the bucket case — a run-through server refuses a bucket it was not given, because fetching any bucket the upstream credential can see would hand the agent the credential it is promised not to hold.

## Goal

Deliver one installable, trustworthy workflow for local S3-compatible use and prepared agent workspaces. Then use provider-backed and independent-user evidence to choose portability features. Preserve the accepted boundaries: local behavior is the default, live writes require explicit consent, workspaces are same-machine filesystem directories rather than security sandboxes, and handoffs do not carry credentials.

## Current status

| Workstream | Status | Remaining |
|---|---|---|
| Run-through correctness and multi-writer protection | Implemented locally; regression gates pass | Provider-backed validation remains open. A never-observed upstream key cannot have a prior-version precondition. |
| Detached cache — what an agent can read offline | **Done.** `prewarm` takes an explicit key list and refuses globs; `--offline` is a hard refusal with no upstream call; `/_stow/inspect` names the cached keys with `readable` and `has_expiry` | The warm reports the settled cache, so a warm larger than the cache limit reports what survived it rather than what it fetched. |
| Upstream-failure reporting | **Done.** A dead upstream is 503 `ServiceUnavailable`, not 500 `InternalError`; an upstream 4xx is passed through rather than guessed at | A cache miss under `--offline` still reports `NoSuchKey`, which reads as "does not exist" where the truth is "cannot ask". Deliberate, and worth revisiting. |
| Workspace prepare, resume, checkpoint, handoff, archive, cleanup | Phases 0–4 implemented locally | Submodule/LFS materialization and broader platform symlink/race hardening; OS-level no-follow and arbitrary-writer quiescence guarantees remain open. |
| Workspace enumeration | **Done.** `workspace list` reports every entry, quietest first, including those whose directory is gone, and names the registry it read | Nothing outstanding. The `--all` flag that hid those entries by default is gone from the CLI and both wrappers. |
| Delta sync | Implemented for checkpoints in one registry, with CLI and TypeScript/Python transport | An opt-in document digest catches a substituted change list; provenance and authorship are not recorded. Broaden only for a demonstrated consumer. |
| Checkpoint trigger | **Not started.** No watch, auto-checkpoint, or debounce | An agent editing files calls `workspace checkpoint` by hand. Decide the shape before building: a long-lived watcher interacts with the registry's in-use liveness check and with `Collect`. |
| SigV2 presigned URLs | **Done.** Both forms are refused by name, naming SigV4 and how to switch | The refusal itself is unchanged and `sigv4_strictness_test.go` still guards it. |
| `GetBucketLocation` | **Done.** Answers 200 with the element present and empty; `NoSuchBucket` for a bucket that is not there | None. |
| Workspace retention | **Partly closed.** `workspace prune` forgets entries whose directory is gone, and `max_workspaces` refuses a prepare that would take a registry past a declared bound | A sweep is still manual and `TTLSeconds` still defaults to unlimited. The bound is declared per prepare and not persisted, so the owner of the policy sets it every time; a standing bound would need a registry-level settings file, not a field on an entry. |
| Go/TypeScript/Python workflow | CLI and thin wrappers exist; Phase 5 partial | Published distribution, broader clean-install validation, documentation surfaces; MCP remains deferred. |
| Workspace distribution and adoption | Phase 6 partial | Clean install/release gate, external pilot, and evidence of repeat use. |
| WASM/runtime floor | Phase 2 closed with measured limitation | `-s -w` reduces the artifact by 118 KB (2.2%) but leaves the 8 MiB post-boot floor unchanged. A 24-file agent workload reads in ~26 ms with 0 MiB added; an isolated 1 MiB read adds 7.5 MiB. A sub-8-MiB floor needs a narrower runtime/API or another compiler/runtime. |
| Cache eviction | Phase 3 measured; incremental index deferred | Planner cost is ~5.9 µs/8.5 KB at 40 entries and ~2.69 ms/1.93 MB at 10,000; removing a redundant sort saves three allocations but does not produce a clear timing win. Defer a more complex index until a configured target workload establishes cache depth and latency needs. |
| Persistence backend | Conditional | Choose a concrete target before designing or implementing another backend. |
| SSE-S3 and additional surfaces | Not started | Defer until compatibility need is established and the earlier conflict/runtime work is settled. |

The local `make test-all` and `make standards` gates pass. Live provider tests are skipped because provider configuration is unavailable. No release, clean-room package install, or external pilot has been completed.

**Execution state (2026-09-28):** A code-quality review of the cache and workspace work found and fixed three defects that every gate had passed, which is the point worth recording. A pre-warm reported the fetch rather than the cache — warming six keys into a cache bounded to two reported all six warm and exited 0. `workspace list` hid the unreadable entries behind a flag, so the default output of the one verb whose job is surfacing a problem did not have them. And the `--offline` flag re-parsed its own string and discarded the parse error, so an unreadable value left the network open on the flag whose job is closing it. Following that, a fourth defect surfaced from a test rather than a review: a comment containing a comma in a `--keys-file` was cut in half and the remainder was warmed as a key.

Two root causes are now structural rather than incidental. `check-coverage` is a floor ratchet, so a verb added with no tests left it satisfied; the floor has been raised and `prewarm` has tests. The contract driver's `{{...}}` resolution happened at six call sites, one of which already failed silently, and is now a single pass that cannot be skipped. A separate review finding was that a dead upstream returned 500 `InternalError` naming stow's own storage layer; it is now 503 with a message naming the upstream, verified against a SIGKILLed upstream with a real SDK client.

The release gate is unchanged and remains the only thing between this and a release. It is also the instrument Steps 1 and 3 need, which is why it is the critical path rather than the last step.

**On numbers in this file.** Test counts, coverage percentages, and artifact hashes are deliberately not recorded here. They were, and they went stale silently: the progress record in `agent-workspace-plan.md` carried counts and a WASM digest that were already wrong before anyone noticed, because nothing checks a number that has been copied into prose. The authoritative values are produced by `make standards` (which includes `check-coverage`, whose recorded floor is the ratchet) and `make check-generated` (which proves the packaged artifact is reproducible). Read the number from the gate, not from a document.

## Ordered work

**Execution state (2026-09-27):** Local lifecycle soak is complete; Step 1 remains open for clean-room installation and release readiness. This pass rebuilt native/WASM artifacts; version/install-surface checks and npm package dry-run passed. Go workspace/storage/CLI tests, TypeScript and Python workspace wrapper tests passed. A child-process kill/resume test, archive staging/temp cleanup assertions, and a three-round/eight-workspace concurrent isolation test were added; the concurrency test passed under `-race`. A two-second TTL CLI smoke collected an expired workspace and removed its root; checkpoint removal is covered by the existing collection tests. A 24-workspace manual isolation run also passed. The WASM stripped candidate shrank from 5,404,915 to 5,286,611 bytes (2.2%) with the post-boot floor still 8 MiB; agent-shaped read overhead was 0 MiB, but an isolated 1 MiB read added 7.5 MiB. Cache planner measurements and the small allocation-only improvement are recorded below. The agent guide and skill now present ready workspaces as the persistent coding-task path and scoped S3 sessions as disposable. The CLI package needed loopback permission outside the sandbox. The shared npm cache was not writable, so it was left untouched. The Python wheel rehearsal could not run because Python 3.12 lacks the `build` module; CI's release matrix remains the wheel-build check. No release or external registry action was taken.

### 1. Close the local readiness gate — local soak complete; clean install remains open

- **Done locally:** lifecycle soak for parallel workspaces, process death/resume, TTL collection, checkpoint cleanup, source immutability, and absence of partial archives or leaked Git state. The concurrent isolation case passed under `-race`; see the execution record above.
- Verify the documented install and full lifecycle outside the source checkout, without ambient configuration or a locally built binary. Publish/install npm and PyPI packages when registry setup is available; make that dependency explicit in the release checklist.
- **Done locally:** README, site, and skill install guidance lead with the supported ready-workspace flow and state its isolation boundary.
- Keep the release version/tag decision separate from the incomplete `v0.2.0` tag; do not treat that tag as a completed release.

**Exit:** a clean environment can install, prepare, use, review, resume/export, and clean up a workspace using only documented commands and available artifacts.

### 2. Finish the open P0 deploy-anywhere work

- Runtime Phase 2 is closed by documenting the measured limit: linker stripping shrinks the file but not runtime memory, and no supported Go switch removes the runtime reflection/type machinery. The 24-file workload (597 KB) seeds to 12.5 MiB at 7.9× payload, reads in ~26 ms with 0 MiB additional memory, and performs six edits in ~4 ms. Keep the large-object result distinct: storing a 1 MiB object adds 4.5 MiB and reading it adds a further 7.5 MiB. A lower floor requires a narrower WASM API or a different compiler/runtime; defer until a concrete device budget justifies that cost.
- Cache Phase 3 keeps backend listing off refresh. The synthetic planner benchmark measures ~5.9 µs/8.5 KB at 40 entries, ~26.7 µs/33 KB at 160, ~208 µs/197 KB at 1,000, and ~2.69 ms/1.93 MB at 10,000. Removing the duplicate sort reduced allocations from seven to four per call without a reliable timing gain. Defer a maintained incremental eviction index until a target cache depth and refresh p95 requirement are named; preserve current policy and reconciliation coverage.

**Exit:** the runtime-floor outcome is recorded against the plan's criterion. Bounded-cache cost is measured; the incremental index remains conditional on workload evidence rather than an assumed requirement.

These tasks may proceed alongside the install gate. Resolve any overlap with the workspace workflow before adding MCP or another surface.

### 3. Validate with providers and independent users

- Run the existing local fake-provider matrix and then a real S3-compatible scratch-provider matrix when credentials are available. Include first upload, conditional writes/conflicts, durable outbox retry/restart, multipart, and propagation timing.
- Pilot an interrupted/resumed coding task, a repo task with fixtures, a non-Git file task, and artifact export with users outside the implementation team.
- Record setup interventions, time to agent-ready, success/recovery, checkpoint and transfer cost, disk use, cleanup, and repeat use. Use those results to choose the next feature.

**Exit:** provider behavior and product value have evidence beyond loopback and internal smoke runs.

### 4. Make conditional and evidence-led decisions

- Name a persistence target before starting deploy-anywhere Phase 4. If no target is required by a real deployment, leave the phase conditional.
- Complete SSE-S3 wire behavior only for demonstrated SDK compatibility needs.
- Add MCP or other integration surfaces only when a pilot shows that the stable CLI/API contract leaves material integration work. Keep the adapter on existing operations and preserve the same permission and conflict behavior.
- Revisit cache indexing, session defaults, and concurrency guidance with workload data rather than converting local benchmark points into product limits.

## Performance evidence and implications

The [local performance baseline](benchmarks/2026-09-27/tool-performance-baseline.md) is a machine-specific M1 Pro, loopback, memory-backend and local-APFS measurement. Fresh S3 session first upload is 24.06 ms p50; 8 to 16 sockets add about 8% local PUT throughput while p50 latency rises about 75% and loaded RSS rises 33.5 MiB. These results do not justify a connection cap or predict provider performance.

The baseline identifies two follow-up measurements, not automatic changes: test memory at the configured maximum object size and realistic concurrent sessions, since the 16 MiB object quota is not a memory ceiling; and measure long-lived workspace disk usage/cleanup, since root plus registry is about 3× seeded payload with two full checkpoints. Existing checkpoint count/byte caps should be exercised in the soak.

## Source plans and evidence

- [Agent workspace, handoff, and run-through detail](agent-workspace-plan.md)
- [Deploy-anywhere, portability, and multi-writer detail](deploy-anywhere-plan.md)
- [Assessment and repair follow-up](assessment-2026-09-27.md)
- [Workspace contract](workspace-contract.md) and [S3 compatibility contract](compat-contract.md)
- [Raw and summarized performance measurements](benchmarks/2026-09-27/tool-performance-baseline.md)
- [Current handoff notes](handoff-2026-09-27.md)
