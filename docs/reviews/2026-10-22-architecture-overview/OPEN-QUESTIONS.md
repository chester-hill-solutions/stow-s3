# Open questions — architecture-overview / stow-s3 / run 1

Three kinds of open item: coverage the run never reached, facts the workers could not establish,
and claims no independent agent checked. The third is the largest and the most important.

---

## 1. Coverage never reached (32 deferred units)

Deferral reason, identical for all of them, recorded per unit in `coverage-ledger.json`:

> `budget_cannot_reserve_critics_and_validation`: the 45-agent budget was consumed by
> reconnaissance, wave 1 (5 units), wave 2 (4 units) and the wave-1 critic; the harness then
> reached its subagent limit, so wave 3 was never launched and no unit from it was assigned.

### Entry points with no unit — the highest-value gap

| Unit | Class | Why it matters |
|---|---|---|
| TypeScript client package (`packages/stow-s3`) | Module | One of two in-repo consumers of the CLI's flag spellings and readiness record. |
| Python client package (`packages/stow-s3-py`) | Module | The other one. Two independent implementations of one invocation contract. |
| WASM host bridge (`cmd/stow-wasm`) | Module | An entry point; implements the public `embedded.Store`, bypasses `pkg/stow`. |
| MCP tool servers (`internal/mcpstorage`) | Module | An entry point; workspace/checkpoint tools and scoped object tools. |
| SigV4 verification (`internal/auth`) | Module | **The authentication boundary has no unit.** Described only from outside, via `s3api`. |
| S3 request authenticator seam (`internal/s3api/auth.go`) | Seam | Three adapters reported by reconnaissance, unverified. |
| Session readiness handshake (`internal/ready`) | Data flow | The one value shared by the CLI and both clients; three implementations, no shared schema file. |
| MCP browser extension (`extensions/stow`) | Module | Bundled into the Go binary; its extension contract is unmapped. |

### Seams proposed and not examined

| Unit | Class |
|---|---|
| Propagation outbox (`internal/runthrough/outbox.go`) | Seam |
| Staged-write seam (`internal/storage/staged.go`) | Seam |
| Guarded-write seam (`internal/storage/guarded.go`) | Seam |
| Put-checksum and guarded-write capability seams | Seam |
| Embedded host bridge in the TypeScript client | Extension point |
| Policy source hook (`internal/policy/source.go`) | Extension point |
| Browser persistence adapter (`packages/stow-s3/src/browser-types.ts`) | Extension point |

The outbox, staged-write and guarded-write seams are the ones to examine first: the run already
established that `runthrough.Adapter` erases the guarded-write and save-request capabilities of
the store it wraps, and the outbox is the mechanism that makes propagation durable.

### Modules, flows and cross-cutting units proposed and not examined

`internal/embedded` (the facade), `internal/authority`, `internal/policy`, `internal/capacity`,
`internal/atomicfile` and `internal/rooted` (the durable-file primitives every store depends on),
`internal/parentwatch`, the native CLI flag and environment contract, the run-through write
propagation flow, the object PUT request flow, the storage-to-facade dependency chain, the storage
contract blast radius (Risk), the runtime byte-budget versus capacity-namespace budget dependency,
the conformance contract tree, the quality-gate tooling tree, the site build, the example
programs, and the browser content store and project library.

---

## 2. Gaps the workers raised that were never turned into units

Each of these came from a worker's own `gaps` output, was merged into the ledger, and was then
deferred with the rest.

- The optional put-checksum and guarded-write capability seams on `internal/storage`'s `Store`.
- The runtime's byte budget against the filesystem capacity-namespace budget (a two-authority
  dependency — the run found a *third* authority, the run-through cache budget).
- The native CLI flag and environment contract (proposed as a unit separate from the CLI module).
- The rooted file access primitive (`internal/rooted`), which every workspace read and write is
  routed through.
- The object-record protobuf header schema and its checked-in generated Go
  (`internal/storage/fs/recordpb/`).
- The workspace multipart staging area and its private part-record format — the one part of the
  workspace store that does **not** use `internal/rooted`.
- The machine registry's recovery-hold journal and its identity sidecar.

---

## 3. Facts the workers could not establish, and facts they flagged

### Documentation that contradicts the source (established, not inferred)

1. **The workspace manifest.** ADR 0008 §3 (`docs/adr/0008-workspace-backend-real-files.md:71`)
   and `CONTEXT.md:104` name a single `stow-workspace.json` at the workspace root. The source
   writes `<root>/.stow/manifest.json` and `<root>/.stow/index.json` at layout version 3
   (`internal/storage/workspace/manifest.go:88`, `:93`, `:21`). `docs/workspace-contract.md` §3.1
   and §4 are also stale: the escaped form is
   `<root>/.stow/keys/<sha256(bucket)>/<sha256(key)>`, and the contract shows one document at
   version 1. **Independently confirmed by the parent.**
2. **A moved document cited by code.** `parseBackend` (`cmd/stow-s3/main.go:103-108`) cites
   `docs/architecture/environment-implementation.md` M1.2 as the authority for refusing
   `--backend workspace`. The file is at `docs/history/architecture/environment-implementation.md`.
3. **A warning promised and not emitted.** `Config.AllowPublicAdmin`'s doc comment
   (`internal/s3api/server.go:45-49`) says the server logs a deprecation warning; the package never
   reads the field, and the warning is printed by `cmd/stow-s3/offline.go:54-56`.
4. **A verb count that disagrees with itself.** The workspace verb table's own comment says
   fifteen verbs; the table has seventeen (`cmd/stow-s3/workspace.go:119-137`).
5. **ADRs describing surfaces that do not exist.** ADR 0003's out-of-scope paragraph still
   describes a memory-only `pkg/stow`; ADR 0010 §1 says `EnvironmentPromote` was removed from the
   operation set while `internal/authority` still records it as `Ungated`; ADRs 0013, 0015, 0016,
   0017 and 0018 state surfaces absent from the source. These are facts about the documents, not
   defects in the code — but a reader following them is misled.

### Facts a caller can only learn by reading source

- The `STOW_*` > `S3_*` > `AWS_*` precedence, the live-write consent variable and the offline
  variable appear in no CLI help string or usage line.
- Two different signing regions exist (`--region` for verification and the readiness record,
  `STOW_REGION`/`AWS_REGION` for the upstream client) and only one is published.
- `--backend workspace` is refused, but the flag help says only "filesystem or memory".
- `doctor`'s two exit statuses (1 for a failed required check, 2 for a doctor that could not run)
  appear in no help text.
- The `mcp` command selects its storage mode by which flags were *visited*, not by their values,
  so `--object-dir` with an empty value still selects object mode.
- `Instance.Close` closes the bound store; the rule is stated only in a comment.
- The platform gate on `SupportsGuardedWrites` is invisible at the interface.
- `pkg/stow`'s registry-policy and recovery-hold verbs have no production caller; only the
  package's own test exercises them.

### Two claims whose status the run could not settle

- **Whether the TypeScript and Python clients agree with each other or with the CLI.** Both
  reproduce the invocation contract and the environment prefix regex independently. The run
  established that the duplication exists and that the two spellings of the registry-resolution
  rule already disagree inside `pkg/stow`; it did not compare the two clients.
- **Whether the `stow-s3 workspace` verb table and the `pkg/stow` public surface are the same set.**
  Seventeen CLI verbs, each a direct call into `pkg/stow`, but no unit enumerated the public
  surface's own verb set to compare.

---

## 4. Validation state

**Complete for every record.** All 31 candidates passed Phase 3 (a fresh reviewer who did not
write them) and all 27 mapped records passed Phase 5 (a second fresh agent that did neither).
Four rejected candidates passed Phase 3 and needed no verification.

- **27 mapped records verified.** No verdict was demoted; no `mapped` record was a false positive.
- **4 rejected candidates confirmed.** The single-adapter interfaces and the re-export barrels.
- **27 claims refuted outright**, plus line, scope and caller-set fixes, tabulated in
  `VALIDATION-LOG.md`.
- **814 structured citations re-checked, 0 failures.**

### What remains open

Not confidence — **coverage**. Twenty-six of 41 units were never assigned a worker:

- the durable-file primitives (`internal/atomicfile`, `internal/rooted`) that every store's atomic
  publication and confinement route through;
- the seams inside `internal/storage` — staged writes, guarded writes, the put-checksum capability,
  the propagation outbox;
- `internal/authority`, `internal/policy` and `internal/capacity` — the three modules the runtime
  record names as co-owners of decisions it also makes;
- the extension points — the browser persistence adapter, the embedded host bridge, the policy
  source hook;
- the peripheral trees — `site`, `examples`, `conformance`, `scripts`, `tools`, and the MCP browser
  extension;
- the native CLI flag/environment contract, the run-through write-propagation flow, the object PUT
  flow, the storage-to-facade dependency chain, the runtime byte-budget dependency, and the storage
  blast-radius risk.

### And one process gap

**No coverage critic ran after wave 2.** The wave-1 critic's 10 proposals are the only external
check on coverage in the whole run, and nothing independently states what waves 2 and 3 missed. A
final-clean critic also never ran.

### How to finish

Against `coverage-ledger.json`: the 26 deferred units. The highest-value order is the seams inside
`internal/storage` and the three modules the runtime names as co-owners, then the durable-file
primitives, then the peripheral trees. A coverage critic should run first to confirm the unit set
before more workers are spent.
