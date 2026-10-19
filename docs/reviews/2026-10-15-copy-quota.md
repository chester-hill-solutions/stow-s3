# COPY quota review — 2026-10-15

Follow-up: the byte-quota reopen defect below is now corrected; see the
[fix and measurements](../benchmarks/2026-10-15-reopen-byte-quota/README.md).
The remainder records the investigation at its original baseline.

Reviewed branch `codex/core-copy-quality` at `73eb36c`, based on freshly fetched
`origin/main` `219d1dcfff9807b04660a40313b32127e5be2933`.

## Disposition: no demonstrated COPY admission race in the supported profile

The suspected gap was between runtime source HEAD (quota admission) and backend
COPY (source capture). It is observable with a second writer bypassing the
runtime, but that does not establish a defect in the supported ownership model:

- `runtime.OpenWithStore` takes ownership of its store. Runtime COPY holds
  `Instance.mu` through HEAD, admission, backend publication and accounting.
- Staged uploads prepare outside that lock, then reacquire it before quota
  revalidation and publication. Preparation alone does not replace the source.
- Managed filesystem directories have an exclusive owner. Raw object-record edits
  are outside the public profile; ADR 0020 specifies quiescent reconciliation for
  internal integrations.
- Editable workspace host writers must be quiescent for a consistent capture;
  see the workspace read contract in `docs/compat-contract.md` and the public
  `Workspace.RefreshUsage` comment.
- The base-only custom-store conditional COPY fallback remains explicitly weaker.
  This review does not widen that contract or reintroduce GET/PUT emulation.

## Evidence added

`internal/runtime/copy_quota_concurrency_test.go` exercises the real memory and
filesystem backends, retaining their optional capabilities through embedded test
wrappers. It uses channels to finish a larger source's preparation between source
HEAD and backend COPY. Both writes are made through the same owning runtime.
There are no timing sleeps or direct backing-store mutations.

Each of four cases (two backends, with/without ETag source condition) verifies:

1. A one-byte source is admitted under an eight-byte cap while a seven-byte
   replacement has six additional bytes reserved.
2. Preparation of that replacement finishes during COPY's source observation.
3. COPY publishes the original one-byte source and its ETag.
4. The replacement then publishes; actual object contents and accounting total
   exactly eight bytes across two objects.
5. A subsequent COPY of the larger source over the destination is refused with
   `ErrQuotaExceeded`, preserving the destination's original bytes.

The final test passed 100 repetitions under the race detector: **400 cases**.
An earlier equivalent version also passed 100 ordinary repetitions. This guards
that interleaving; it is not a claim that tests exhaust all possible schedules.

## Validation and costs

Passed:

- `go test ./...`
- `go test -race ./internal/runtime ./internal/s3api ./internal/storage/... ./internal/runthrough`
- `go test -race ./internal/runtime -run TestCopyQuotaWhileSourceOverwriteIsStaged -count=100`
- `make test-conformance`: memory, filesystem, runtime/memory, runtime/filesystem
- `go vet ./internal/runtime`
- Go quality and file-size ratchets, formatting and `git diff --check`

The full documentation-link check reports six missing built website targets in
unchanged `site/agent.md`; checking that file from `73eb36c` reproduces the same
six findings. The new report's local links pass.

No production code, packaged binary, dependency, format, API or storage contract
changes. Production code is unchanged from `73eb36c`; the restored backend COPY
path remains in place. No new runtime costs or compatibility risks are introduced,
so new performance measurements are not applicable. The added test performs small
filesystem writes and syncs, adding test-suite work only.

## Next confirmed finding: combined quota is not enforced on reopen

While tracing reconciliation, a separate defect was reproduced twice on the
current branch using an exclusively owned native filesystem directory:

1. Open with `MaxBytes: 16`.
2. Store a six-byte object and a six-byte multipart part targeting that existing
   object. Both operations are admitted through the runtime.
3. Close, reopen the filesystem store, and bind a runtime with `MaxBytes: 8`.
4. Expected: `ErrQuotaExceeded`, preserving the stored object and upload.
5. Actual: open succeeds with twelve bytes against the eight-byte cap.

`scanStoredUsage` in `internal/runtime/state.go` compares committed and reserved
bytes separately against the limit, without checking their combined total.
Normal write admission counts their sum. This contradicts the shared multipart
byte-budget contract on reopen. This is a quota-admission defect, not demonstrated
data loss; ordinary subsequent writes still consult the combined budget.

The [probe source](2026-10-15-copy-quota-reopen-probe.go.txt) and
[actual failure output](2026-10-15-copy-quota-reopen-probe.log) are preserved.
To rerun, temporarily copy the source into
`internal/runtime/copy_quota_reopen_probe_test.go` and run:

```sh
go test ./internal/runtime -run TestQuotaReopenCountsObjectsAndPartsTogether -count=2
```

Remove that temporary test afterward: it deliberately fails until the next fix.
It is stored as text so the passing regression suite remains usable.

No fix for this separate finding is included. The next independently reviewable
change should add aggregate, overflow-safe reopen admission and regression
coverage for below/exactly/above the combined cap, preserving objects and uploads
on refusal. It should not change the backend COPY implementation.
