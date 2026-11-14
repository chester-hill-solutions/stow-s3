# Native workspace save recovery

**Date:** 2026-11-11. **Status:** W26.D.2 qualified for the local native profile.
The local native macOS ARM64 profile uses `host_quiesced` coordination. The
[contract](../contracts/workspace-editing-contract.md) owns the guarantees and
the [canonical plan](../development/storage-foundation-plan.md) owns release
status. CLI/MCP editing, Linux execution, cross-host use and model qualification
remain separate gates.

## Ownership and recovery

Ordinary owning open claims the existing exclusive Session before Store
initialization can write. Registered resume requires an existing real root and
metadata directory. Session locks refuse symlink targets, validate path identity
after acquisition and use close-on-exec so spawned helpers cannot retain a
writer's claim. New workspace creation retains the existing ownership rules.

Any journal-bearing root, including a terminal-only journal, requires explicit
`ResumeWorkspaceForFileRecovery`. It relooks up the registered root, identity and
bucket under the existing mutation/capture gates, claims one Session, and reads
manifest, index and journal through pinned descriptors. The folded path map is
rebuilt only in memory; object entries are not adopted or reinitialized. Recovery
does not register again or rewrite the working-directory record. Closing a
recovery owner or paused Store does not flush manifest/index state over evidence.

The same Runtime retains registered authority and normalized limits. A read-only
usage scan can initialize above quota, then restores those limits for new
admission. Read authority is required for resolution; write authority is not.
Configured resource policies remain unsupported. New registry entries persist
the multipart cap. Explicit recovery narrows an omitted legacy cap to one,
without imposing that migration rule on ordinary unmarked resumes.

## Public native API

`WorkspaceEditor.SaveFile` and `ResolveSave` delegate to the qualified private
core. Requests carry a canonical path, bounded bytes, request key and either an
observation token or explicit replacement. Results expose the original
generation, outcome, replay flag and cloned Object metadata without file bytes.
A refused save defaults to not_committed; an unresolved or missing receipt
lookup defaults to unknown. Known terminal outcomes survive bookkeeping errors.

Public errors preserve accessible conflict, request, quota, context, closed and
typed authority distinctions. Known committed cleanup errors retain
ErrMutationCommitted. Filesystem errors and wrapped/joined errors are sanitized
without host-path or token text. The public recovery constructor uses the same
mapping. Generic workspace guarded-save/durable-save capability flags remain
unchanged; this API is qualified specifically for workspace file saves.

External-package tests exercise basis saves, released-token replay,
conflict, metadata isolation, quota refusal and read-only owning recovery. API
error tests include joined errors and known committed cleanup results. The
native wrappers passed independent review and focused races before integration.

## Capture and cleanup

A shared static journal probe performs no locking, Store construction, index
adoption or repair. Valid terminal-only state is ready; pending, corrupt,
unsupported, aliased or foreign evidence refuses. The exact 8 MiB terminal
boundary remains readable: receipt enumeration observes EOF before rejecting a
further entry at the charged-byte limit.

Existing mutation/capture gates protect owner, external and recovery-hold
capture, registered removal, direct Forget, checkpoint cleanup and collection.
The hold path checks readiness before admitting a hold. Direct Store mutations
and ready object metadata also check readiness under the Store mutex. The
non-adopting quota scan and current complete-byte file resources remain readable;
immutable older checkpoints retain their existing leases.

Review found that a direct Store Destroy could release its probe lock, wait for
a concurrent save to leave pending evidence, and then remove that evidence. The
repair serializes readiness, closing and removal with the Store mutex, using
the same nonlocking close implementation as ordinary Close. A refused pending
Destroy preserves both evidence and the ability to resolve it. Already-removed
workspaces retain idempotent Destroy behavior. The contention test verifies
pending-save exclusion and usable resolution; it does not reproduce the exact
former probe-to-Close scheduling gap. Review separately verified the continuous
exclusive lock.

## Crash evidence

The subprocess fixture takes a real Session, publishes through the backend and
signals a durable phase boundary. Its parent verifies live-owner exclusion,
sends SIGKILL, checks the terminating signal, waits for process exit and reclaims
the Session before opening recovery. A live timer keeps the child from exiting
through Go's deadlock detector before the kill.

| Interrupted boundary | Required result |
| --- | --- |
| Prepared | not_committed, unchanged payload |
| Publishing before payload | unknown, retained barrier |
| Payload written before durable published proof | unknown, even when bytes match |
| Equal-byte payload before published proof | unknown, retained barrier |
| Durable published proof | committed through index-only repair |
| Index installed before terminal receipt | committed through retained proof |
| Terminal receipt before pending cleanup | original committed result |

Published, indexed and terminal cases compare payload inode and nanosecond mtime
across resolution. Terminal replay after later writes returns the original
metadata without replacing newer bytes. These subprocess backend tests and the
owning public-API composition tests are distinct evidence. They do not simulate
power loss, qualify an arbitrary external writer, or prove another host's
filesystem behavior. The rename-to-durable-proof gap deliberately remains
unknown and requires operator reconciliation.

## Verification

Focused process/race, bootstrap/guard tests, public API tests and independent
review passed. The full standards, agent-fixture and four conformance profiles
passed on the frozen source. The uncached coverage gate also passed.
No provider call, deployment or commit is part of this batch.

| Check | Result |
| --- | --- |
| Native regressions | 30 new functional test methods, excluding the child-process helper; seven SIGKILL scenarios. Existing full-capacity and Destroy-idempotence checks also pass. |
| Independent review | Accepted Runtime recovery, owner bootstrap, guards, process fixture and public API; race/idempotence findings repaired and rechecked. |
| Standards and Go race suite | Passed, with serialized Go package execution and unchanged baselines. |
| Final uncached coverage gate | Passed: 78.00%, 16,039/20,563 statements; no obsolete source locations. |
| Scripts, extension and agent fixtures | 246 script tests, 27 extension tests and 40 agent tests passed; one provider opt-in explicitly skipped. |
| Conformance | Memory, filesystem, runtime-memory and runtime-filesystem passed. |
| Builds/API/generated output | Native/WASM builds, package reproduction and 13-page API generation/verification passed. |
| Frozen source | 855 Go files; 40 changed/new files relative to the D.1 source manifest. |

The first coverage gate passed at 72.65% (16,040/22,079 statements), but its
profile contained 736 unique locations that did not match the current source.
Cached package results retained obsolete block positions. The additional gate
uses `GOFLAGS='-p=1 -count=1'` to execute the instrumented suite without test-cache
reuse. It passed at 78.00% (16,039/20,563 statements), with zero obsolete source
locations. Both logs are retained; the fresh result owns the final measurement.
No source, timeout, assertion or baseline was changed for this rerun.

The [receipt](../assessment-evidence/2026-11-11/workspace-editor-recovery.json)
records 40 source hashes, the unchanged 855-file Go manifest, native/WASM hashes,
logs and qualification limits. The exact 270 pre-existing distribution omissions
were restored after verification. Prior cached percentages are not used to infer
a coverage improvement or regression. Future changed-source coverage runs should
disable test-result caching until the gate's cache handling is addressed.

The prior D.1 qualified artifact remains preserved for comparison,
not automatic downgrade of a journal-bearing root. Disabling editing must retain
the journal and its dependencies.
