# Internal workspace file publication

**Date:** 2026-11-11. **Status:** C.2 qualified for the local internal native
macOS ARM64 profile, with GOFLAGS=-p=1 for integrated Go gates. This is an internal native slice, with a
process-local failure barrier. Public SaveFile/ResolveSave and MCP editing remain
disabled. The [canonical plan](../development/storage-foundation-plan.md) owns status.

## Implemented boundary

The [native contract](../contracts/workspace-editing-contract.md) retains one
owning Workspace, its existing Runtime instance and Store, and explicit
host_quiesced coordination. The private editor bridge uses the declared writable
roots, current read/write authority, owner-bound observation token and existing
lifecycle/registry/capture locks. No UI or provider invocation is included.

Runtime reads current state without index adoption and admits the replacement
against its existing byte/object limits and multipart reservations. Store repeats
the complete basis comparison under its exclusive lock before publication.
Comparison binds exact path, absence, native root/parent/file identity, digest,
size, mode, nanosecond modification time, generation and index metadata hash.
The current trusted identity performs the comparison. The old and new file each
fit the configured whole-file limit, at most 64 KiB.

A dedicated quota view reuses QuotaStoreProvider. Natural and indexed escaped
objects in primary and secondary namespaces return actual size/existence without
adopting host files or reconciling metadata. This also removes the hidden
HeadObject adoption path when usage refresh examines pending multipart targets.

Existing indexed files retain content type and cloned user metadata; the existing
checksum algorithm is recomputed over the new bytes. Unindexed/absent leaves start
without stale index metadata. Existing permissions are preserved; new files use
0644. Every successful publication gets a fresh generation, coherent natural-form
index entry and cleared multipart-only identity fields. The rooted replacement
requires an existing exact real parent and never creates directories.

Internal outcomes describe file effect rather than durable save receipts.
Pre-effect refusal is not_committed. An opaque atomic-writer error is unknown.
Known file publication followed by failed stat/index completion is committed with
IndexReady false. Unknown or index-incomplete effects set a Store-local blocker
and stable usage revision; Runtime refresh cannot silently clear the pause, and
new observations refuse. This state does not survive reopen or qualify capture
and cleanup barriers. Those guarantees remain D.1/D.2.

## Review and regressions

Parent review corrected a new-file byte-quota bypass and an incorrectly combined
old-plus-new body limit. Independent review corrected the quota view's treatment
of escaped objects as absent. All corrections have focused regressions.

Tests cover exactly one concurrent commit, stale basis and owner/path/expiry
refusals, write authority, mode and managed-generation changes, cancellation
before effect, missing parents, aliases, metadata/checksum preservation and
unindexed host-file observations. Runtime tests verify byte-quota refusal,
escaped overwrites, successful creation using a same-target multipart object
reservation, exact shrink accounting and no phantom usage after refresh.

An actual index-save failure is induced after payload publication by replacing
index.json with a directory. The outcome remains committed but not index-ready;
refresh, later Runtime writes and new observations stay blocked. A private
per-Store payload-writer seam defaults to the real rooted writer. Its regression
performs the requested atomic replacement and then returns a synthetic late
error through the real PublishWorkspaceFile branch. The result stays unknown,
and later publication refuses without invoking the writer again. A narrow
Runtime test wrapper separately verifies unknown-state refresh behavior. These
are local fault tests, not process-kill or durable recovery qualification.

## Verification

| Check | Result |
| --- | --- |
| Focused tests/races and independent review | Twenty new C.2 regression methods; races passed in all four affected packages. Final source and fault/accounting rechecks accepted. |
| Integrated Go race suite and standards | All targets passed with serialized Go packages. Complexity, state-machine, file-size and coverage baselines unchanged. Coverage: 73.81% (14,845/20,113 statements). |
| Scripts, extension and agents | 246 script tests, 27 extension tests and 40 agent tests passed; one opt-in model test explicitly skipped. |
| Conformance | Memory, filesystem, runtime-memory and runtime-filesystem passed. |
| Artifacts/API | Native/WASM builds and 13-page API generation/verification passed. |

The generated-output check initially failed because the package build preceded
the final WASM rebuild. Regenerating the distribution copied the current WASM;
check-generated then matched a fresh build, and the remaining extension/agent
targets passed. The preceding Go race, coverage and standards results remained
valid: no Go source changed during the integrated gates. The original failed log
and successful continuation are both retained; no full-gate failure is hidden.

The [receipt](../assessment-evidence/2026-11-11/workspace-editor-publication.json)
records source/artifact hashes, the combined source state and gate results.
The qualified native binary is separately preserved locally. After validation,
the 270 pre-existing package distribution omissions are restored. No provider
call, commit, publication or deployment occurred.

A separate active chat repaired preparation/fingerprint code and portable scope
assertions in the same checkout during this slice. Those edits are preserved;
C.2's qualification must identify the combined source state rather than attribute
those repairs to this work. One focused run saw an incomplete preparation
refactor and failed to compile; the unchanged C.2 tests passed on the stable
recheck. No baseline is raised and no production/test timeout is changed. The earlier C.1 and W25 qualified binaries remain separately saved.

## Remaining work

D.1 adds bounded durable intent, request identity, published proof and terminal
receipts before enabling native saves. D.2 supplies bootstrap/reopen recovery,
capture/removal guards and process-kill tests. MCP adapters follow the qualified
native API. Cross-host/Linux execution and model editing retain separate gates;
the two earlier strict CLI model attempts remain failed.
