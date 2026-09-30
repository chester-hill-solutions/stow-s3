# Native filesystem guarded-save increment — 2026-09-30

This receipt belongs to [W02](storage-foundation-plan.md). It records the bounded
native filesystem increment, not completion of the foundation or a deployment.
Local verification runs on macOS arm64; Linux is a supported native code profile
but still requires its own physical-host evidence.

## Implemented boundary

- `OpenFilesystem` owns a locked local directory; constructor failure releases its
  lock, Close retains contents and Reset is refused. Existing `Open` behavior stays
  compatible.
- `ReadForSave` and `SaveObject` compare managed generation, SHA-256 content and
  logical metadata/checksums. Native conditions remain runtime/resource bound.
- Optional request keys bind immutable save meaning and bounded metadata receipts.
  Same-key replay preserves newer object contents; `ResolveSave` works after reopen
  without reapplying the intended body. Missing evidence remains unknown.
- Prepared and publishing phases distinguish proven no-attempt from uncertainty.
  Matching object markers/fingerprints and complete original result metadata are
  verified and resynchronized before a committed receipt. Versioned receipt
  checksums detect structurally valid accidental corruption before trusting phases;
  they provide integrity checking, not hostile-host authentication. Unresolved/corrupt pending evidence pauses all managed
  mutations, including legacy and multipart routes.
- Current operation authority governs keyed saves and resolution. Qualified FS
  mutations refresh authoritative object and multipart accounting; replay precedes
  new-effect quota admission and reports the retained original result. Opening still
  refuses preexisting usage above its configured quota; obtain an admissible
  resolution handle before resolving after restart.

## Limits

The fixed journal admits at most 1024 retained terminal entries, 8 MiB of encoded
bookkeeping, 64 KiB per entry and one pending slot. Admission reserves the largest
encoded phase before publication; no required receipts expire automatically. This
is logical bookkeeping admission, not physical disk preallocation. Object and
receipt files are plaintext. Receipts preserve original metadata, not old bodies.

At the close of this core increment, the filesystem profile qualifies only
macOS/Linux native local ownership. Workspace,
custom public Store adapters, WASM, TypeScript, MCP and S3 do not expose this stronger
managed-save API. Tokens are not serializable or portable to a reopened runtime.
Arbitrary host edits, copied directory identity, shared-host fencing, pre-enrollment
ABA history, power-loss behavior, resource ACLs, encryption and ready-output holds
remain outside this evidence. Full store scans make workload measurement necessary.

## Verification

Boundary tests inject errors and close/reopen the directory; they do not kill a
process or emulate physical power loss. Completed local checks:

- Full `make standards`, with lockfile-pinned npm tools installed and existing
  dependencies reused. Includes full Go races, vet, coverage, quality/file-size/
  version/install/doc/ADR/script gates, TypeScript standards and generated output.
- Supplemental final filesystem/runtime/public guarded-save races after the last
  UTF-8 validation and test portability edits. Concurrent public filesystem writers
  passed ten race repetitions: one commit, one conflict, exact winner and usage.
- Four local S3 conformance profiles: memory, filesystem, runtime-memory and
  runtime-filesystem. The final validation refinements only affect keyed saves;
  ordinary S3 requests retain their prior behavior.
- All 103 TypeScript tests (including shared workspace contracts), all 74 Python
  tests, and one real WASM runtime test passed against locally built assets.
- Native/WASM assets rebuilt; generated package output is reproducible. Coverage
  is 72.81%, 8841/12143 statements, above the existing 6811-statement floor.
- Relative Markdown file links checked locally; no missing targets. Whitespace
  checks passed. No ratchet baseline was raised.

An initial sandboxed full focused run could not bind loopback listeners; the full
race gate reran with approved disposable listener access and passed. The first
standards attempt then stopped at missing TypeScript dependencies. Installing the
lockfile-pinned tools resolved that environment issue; the final complete gate
passed. Optional published carrier binaries were removed from installed test tools
so compatibility tests resolve the current repository build.

Verification logs are retained locally under
`/private/tmp/stow-foundation-2026-09-30-*.log`. This evidence closes the bounded native
persistent increment. W02 remains open for two real consumers, managed wrapper
adoption, further backend qualification and physical Linux evidence. W03 holds and
namespace budgets, W04 resource ACLs, and W07 encryption remain separate work.

## Native object MCP wrapper addendum

The next bounded W02 increment wraps the same core through the existing native
binary and Go MCP SDK. `mcp --object-dir ... --bucket ...` selects one host-owned
filesystem directory and bucket; tool inputs cannot choose paths, buckets,
backends or authority. Existing workspace/checkpoint MCP tools keep their profile.
Mixed profile flags are refused before opening storage.

Five tools expose read-for-save, mandatory-key save, resolution, observation release
and capabilities. The adapter retains actual opaque core conditions behind random,
key-bound tokens. Tokens expire after 15 minutes, are bounded to 256 and do not
survive restart. Reopen resolution uses the original request key without reapplying
the body. Read-only profiles may read/resolve; saves and bucket creation refuse.
Request meaning and comparison remain core-owned rather than duplicated in MCP.

Adapter bounds are 64 KiB decoded bodies, eight admitted tool calls, 128-byte tool
names, 256 KiB raw tool arguments and 256 KiB complete encoded tool results. Hosts
may narrow bodies/observations. Native stdio incoming lines have a 1 MiB limit.
Validation-error expansion is included in tool-result accounting. Minimal encoding
or oversize fallbacks preserve committed/unknown effect outcomes and replay status.
These are payload/admission limits, not process RSS or arbitrary JSON-RPC ID/parser
limits; filesystem records may be materialized during preflight size checks.

Completed local verification on macOS arm64:

- Real SDK tests cover stale content/metadata, exact-metadata ABA, observed absence,
  replacement, replay after later writes, original receipts after reopen, old-token
  refusal, current read-only authority, concurrent writers and exact winning bytes.
- Observation tests cover release, expiry, capacity, oversized reads and token
  cleanup. Response tests cover encoding errors, escaped schema-error expansion,
  effect preservation and bounded busy responses while negotiation still works.
- Real binary stdio tests negotiate both MCP 2025-11-25 and 2026-07-28. They discard
  an application reply, close/reopen and resolve; verify stale refusal and read-only
  denial; and prove EOF/SIGINT release directory ownership without deleting data.
- Focused adapter/CLI races and full `make standards` passed. The complete gate
  includes existing workspace MCP tests, full Go races/vet, TypeScript standards,
  generated output and documentation/ADR/install/version/quality gates.
- Native/WASM builds and all 103 TypeScript, 74 Python and one real WASM test passed.
  Coverage is 73.26%, 9106/12429 statements. No ratchet baseline was raised.
- Final documentation checks passed: 5 commands, 17 verbs, 63 flags and 15 checked
  documents; ADR index retains 16 decisions and two statuses. All 668 checked
  Markdown file links resolve, and whitespace checks passed.

Logs are retained locally under `/private/tmp/stow-object-mcp-2026-09-30-*.log`.
This closes the bounded native object MCP increment; it does not close W02.
Discarded-reply/orderly-reopen tests are synthetic evidence, not killed-process or
physical power-loss qualification. Linux execution and two real downstream consumers
remain open. WASM/in-process TypeScript/S3 managed-save APIs, resource ACLs,
encryption and historical body holds remain separate work. See the
[object MCP contract](object-mcp-contract.md) and [current plan](storage-foundation-plan.md).
