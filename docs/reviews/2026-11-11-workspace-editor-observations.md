# Native workspace editor observations

**Date:** 2026-11-11. **Status:** C.1 qualified for the local native macOS ARM64 profile, with
GOFLAGS=-p=1 for integrated Go gates.
This record covers the owner-bound read foundation only. It does not qualify
guarded publication, durable save receipts, recovery or an MCP editing surface.
The [canonical plan](../development/storage-foundation-plan.md) owns status.

## Local boundary

The [frozen contract](../contracts/workspace-editing-contract.md) attaches one
WorkspaceEditor to the existing owning Workspace, Runtime and Store. The current
slice exposes open, read, observation release and close. SaveFile and ResolveSave
remain future work; the generic guarded-save capability stays unsupported for
the workspace backend.

The host explicitly selects host_quiesced coordination. Read and writable roots
are separate; writable roots must be inside the declared read scope, but observing
a path does not grant permission to save it. Existing sensitive-path exclusions
and natural-path confinement apply. A configured resource Policy is unsupported;
coarse read authority is required, including for read-only owners.

The narrow Runtime/Store bridge captures complete bounded bytes,
native identity, file metadata and the stored index generation together. It does
not adopt a host file into the index or create a new generation on a read. The
cache retains comparison state and fingerprints rather than file bodies. Bounds
are at most 64 KiB per file, 64 live observations and five minutes per observation.

Missing leaf observations require an accessible exact real parent. A symlink,
special file, spelling collision, unreadable parent or changed root must refuse
instead of reporting absence. An existing file-save journal marker also refuses:
the C.1 build has no journal decoder or recovery behavior to interpret it.

Owner lifecycle, registry mutation/capture gates, the existing Runtime mutex and
the Store mutex serialize observation. These locks do not fence arbitrary host
writers. Existing coordination lock files may be created; payload, workspace
identity, object index and registry records must remain unchanged by observations.

## Review and repairs

Parent review replaced retained serialized index metadata with a fixed SHA-256
fingerprint and replaced absolute directory inspection with rooted descriptor
traversal. The implementation uses an opaque identity comparison interface to
keep platform file types out of the generic storage package. Read scope remains
separate from write scope; shared normalization counts distinct roots.

The independent review accepted the lifecycle/registry/capture/Instance/Store
ordering, absent-leaf rules, regular-file bounds, policy/authority refusals,
marker guard and no-adoption behavior. A focused test recheck covers private and
Unicode paths, folded spelling, symlink parents/leaves, directory/special nodes,
missing parents, empty/at-limit/over-limit bodies, cancellation, root replacement,
cache capacity/expiry/incarnation, read-only owners and owner-close concurrency.
Snapshots check registry record, manifest and index bytes remain unchanged.

The first full gate hit the sandbox's local-listener prohibition. The approved
rerun then exhausted the standalone filesystem package's ten-minute total budget
while its current allocation subtest was only two seconds old. Separately, the
checkpoint active-expiry fixture's 100 ms lifetime expired during constructor
setup. The filesystem allocation test never calls C.1.

A sequential isolated replay passed all four allocation cases in 4.46 seconds
with 8.42–8.75 MiB allocated against a 10 MiB limit. The unchanged active-expiry
fixture passed 20/20 in 6.06 seconds. Source review confirmed that reader lifetime
starts before leases, manifest validation and indexing, so setup can consume that
small deadline. These observations support scheduling/contention sensitivity;
they do not justify weakening a production bound or claiming the parallel gate
passed.

The successful integrated run used GOFLAGS=-p=1 to serialize Go packages with reviewed
permissions for test listeners. All assertions, package timeouts and baselines
remain unchanged. No test or product source repair was made for these failures;
the serialized full race suite passed. The previously timed-out filesystem
package completed in 212.981 seconds under the unchanged ten-minute limit. The
default parallel run remains a recorded failure, not a qualified configuration.

## Verification

| Check | Result |
| --- | --- |
| Focused native observation/lifecycle tests and races | Nine TestWorkspaceEditor cases passed under the race detector; affected Runtime/workspace package commands passed. |
| Independent review and focused repairs | Accepted production and final test scope; no remaining blocker. |
| Integrated standards, build/API generation and agent tests | Passed with Go packages serialized. Native/WASM build and 13-page API generation passed; 246 script tests, 27 extension tests and 40 agent tests passed, with one separate opt-in model test skipped. Coverage: 74.59% (14,582/19,550 statements); baselines unchanged. |
| Existing four conformance profiles | Memory, filesystem, runtime-memory and runtime-filesystem passed with GOFLAGS=-p=1. |

The [receipt](../assessment-evidence/2026-11-11/workspace-editor-observations.json)
records the native/WASM artifacts, nine source-file digests, exact test profile
and isolated failure measurements. The previous W25 artifact remains separately
preserved; the old model receipts are not reattributed to this build. Validation
recreated package distribution outputs; the 270 pre-existing omissions are
restored afterward. No provider call, commit, publication or deployment occurred.

## Remaining work

C.2 supplies scoped comparison/publication through the same Runtime quota state.
D.1/D.2 supply durable intent, index readiness, unknown-effect barriers and safe
reopen resolution. Those batches precede MCP editing qualification. Managed
comparison tokens alone are not a safe-save implementation.

Linux execution, second-host continuation, protected path policy and actual model
editing retain separate qualification gates. The two previous CLI model attempts
remain failed under their strict output contracts. Source research identified an
SDK structured-output path for a future caller qualification; no provider calls
are part of this native slice.
