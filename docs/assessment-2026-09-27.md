# Stow assessment: codebase, tool, idea, and execution

The assessment below records the state of `main` at `155dff287e4b522b80acceacd12a945d2837b37b`, after fetching and fast-forwarding from origin. Its findings are historical: the implementation follow-up at the end of this document repairs the nine reproduced defects and adds lifecycle commands. Release and adoption gaps remain.

## Judgment

Stow is worth continuing, with a narrower promise and a period devoted to correctness and delivery. The local S3 fixture is the strongest established use case. Prepared agent workspaces have a useful working core, but the language integrations and complete lifecycle need work. The broader portable, multi-writer storage story currently exceeds the evidence.

The architecture is serviceable and should be retained. The immediate problem is that documented guarantees and completion labels are stronger than the behavior at several boundaries. Both existing aggregate gates pass, while targeted checks reproduce data-integrity failures and a completely broken Python workspace entry point. More features would increase the cost of repairing those contracts.

| Dimension | Assessment |
| --- | --- |
| Idea | A credible developer-tool niche: local storage with ordinary SDKs, scoped lifecycle, and no separately managed service. The workspace extension needs evidence of benefit over existing filesystem/Git workflows. |
| Tool | Native CLI and core local workflows work from source. Package distribution, workspace wrappers, and cleanup ergonomics prevent a complete newcomer experience. |
| Codebase | Good shared abstractions, substantial tests, explicit permissions, and reproducible gates. Important composition and input-validation defects remain. |
| Execution | Fast implementation and useful measurement-driven corrections. Weak closure of user workflows, release delivery, and acceptance claims. |

## Verification performed

- `make standards`: **passed**, including Go vet, race tests, structural gates, coverage, script tests, TypeScript typecheck/lint/ratchets, and generated-artifact verification. Observed Go statement coverage was **69.8% (5,992/8,585)**; the recorded floor remains lower. There were 65 passing script tests.
- `make test-all`: **passed**. Go tests and race checks, four local conformance configurations, **84 TypeScript tests**, **50 Python tests**, and **one WASM integration test** passed. Live-provider tests were explicitly skipped because no provider was configured for this run.
- Seven temporary Go regression probes: **all seven failed their intended safety/correctness assertions**. The probes used a Go overlay and disposable local data, leaving repository test files unchanged.
- Real TypeScript and Python workspace calls exposed two additional defects described below.
- A native CLI pilot used this repository at the reviewed commit, prepared an isolated copy, made two disposable edits, created a handoff, resumed, checkpointed, diffed, exported, previewed, and imported successfully. The source checkout was not edited by that pilot.

The pilot staged **528 files / 19,092,945 bytes**, including selected Git metadata. The final checkpoint contained **507 files / 13,433,621 bytes**. Single-run timings on this macOS machine were:

| Operation | Seconds |
| --- | ---: |
| Prepare selected Git ref | 0.874 |
| Initial checkpoint | 0.298 |
| Handoff to file | 0.135 |
| Resume | 0.072 |
| Checkpoint after editing | 0.341 |
| Diff | 0.008 |
| Export | 0.513 |
| Preview | 0.080 |
| Import into another registry | 0.216 |

These are local smoke timings, not a comparative benchmark, clean-install validation, or evidence that an agent completes tasks faster. The first pilot harness assumed every successful CLI command printed JSON; `handoff --output` correctly writes to a file instead. After adapting the harness to that behavior, the CLI flow completed. The shipped wrappers make the same assumption and are affected as detailed below.

Local evidence is retained under `/tmp/stow-assessment/`, including `overlay.json`, the two probe source files, `reproductions.log`, `quota-reproduction.log`, `python-wrapper-result.json`, and `pilot-result.json`. Aggregate logs are `/tmp/stow-assessment-standards.log` and `/tmp/stow-assessment-test-all.log`. These are temporary review artifacts, not committed regression tests.

## Confirmed findings

Five high-priority and four medium-priority defects were reproduced. Severity here reflects impact on the advertised workflows; no remote exploitation claim is made.

### High priority

**H1 — Delta application can overwrite its immutable base through an unchecked path.**

In [delta_apply.go](../pkg/stow/delta_apply.go), `ApplyDelta` validates version and per-file preconditions, then `applyDeltaWrite` joins `change.Path` onto its staging directory without using the existing checkpoint path validator. Staging is inside the base checkpoint. An added path of `../../files/edit.txt` escapes staging and overwrites the base checkpoint's file. The probe changed the base from `before` to `pwned!`, and `ApplyDelta` returned success. Key locations: lines 45, 52, and 123.

This is currently a public Go API issue; the delta command/remote transport is not exposed. Repair the application boundary before exposing portable deltas through additional clients: validate paths and document structure before any write, contain filesystem operations, verify content, and reuse the archive/checkpoint validation rules.

**H2 — Copy operations bypass upstream conflict detection.**

In [outbox_adapter.go](../internal/runthrough/outbox_adapter.go), line 114 records upstream state only for `OutboxPut`. Copy and multipart completion enqueue other operation types but eventually use the same propagation function. A reproduced sequence read an upstream key, let a second writer change it, then copied a local source over that key. The operation returned success and upstream contained `agent copy`, replacing `device update`.

The multipart preparation path has the same missing condition by inspection, and delete propagation has no upstream version condition. The copy case is the directly reproduced evidence. The compatibility contract's general concurrent-write claim needs an operation-by-operation implementation and test matrix.

**H3 — A successful first write discards the provenance needed to protect the next edit.**

[adapter.go](../internal/runthrough/adapter.go), line 251, deletes the cache index entry after a local write. That entry also holds the observed upstream ETag. A later read comes from the authoritative local store. The next write therefore consults the current upstream state rather than retaining the state the local version was based on.

Reproduction: read original, write `agent first` successfully, read again, let another writer publish `device update`, then write `agent second`. The second edit returned success and overwrote the device update. Persist provenance independently of cache residency and advance it coherently after successful propagation. Cover repeated edits, eviction, and restart.

**H4 — A failed upstream write leaves committed local data outside runtime quota accounting.**

[objects.go](../internal/runtime/objects.go), line 53, returns on a store error before updating usage. The run-through adapter can return an upstream error after committing locally. These are incompatible interpretations of the store result.

With `MaxObjects=1`, the probe forced upstream writes to fail. The first local object existed while runtime usage reported `{Bytes:0 Objects:0}`. A second distinct object was also committed locally instead of being rejected for exceeding the quota. This affects the advertised bounded-storage guarantee during provider failures. Define and handle local-commit outcomes across the runtime/adapter boundary, including copy, delete, and multipart mutations.

**H5 — Every Python workspace wrapper fails before starting the native CLI.**

[workspace.py](../packages/stow-s3-py/src/stow_s3/workspace.py), line 30, passes `require_stow_binary()` directly as the first subprocess argument. [bin.py](../packages/stow-s3-py/src/stow_s3/bin.py), line 118, returns a `ResolvedBinary` object, whose `.path` is the required string.

A real `resume_workspace` call against the successfully prepared CLI workspace raised `TypeError: expected str, bytes or os.PathLike object, not ResolvedBinary`. All workspace helpers use this runner. The existing workspace tests replace the resolver with a string-returning mock, so they test a different contract and pass. Repair the call and add a real binary integration path to the Python workspace suite.

### Medium priority

**M1 — Delta serialization silently drops all file content.**

[delta.go](../pkg/stow/delta.go), line 97, marks `Content` as `json:"-"`; `EncodeDelta` at line 268 simply marshals that struct. `CreateDelta → EncodeDelta → DecodeDelta → ApplyDelta` fails with `delta carries no content for "edit.txt"`. The lack of a CLI transport is already documented; the public codec itself is also incomplete. Pin a complete round trip before presenting it as transferable.

**M2 — Delta content reads do not verify the promised digest.**

`attach` in [delta.go](../pkg/stow/delta.go), line 208, checks file length but not SHA-256, despite its comment. `ReadCheckpointFile` has the same size-only behavior. Replacing a target checkpoint file with different bytes of the same length caused `CreateDelta` to accept and carry the forged content. Application also does not validate supplied content against the declared target digest. Reuse the digest verification already present in archive/capture paths and test same-length corruption.

**M3 — Delta application loses executable-bit changes.**

[delta_apply.go](../pkg/stow/delta_apply.go), line 127, uses `os.WriteFile` with the target mode after the old file has already been copied into staging. On an existing file, that argument does not change its permissions. A valid delta changing `run.sh` from `0644` to `0755` produced a checkpoint still recording `0644`. Apply the mode explicitly and include metadata in conflict validation.

**M4 — TypeScript handoff-to-file reports failure after successfully writing the handoff.**

[workspace.ts](../packages/stow-s3/src/workspace.ts), line 40, always parses stdout. `handoffWorkspace` forwards its supported `output` option, while [workspace.go](../cmd/stow-s3/workspace.go), line 302, writes that JSON to the file instead. The real TypeScript call created the requested file and threw `SyntaxError: Unexpected end of JSON input`. Align the CLI result contract and wrapper behavior. The Python runner also always expects stdout JSON and will encounter this second issue after H5 is fixed.

## Codebase and architecture

The strongest engineering choices are worth preserving:

- The shared storage interface and backend contract suites keep common object behavior in one place.
- Runtime authority and quota enforcement give the adapters a common policy layer.
- Local mode constructs no upstream client, and scoped sessions strip ambient cloud configuration. Those are useful structural safety properties.
- Durable outbox preparation, recovery, ordering, and claims address real failure modes rather than assuming network writes always succeed.
- Tests use actual AWS SDKs and the real S3 server, and generated WASM/package outputs are checked for drift.
- Archive import already has staged validation, path checks, and digest checks. The delta defects call for reuse of these mechanisms, not a replacement architecture.

The weaknesses are chiefly between layers and between consecutive operations. A method may return an error after committing; the next layer assumes it did not commit. A cache index doubles as provenance storage. A wrapper mocks away the resolver's real type. A new artifact format bypasses the older format's validation. These relationships deserve more test attention than another structural ratchet.

The cache improvement also has a narrower result than its test commentary claims. [cache_policy.go](../internal/runthrough/cache_policy.go), lines 145–149 and 188, still walks the entire index and sorts candidates on every bounded-cache refresh. The new test measures backend listing rows and therefore correctly proves that those reads are gone. It does not prove that refresh cost stops growing with cache depth. Incremental counters and eviction ordering remain distinct work if larger measured workloads justify them.

Tracked source scale was approximately 22,000 Go lines excluding tests, 4,300 TypeScript lines, and 1,400 Python lines, plus roughly 27,300 test lines across those languages. There were 29 Markdown documents under `docs/`, totaling about 10,700 lines. These counts include comments and describe maintenance surface, not quality. The core is large enough that overlapping plans and duplicated guarantee prose now have a tangible maintenance cost.

## The tool as a user experiences it

The native workspace workflow succeeds on a real repository, and the measured local overhead is modest. Preparation produces an explicit working directory and commit identity; checkpoints and archives give a task runner something concrete to review or transfer. That is useful functionality.

Three gaps prevent a complete product experience:

1. **Acquisition:** the repository documents an incomplete `v0.2.0` release and unavailable npm/PyPI install surfaces. Even after publication is repaired, the npm instructions omit an adoption cost: GitHub's npm registry requires authentication for public packages as well. Scope mapping alone is insufficient. A public FOSS entry point should either document that setup fully or deliberately choose a registry/distribution route without that requirement. [GitHub's registry documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry).
2. **Language parity:** the CLI workflow works, but Python workspace calls are broken and the TypeScript output-file path is broken. Returning `Record<string, unknown>` / `dict[str, object]` also makes these wrappers less helpful than the typed scoped-session APIs.
3. **Lifecycle completion:** the CLI command switch exposes prepare/resume/checkpoint/diff/restore/handoff/export/preview/import, but no workspace destroy or collect command. Those operations exist in Go. A CLI or language-wrapper user cannot complete the advertised cleanup lifecycle through the same surface, and setting a TTL is not itself a running collector.

Finish install → prepare → use → checkpoint → resume/export → cleanup through one documented entry point. That is more valuable now than adding an additional way to invoke incomplete operations.

## The idea and its positioning

The clearest promise is: **give a test or agent its own local storage with predictable lifecycle and an SDK it already uses.** Fast startup, no separately operated service, explicit local defaults, and cross-language behavior make a coherent developer tool.

There is existing competition. LocalStack documents CLI/container workflows; Adobe S3Mock provides a local S3 subset; Moto supplies convenient Python mocking. Stow's case should be demonstrated convenience and lifecycle fidelity for its chosen workload, with a clear compatibility boundary. Merely implementing S3 locally is not sufficient differentiation. [LocalStack installation](https://docs.localstack.cloud/aws/getting-started/installation/), [Adobe S3Mock](https://github.com/adobe/S3Mock), [Moto getting started](https://docs.getmoto.org/en/latest/docs/getting_started.html).

The prepared-workspace idea is plausible, especially for mixed Git and non-Git inputs and reviewable handoff artifacts. But ordinary coding tasks already have directories and Git worktrees. Stow must show fewer setup/recovery steps or better artifact handling than that baseline. Its selected-ref copy, non-Git input manifests, persistent task identity, and export rules are the candidate advantages. The recorded two tiny internal pilots do not establish external demand or reduced agent task time. [Git worktree documentation](https://git-scm.com/docs/git-worktree).

The portability work is a promising option, not the release's main justification yet. The Node VM probe establishes useful absence-of-Node-API compatibility. It does not validate a deployed Worker, real browser persistence under browser failures, or a microcontroller. Cloudflare's current documentation does support the revised 64 MiB uncompressed bundle limit, but also specifies memory, startup, and CPU constraints; the complete runtime and persistence path still needs a named deployment test. [Cloudflare Workers limits](https://developers.cloudflare.com/workers/platform/limits/).

I would lead the next usable release with local S3 fixtures and keep prepared agent workspaces as the focused adjacent pilot. If the strategic priority is specifically agent workspaces, make their complete lifecycle the release gate and defer unrelated portability features. Neither choice currently requires a new repository, a new framework, or a runtime rewrite.

## Execution assessment

The project shows productive engineering habits: measurements have changed plans, abandoned directions are documented, real provider limitations are recorded, and regression tests accompany fixes. The successful local gates and repository-sized CLI pilot substantiate the work.

The failure pattern is equally clear: the implementation of a feature and a convincing explanation of its intent are being treated as stronger completion evidence than they are. The latest delta comments promise digest verification while checking length. The Python wrapper suite passes with an impossible mock return type. The cache test proves less than its prose says. The plan marks phases 0–5 done despite open conditions and the defects reproduced here. ADR 0009 still describes an old run-through blocker as current.

This makes the current execution look better at producing components than closing user journeys. Fixing that requires a stricter definition of done, not a longer roadmap: each important guarantee needs a test through the actual public entry point, an adverse-state case, a repeated-operation case, and accurate delivery status.

## Recommended order

1. **Repair trust boundaries and composition first.** Address H1–H5, then the delta round trip/integrity/mode issues and handoff output contract. Preserve reproductions as focused regression tests. Review all mutation forms and repeated/restarted workflows when fixing provenance and quota accounting. Reopen the affected completion labels.
2. **Deliver one complete installable workflow.** Choose the first release audience, finish the corresponding CLI/language lifecycle including cleanup, verify packages outside the source checkout with no ambient binary/configuration, and complete the existing release gate under a new version. Make any excluded experimental guarantees explicit.
3. **Use representative pilots to choose subsequent features.** Have independent users run a real S3 integration fixture and an interrupted/resumed coding task with artifact review and cleanup. Measure setup interventions, success, recovery, disk use, and repeat use. Add MCP when it removes demonstrated integration work; prioritize SSE headers or runtime-memory optimization when a named workload needs them.

Until the correctness findings are resolved, I would not begin SSE-S3 support, broaden delta transport, or present multi-writer behavior as safe across operations. The project has enough capability to test its value now; the next improvement should make that capability trustworthy and easy to obtain.


## Repair follow-up

The authorized follow-up added regression coverage and addressed the nine findings above. Delta documents now serialize content, validate paths and metadata at encode/decode/apply boundaries, verify SHA-256 on reads and writes, preserve modes, and require both sender and receiver opt-in for sensitive-looking paths. Staging is outside the immutable source checkpoint.

Run-through now persists upstream provenance in the durable outbox across cache eviction and restart, captures unknown state explicitly, propagates provider ETags, and applies atomic conditions to put, copy, multipart completion, and delete. Unknown or legacy unguarded entries fail closed. Runtime accounting counts mutations known to have committed locally even when propagation or outbox completion returns an error; its quota view excludes upstream-only and separately cached objects. The Python wrapper now invokes `ResolvedBinary.path`; handoff writes its file and returns the same JSON on stdout. CLI, TypeScript, and Python expose explicit workspace destroy and collection operations.

Fresh focused Go checks pass, including regressions for all mutation forms and the delta defects. The first full gate attempt caught a compile break in the conformance cleanup fake after the upstream client gained conditional-delete and response-ETag behavior; that fixture was repaired. Final verification then passed: `make test-all` completed with 85 TypeScript tests, 51 Python tests, Go/race and four conformance runs, plus the WASM integration test. The CLI cleanup lifecycle was exercised end-to-end through both wrappers. `make standards` also passed, including all 65 script tests, TypeScript checks, generated-artifact reproducibility, and Go coverage of 69.74% (6,113/8,765 statements). Live-provider tests were skipped because provider configuration was unavailable.

The local performance baseline is recorded in [`tool-performance-baseline.md`](benchmarks/2026-09-27/tool-performance-baseline.md), with raw run data beside it. It covers first upload, request latency, RSS scaling, multiple connections and sessions, workspace handoff/checkpoint/archive costs, and parallel workspace preparation. It is machine-specific local evidence; provider/network and external adoption performance remain open.

The assessment's product conclusions still apply: no new release has been published, the incomplete `v0.2.0` tag remains untouched, provider/live and outside-monorepo install checks remain outstanding, and no external pilot was performed. The cache refresh no longer lists the backing store, but its in-memory eviction planner still scans and sorts the index. Phase 2 runtime-floor work remains open; Phase 4 remains conditional on naming a persistence target.
