# Validation log — architecture-overview / stow-s3 / run 1

What Phases 3 and 5 did, what they found, and what remains open.

## The bar

- **Phase 3, candidate validation** — one fresh reviewer per proposed candidate, who did not
  produce it, re-deriving every cited trace, evidence path, caller, interface fact and depth
  verdict from source.
- **Phase 5, independent verification** — one fresh verifier per retained `mapped` record, who did
  neither produce nor validate it.

## What ran

The run took two sittings. The first ended when the subagent provider reported "Free usage limit
reached" after the wave-2 workers; the second resumed once agents were available again, and a
third round closed the coverage gap.

| Phase | Agents | Outcome |
|---|---|---|
| Reconnaissance | 4 | complete |
| Work wave 1 (5 units) | 5 | complete |
| Coverage critic 1 | 1 | complete, 10 proposals merged |
| Work wave 2 (4 units) | 4 | complete |
| Coverage critic 2 | 0 | failed twice: a deprecated model, then the harness limit |
| Work wave 3 (6 units) | 6 | complete — the auth boundary, both clients, both readiness records, the WASM bridge, both MCP servers |
| **Phase 3 — candidate validation** | **31** | **31 of 31 candidates validated** |
| **Phase 5 — independent verification** | **27** | **27 of 27 mapped records verified** |
| Final-clean coverage critic | 0 | never ran |
| **Total** | **79** | |

### Three deviations from the pattern, all deliberate

1. **Validation ran in priority order, not fingerprint order.** The pattern's deterministic rule is
   fingerprint order. With a real budget ceiling and an unknown remaining quota, importance is the
   better key.
2. **P1 records were verified before P2/P3 candidates were validated.** This maximises the number
   of records that pass *both* phases under a ceiling. It is no longer a trade-off: every record
   has now passed both.
3. **The run resumed after the harness limit reset**, which is why 79 agents ran against a budget
   declared at 45. The budget was a setup parameter, not a contract with the repository.

## What validation found

**No false positives.** All 27 `mapped` verdicts and all 4 `rejected` verdicts were upheld. Not one
candidate was demoted or refuted outright. The map was accurate about *which* things are modules
and seams.

**Three verdict-level corrections:**

| Record | Change |
|---|---|
| `mod:pkg/stow:filesystem-backend-entry` | depth `shallow` → `mixed` — about half the body is delegation, but the deletion test does not show the complexity vanishing |
| `mod:packages/stow-s3-py:python-client` | depth `deep` → `mixed` — `session.py`/`ready.py`/`bin.py` own decisions, but `workspace.py` is a veneer and `__init__.py` a barrel |
| `mod:pkg/stow:registry-policy-recovery-holds` | confidence `medium` → `high` after the trace, interface, caller set and deletion test were independently established |

**Twenty-seven claims were refuted outright.** Each was a Phase 2 assertion the source does not
support. They are the strongest evidence that the two review phases earned their cost.

| # | Record | Claim that was refuted | What the source shows |
|---|---|---|---|
| 1 | `…:workspace-lifecycle` | The registry-resolution rule is spelled three times. | `ResolveRegistryDir` is a single definition and the CLI calls into the package. The real duplication is the resolve-then-open *composition*, whose copies differ (`openRegistry` creates the registry; `openRegistryReadOnly` and `List` do not). |
| 2 | `…:workspace:registry` | `Prune` uses `AllStrict`, so it surfaces a corrupt entry. | `Prune` reads through `All` and skips silently too. `AllStrict`'s readers are `RegisterWithLimit`, `countRegistryEntries` and `SetRegistryPolicy`. |
| 3 | `…:workspace-preparation` | The seed budget is charged twice for a Git input. | Charged once. The pre-fetch admission runs against a discarded value copy. |
| 4 | `…:workspace-preparation` | A moving ref could change the tree between `ls-tree` and checkout. | `resolveGitInput` pins the tree to a commit SHA before any read; that cannot occur. |
| 5 | `…:conditional-copy-seam` | The two fallback paths are not equivalent. | All three fallbacks (s3api, run-through, runtime) are the same head-then-copy shape with the same weaker guarantee. |
| 6 | `…:delta-document` | The effective ceiling is the raw read bound. | The content bound binds: 16 MiB encodes to ~21 MiB, under the 32 MiB read bound. |
| 7 | `…:logical-snapshot` | The snapshot does not read the tree's mtimes. | It does read them into an internal manifest entry; they never reach the returned object. |
| 8 | `…:logical-snapshot` | Trace sink is `checkpoint_archive.go:225`. | That is the export/handoff path, unreachable from the capture entry point. Re-pointed to `checkpoint_publish.go:35`. |
| 9 | `…:native-cli` | The whole environment half is invisible from the CLI. | `STOW_OFFLINE`, `STOW_MODE` and `STOW_ADMIN_TOKEN` *are* named in their own flags' help. Only the upstream half and the consent variable are invisible. |
| 10 | `…:checkpoint-archive-handoff` | A digest mismatch against the reference raises `ErrHandoffDigestMismatch`. | That sentinel is for the caller's out-of-band `ExpectedSHA256`; the reference mismatch is a plain error. |
| 11 | `…:run-through-adapter` | The outbox prepare step follows the local write. | The intent is prepared *before* the local write and committed after. |
| 12 | `…:runtime:instance` | Every verb consults authority and policy before taking the lock. | Upload-ID-addressed multipart verbs lock first, then `checkUpload`. |
| 13 | `…:store-contract` | Nine optional capability interfaces. | **Eleven.** |
| 14 | `…:store-contract` | Seven concrete implementations. | **Nine.** |
| 15 | `…:checkpoint-store` | `in_progress` classification applies on every path. | Applied only on the request path; the direct capture path returns the raw error. |
| 16 | `…:workspace-readiness-record` | A reader tells the two readiness records apart by the absence of a capabilities object. | Both records carry a capabilities object. Only the key name and vocabulary differ. |
| 17 | `…:workspace-readiness-record` | The session record suppresses credentials from stdout. | Both writers fall back to credential-bearing stdout when no descriptor is supplied. |
| 18 | `…:session-readiness-handshake` | The same record yields 0 or the real value depending on which client reads it. | Go emits `maxConcurrentRequests` unconditionally, so both readers get the real value. |
| 19 | `…:python-client` | The TypeScript argv supports `--read-only`. | No such flag in either client. |
| 20 | `…:typescript-client` | The package exports `ContentDbContentAdapter`. | The real name is `IndexedDbContentAdapter`. |
| 21 | `…:typescript-client` | Every operation after close throws code `closed`. | The `dataDir` getter and the `s3` field are unguarded. |
| 22 | `…:wasm-host-bridge` | `content-object-host.ts` dispatches 16 ops. | 15. |
| 23 | `…:wasm-host-bridge` | `content-object-host.ts` is a caller of the bridge. | It is the reverse-protocol host the bridge calls, not a caller. |
| 24 | `…:mcpstorage:workspace-checkpoint-server` | It exposes five checkpoint tools. | Five tools total: one workspace inspect plus four checkpoint verbs. |
| 25 | `…:mcpstorage:object-store-server` | The guarded-save workflow is ADR-described. | No ADR describes the MCP object profile; it is documented in `docs/implementation-2026-09-30.md`. |
| 26 | `…:workspace-s3-facade` | The MCP object host depends on the facade descriptor. | The MCP object server opens its own in-process runtime; it does not consume the descriptor. |
| 27 | `…:registry-policy-recovery-holds` | The standing policy is applied at the registry root only. | It is scoped per registry directory; these entry points simply pass an empty team. |

Twenty-seven rows — every one refuted by source. The remaining corrections were line, scope and
caller-set fixes, including several
**missing callers** the Phase 2 workers had not found: the CLI's `doctor` in the run-through
record, the two `workspace serve` spawners in the CLI record, ten caller files for
`OpenFilesystem`, three direct registry callers, and the browser WASM loader plus two isolate
harnesses in the WASM record.

## What remains open

Not confidence — **coverage**.

- **37 of 52 units were never assigned a worker.** The highest-value gaps: the seams inside
  `internal/storage` (staged writes, guarded writes, put-checksum, the propagation outbox); the
  durable-file primitives `internal/atomicfile` and `internal/rooted`; and `internal/authority`,
  `internal/policy` and `internal/capacity` — the three modules the runtime record names as
  co-owners of decisions it also makes.
- **No coverage critic ran after wave 2**, so nothing independently states what waves 2 and 3
  missed. The wave-1 critic's 10 proposals are the only external check on coverage in the run.
- **No final-clean critic ran.**

## Integrity checks

- **814 structured citations** across the 31 promoted records resolved against the repository:
  every path exists, every cited line is in range. **0 failures.**
- **95 inline prose references** in the reports resolved the same way. **0 failures.**
- All 31 records validate against `RECORDS-SCHEMA.json`; the ledger validates against
  `LEDGER-PROFILE.json`. 30 of the 31 record fingerprints are claimed by exactly one ledger
  unit. The exception is `mod:internal/s3api:s3-http-server`: the `S3 HTTP surface` unit that
  corresponds to it is marked `covered` and carries an empty `result_fingerprints`, so no unit
  claims that record.
- **6 citation fixes** the verifiers requested were applied, each re-checked against source first
  (`apply-corrections.cjs`).

## Reading the result

Every record in `records.json` passed validation and, if mapped, independent verification. The
caveat is coverage: the map is silent about 26 units, and that silence is not a judgement that
those areas are sound.

The target repository was never modified. All 79 agent invocations were read-only, and the run
wrote only inside its own directory.
