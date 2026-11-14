# Workspace editing through CLI and MCP

**Date:** 2026-11-12. **Status:** E.1/E.2 locally qualified for CLI/MCP and SDK
interoperability. This extends the locally qualified
[native foundation](2026-11-11-workspace-editor-recovery.md) for the macOS ARM64
host_quiesced profile. The [contract](../contracts/workspace-editing-contract.md)
owns behavior and the [canonical queue](../development/storage-foundation-plan.md)
owns overall status. No model, Linux, second-host or power-loss qualification is
implied by these SDK tests.

## Implemented surface

The host explicitly selects --workspace-files --workspace-edit, writable roots
and --coordination host_quiesced. --recover-file-saves explicitly selects owning
recovery for every journal-bearing root, including terminal-only state. There
is no fallback between ordinary resume and recovery. Object, resource-only,
selected-checkpoint and browser combinations refuse editing before serving.
Invalid modes have no filesystem effects, and a live owner prevents a second
owner before manifest/index/registry-entry or payload initialization writes.

NewWorkspaceEditing uses exactly one registered Workspace, Runtime and editor.
ResumeWith/ResumeWorkspaceForFileRecovery retain registered authority and
storage limits; capture bounds do not replace them. Existing resource readers
coexist with that owner. Calls use the existing eight-active admission and
deadline clamp. Close drains admitted work, clears volatile observations and
releases the owning Session and reader lease. Pending evidence remains durable.

| Tool | Native operation |
| --- | --- |
| stow_workspace_file_read_for_save | Complete bounded bytes or observed absence, plus an opaque comparison token |
| stow_workspace_file_save | One text/base64 body and one basis/replacement choice, with a persisted request key |
| stow_workspace_file_resolve_save | Original retained result; no new payload write |
| stow_workspace_file_release_observation | Release a volatile basis without deleting receipts |

Editing arguments cannot select another root, registry, workspace or bucket.
The decoded body cap is 64 KiB; raw arguments and the complete encoded tool
result each fit 256 KiB. Complete base64 remains available when optional UTF-8
text would exceed the result limit, including control-character expansion and
duplication into text and structured content. Undisclosed observations are
released on a failed response. Empty bodies and zero permission modes retain
their distinct representations.

Save/resolve return original generation, canonical relative receipt key and
cloned metadata without body bytes. Errors disclose no raw host paths or tokens.
Known pre-native save refusals are not_committed. Missing or uncertain receipt
evidence stays unknown. Known terminal outcomes and committed-error markers
survive response fallback. There is no second quota, observation or request
journal implementation in the adapter.

## Review and focused behavior

Twelve new Go test methods cover adapter and CLI behavior: flags and incompatible
modes, stdio save/recovery, scopes, empty/maximal bodies, raw-argument and encoded
response limits, metadata isolation, native observation release, existing
pending-journal recovery, Session release and live-owner refusal. Existing
read-only/resource/checkpoint and object-mode checks remain required.

Independent review accepted the ownership, mode, lifecycle and protocol paths.
Focused repairs preserved explicit mode zero, original user metadata, committed
markers in bounded fallback and not_committed for provable pre-native refusals.
The corrected paths passed focused race, quality and file-size checks before
the final candidate build. No baseline, assertion or timeout was weakened.

## Independent SDK scenario

One TypeScript SDK fixture connects two separate Client/StreamableHTTP transports
to one actual loopback HTTP subprocess and its single editor. Both clients
observe the same basis with distinct tokens. Concurrent distinct requests yield
exactly one committed save and one not_committed stale-basis conflict. A separate
explicit checkpoint request succeeds after the known committed file save.

A custom client fetch forwards one real save POST, consumes the completed server
response as a test oracle, then throws before the client receives the reply.
The test observes one intercepted save response with no automatic retry during
the rejected call; the caller does not reissue the save. The other client confirms the
published bytes. The parent kills the server, waits for SIGKILL, changes the file
while no owner is running, then restarts with explicit recovery. Resolve returns
the original nonempty generation and metadata-only result while later bytes,
device, inode and nanosecond mtime remain unchanged.

This is completed-response loss followed by process death, not a transport
failure at every publication phase. The native D.2 record separately qualifies
seven backend SIGKILL boundaries and the honest unproven-publishing gap. No
production fault hook, provider request or execution engine was added for E.2.

## Verification and backout

The final focused SDK run passed against native SHA256
ca9fc0b1ba1d33032344a8b4d58354799a1923508313882c5c3bfc1e0de08a9e.
Native/WASM builds, package generation and 13-page API generation passed.
Integrated standards, agent fixtures and four conformance profiles passed.
The Go gates use GOFLAGS='-p=1 -count=1' to avoid stale cached coverage locations.

| Check | Result |
| --- | --- |
| Focused regressions | 12 new Go test methods and one independent SDK scenario passed; affected race, quality and file-size checks passed. |
| Independent review | Accepted backend/CLI and focused response/oracle repairs. |
| Full standards and Go race suite | Passed on unchanged frozen source, with existing baselines. |
| Coverage | Passed: 78.13%, 16,232/20,775 statements; zero obsolete source locations. |
| Scripts, extension and agent fixtures | 246 script tests, 28 extension tests and 40 agent tests passed; one provider opt-in explicitly skipped. |
| Conformance | Memory, filesystem, runtime-memory and runtime-filesystem passed. |
| Builds, API and generated output | Native/WASM, fresh package reproduction and 13-page API generation/verification passed. |

An initial SDK run failed while JSON-stringifying a diagnostic that included an
SDK client object. The diagnostic was corrected without changing assertions or
production code. Its failure excerpt remains in the tool session; its original
log was overwritten by the corrected run and is not retained separately. The
strengthened final success log is retained.

The [receipt](../assessment-evidence/2026-11-12/workspace-editing-mcp.json)
records eleven source hashes, the unchanged 860-file Go manifest, artifact
hashes and exact evidence boundaries. The native runtime/storage foundation and
WASM/browser artifacts remain unchanged from D.2. The exact 270 pre-existing
distribution omissions were restored after verification.

The [usage guide](../guides/workspace-file-saves.md) records explicit observation,
save, recovery and caller-owned checkpoint continuation. Disabling editing must
preserve all journals and dependent workspaces. Ordinary startup refuses marked
roots; do not delete evidence to make an older binary or mode open one.

Resource-only mode continues to expose no tools. The browser bundle is unchanged;
this batch adds only its editing-mode refusal. Model editing, arbitrary external
writers, Linux execution, cross-host use and a product release retain their
separate gates. No provider call, commit or deployment occurred.
