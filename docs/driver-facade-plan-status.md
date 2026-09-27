# Driver-facade plan: retired, and what survived it

**Date:** 2026-09-27
**Plan:** `/home/nathaniel-arfin/.opencode/plan/2026-09-25-stow-s3-driver-facade.md`
**Status:** retired. Not superseded by another plan; the goal is not being pursued.

This records why, so the plan is not re-derived by the next session that finds
it. The plan file lives outside the repository and nothing under `docs/` pointed
at it, which is how a two-day-old investigation came to need redoing.

## What the plan proposed

A stow-owned TypeScript client replacing `@aws-sdk/client-s3`, selected by a
variable discriminant:

```ts
const s3 = Stow.s3({ driver, addressing, isolation });
```

with `driver: "embedded" | "http"` narrowing the types, and `Stow.start`,
`withStow`/`openStow`, and `EmbeddedStow.open` consolidated into one entry point.

## Why it is retired

The goal is opposed by decisions accepted after the plan was written.

**Reimplementing S3 in the client is a stated design principle against it.**
`docs/agent-dx-plan.md` §3.2: "Language packages should configure existing S3
clients. They should not reimplement S3 operations." ADR 0002 makes the
version-pinned AWS SDKs the compatibility target rather than a convenience, and
its §Consequences requires the conformance suite to run *both* SDKs against both
backends. A first-party client does not add a second opinion to that suite; it
removes one of the two the contract mandates.

**Entry-point consolidation reverses two accepted ADRs.** ADR 0007: "Existing
`Stow.start()`, `Stow.connect()`, `withStow`, and `with_session` are unchanged
and additive. Nothing in this ADR is a breaking change." ADR 0003 says the same
of the process-owned and endpoint-owned contracts. Consolidation is a
major-version change to the client packages, on top of the one ADR 0009 already
records for `close()` becoming non-destructive.

**The plan's ADR slot was taken by a different decision.** Its step 3 called for
ADR 0007 to record the consolidation. ADR 0007 is now "The workspace is the
default; S3 is an opt-in facade" — an unrelated decision. The plan's own
decisions were therefore never recorded anywhere.

**It is partly obsolete on its own terms.** Its step 6, flipping a
`multipartEnabled` flag in `runtime.Open`, describes a flag that no longer
exists: `newInstance` derives multipart support from
`store.(storage.MultipartStore)`, and `MemoryStore` satisfies it, so multipart
already works through `Open` with no change.

## A naming collision worth recording

ADR 0007 and `docs/workspace-contract.md` §7 already define an "S3 Facade": a
loopback S3 endpoint bound to the same runtime instance as a workspace. The plan
meant a TypeScript *client*. The two are unrelated and share a name. Anything
reviving the client idea should pick a different word.

## What survived, and is worth doing on its own

Three findings from the plan were re-verified against the tree on 2026-09-27 and
are still true. None of them depends on the facade.

- **The shared corpus cannot referee a client, and does not yet cover the
  operations it should.** `conformance/corpus/cases.json` holds 15 cases over 7
  operations, with no case for a range read, a presigned URL, virtual-hosted
  addressing, `HeadBucket`, `DeleteObjects`, or five of the seven multipart
  operations. `conformance/CONFORMANCE.md` records virtual-hosted as pending.
  ADR 0002 §Consequences requires the corpus to be amended before behaviour
  changes, so this is owed work regardless of the plan.

- **Addressing was derived in two places that could disagree, and is now split
  across two fixes.** As of 2026-09-27 this finding is half resolved, and the
  remainder is tracked in
  [`Deploy-Anywhere Plan`](deploy-anywhere-plan.md) Phase 7 item 3 rather than
  here. `Stow.awsSdkV3Config` spread the caller's options and then set
  `forcePathStyle: true` over the top, so the option was in the public type,
  honored by `buildAwsSdkV3Config`, and discarded by the one function that
  mattered — and the existing test asserted the discarded value. Fixed in
  `59568ca`. The run-through upstream client was worse: `UsePathStyle: true` was
  hard-coded, so a provider serving only virtual-hosted addressing was
  unreachable and the failure was a DNS lookup with nothing in stow's output to
  explain it; `STOW_UPSTREAM_ADDRESSING` and `UpstreamConfig.Addressing` landed in
  `87b0a4a`. Still open: the local server's `STOW_BASE_HOST`, which is
  `docs/remediation-plan.md` open item 5, and the virtual-hosted corpus case that
  would keep any of it honest.

- **A latent asymmetry in the runtime constructors.** `runtime.Open` returned an
  instance whose initialization had never run. Fixed on 2026-09-27; it was inert
  at the time and the commit says so.

- **The corpus grew its first range-read cases, and the first one found a defect
  on `main`.** `range-partial-object` failed against the real
  `@aws-sdk/client-s3` because a 206 carried the whole-object checksum beside a
  partial body. Fixed in `e393166`. This is the plan's corpus finding paying out
  two days after the plan was retired, which is the argument for having kept the
  finding rather than the plan. Range is not the only gap: presigned URLs,
  virtual-hosted addressing, `HeadBucket`, `DeleteObjects` and five of seven
  multipart operations are still uncovered.

## If this is ever revived

It needs an argument that the pinned-SDK compatibility target can survive, which
means reopening ADR 0002 rather than choosing an implementation strategy. It also
needs a supersession path for `docs/agent-dx-plan.md` §3.2, which is a design
principle rather than an ADR and so cannot be cleanly retired by one. The
cheapest way to find out whether such an argument exists is to grow the corpus
first: the acceptance cases the workspace contract already specifies are the
honest measure of what the product does, and they are not all written.
