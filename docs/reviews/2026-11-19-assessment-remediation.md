# Assessment remediation, 2026-11-19

**Status:** implemented in this tree for 0.4.3 and measured 2026-11-20. The one
deliberate remainder is object-surface outcome classification: the S3-object MCP
tools still default to `unknown` for a pre-flight refusal, while the workspace
save surface reports `not_committed` and carries `next`. This is a dated
remediation record, not a work queue: the
[consolidated plan](../development/storage-foundation-plan.md) remains the only
dispatch authority.

## Findings this plan answers

Measured against the implementation and its runtime, not against this
repository's prose.

| # | Finding | Evidence |
| --- | --- | --- |
| 1 | A committed MCP file save makes the workspace unopenable to every lifecycle verb | `workspace resume`, `workspace serve` and `workspace destroy` fail after an edit-and-save with `workspace file observation unsupported`; `.stow/file-saves` is never removed by any code path |
| 2 | A second editing session cannot start on a workspace that has saved before | `Workspace.OpenFileEditor` refuses at `CheckWorkspaceFileSaveEditorOpen` for the same reason |
| 3 | Default AWS client multipart uploads are refused | `aws s3 cp` above the 8 MiB single-PUT cap fails with `NotImplemented: CRC64NVME multipart full-object checksums are not supported`; `--checksum-algorithm CRC32` works |
| 4 | Seeding aborts on a sensitive-looking path instead of excluding it | `.env`, `*.pem`, `id_rsa` stop a prepare with "is excluded by default"; `include_sensitive_inputs: true` copies the secret |
| 5 | Workspace file read/save is capped at 64 KiB with no flag | an 84 KB file returns `payload_too_large` on read and `invalid` on save |
| 6 | Pre-flight refusals report `outcome: "unknown"` | pushes agents into pointless `resolve_save` cycles |
| 7 | A staged archive re-import is not idempotent | `workspace import` of the same archive fails with "checkpoint already exists" |

## Work items

| ID | Item | Effort | Track | Status |
| --- | --- | --- | --- | --- |
| W-A1 | A journal is a state, not a lockout (findings 1, 2) | 2–3 d | A | done |
| W-A2 | Sensitive-path exclusion instead of abort (finding 4) | 1 d | A | done |
| W-A3 | `--max-file-bytes` for workspace editing (finding 5) | 0.5–1 d | A | done; `stow_workspace_inspect` reports the effective bound |
| W-B1 | CRC64NVME multipart full-object checksums (finding 3) | 4–5 d | B | done on the direct stores; run-through does not forward the claim yet |
| W-C1 | Outcome, timestamp and import polish (findings 6, 7) | 1–1.5 d | A | done for the workspace save surface: outcome codes, `next`, `omitzero`, import idempotency, admin-flag deprecation and the streaming-SigV4 message. Object-surface outcomes remain as noted above |

Tracks A and B do not touch the same files and can run in parallel. Ship one
release, 0.4.3.

### W-A1 — A journal is a state, not a lockout

**Rule.** Opening a workspace classifies its journal as absent, intact or
blocked, using the predicate the barrier already encodes
(`CheckWorkspaceFileSaveBarrier`): matching identity, only journal documents, no
pending record. Intact opens; blocked refuses with `ErrWorkspaceFileBlocked`;
absent opens as before. Object mutations on a handle that has not loaded the
journal apply the same rule. An editing session adopts an intact journal instead
of refusing it.

**Implemented.** `internal/storage/workspace/path_read.go` owns the
classification; `workspace.New` calls it; `Destroy` inherits it. ADR
[0022](../adr/0022-a-file-save-journal-is-a-state.md) records the decision.

**Regression gate.** `cmd/stow-s3/mcp_workspace_editing_test.go`:
prepare → MCP editing save → close → resume → object write → destroy, all in one
process-level test. It fails on the previous tree with the reported message.

**Acceptance.** The reported repro no longer reproduces. A pending or unknown
journal still refuses ordinary open, mutation and destroy. A second editing
session starts on a workspace with settled receipts. No code path deletes
journal evidence.

**Not done, deliberately.** `workspace recover` was not added. The blocked case
is already resolvable through `mcp --workspace-edit --recover-file-saves`, and a
lifecycle verb would duplicate that resolution rather than reduce it. The
refusal names recovery instead of "unsupported observation".

### W-A2 — Seeding excludes sensitive paths and says so

Replace the abort with a policy on `PrepareOptions` / `WorkspaceTaskManifest`:
`skip` by default (reported in `excluded`), `include` for today's explicit
`include_sensitive_inputs: true`, `refuse` to preserve the current fail-closed
behaviour. Apply one policy function at all four sites (`prepare_seed.go`,
`prepare_git.go`, `prepare_git_budget.go`) rather than four edits.

Tests: a project with `.env` prepares and reports it; `include` copies it;
`refuse` still aborts; a Git input containing a key file behaves the same way.
Docs: `docs/contracts/task-manifest.md` and the release note, because it changes
a default.

### W-A3 — Make the editing bound configurable

Add `--max-file-bytes` to MCP workspace editing and plumb it to
`WorkspaceEditOptions.MaxFileBytes`, with positive-integer validation and the
effective bound reported in `stow_workspace_inspect`. The read-only resource cap
stays fixed and documented, so there is one bound per surface.

### W-B1 — Accept the checksum AWS clients send by default

Record `CRC64NVME` at create instead of refusing; verify and persist per-part
checksums; compute the concatenated value in the existing assembly pass, verify
`x-amz-checksum-type: FULL_OBJECT`, and report both on complete, HEAD and GET.
`storage.PartInfo` gains an additive checksum field, so the part record is a
schema change with a pre-change fixture test. Run-through forwards algorithm and
type upstream.

Tests: published vectors, part-order and single-part edges, an SDK test at 12 MiB
with default checksums, corrupt-value and corrupt-part negatives, conformance
corpus cases, and the real-CLI profile extended beyond 8 MiB.

**Interim if the store work slips:** make the refusal name
`--checksum-algorithm CRC32` and document it, so a user is not stuck at
`NotImplemented` with no next step.

### W-C1 — Small correctness and polish batch

- Pre-flight refusals report `not_committed`; `unknown` is reserved for
  `ErrSaveRequestUnknown` and uncertain publication.
- `Modified` and `ExpiresAt` use `omitzero`, not `omitempty`.
- A repeated `workspace import` of the same archive returns the existing
  checkpoint; different content for the same ID is a conflict.
- Resolve `--allow-public-admin`: remove it or make its help state the behaviour.
- The streaming-SigV4 refusal names `STREAMING-AWS4-HMAC-SHA256-PAYLOAD` as
  unsupported and says what to do.
- `/_stow/inspect` either documents loopback-only or stops returning host paths.

## Gates per change

`make standards`; `make test-conformance` across the four backend lanes; `go test
-race` over `internal/s3api`, `internal/storage` and `pkg/stow`; `make test-node`;
`node scripts/check-coverage.mjs --baseline` once new tests land;
`node scripts/check-file-size.mjs`; `check-doc-commands` for every touched guide;
`check-adr-index --write-digests` for a new ADR.

Release: bump `internal/version`, the npm package and the Python package; run
`check-version`; add `docs/releases/v0.4.3.md` and a CHANGELOG entry; then the
existing native/wheel/packed-MCP gate. No provider publication without an
authorized interactive step.

## Risks

| Risk | Mitigation |
| --- | --- |
| The journal classification weakens crash-safety | Only intact opens; blocked keeps every refusal; the kill-helper fixtures and a serve-during-edit test stay in the suite |
| CRC64 combine differs from AWS | Published vectors, a real-CLI cross-check, and a corrupt-value negative |
| A part-record change breaks old data | Additive protobuf field with a round-trip test on a pre-change fixture |
| A seed default change surprises callers | Keep `refuse`, and call the change out in the release note and manifest contract |

## What this plan does not add

No new dependency for CRC64NVME (stdlib `hash/crc64` plus one assembly pass), no
second journal mechanism, no parallel seed-copy implementation, and no lifecycle
verb for a state the editing surface already resolves. Every item above is
expressed as a change to code that exists.
