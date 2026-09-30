# Scoped access and encrypted working storage

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Broad scoped/encrypted working storage is W04/W05/W07/W14/W15 and host qualification W17/W18. Cache remains one profile; the body supplies design rationale, not independent ordering. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** accepted direction; proposed contracts, not implemented or deployed.
**Authority:** [ADR 0016](adr/0016-scoped-access-encrypted-cache.md).
**Work order:** S1-7 access control, S1-8 encryption, S4-4 qualification in
[the canonical plan](plan.md).

## Goal and reuse

These are shared storage capabilities for objects, workspaces, checkpoints, transfers
and notifications. Cache deployment is an initial qualification profile. The subsequent
[portable recovery decisions](portable-recovery-plan.md) add unified project/customer
budgets and a recovery reserve per trusted storage namespace under S1-4/S1-9; access
policy and accounting must agree on that namespace identity.

Authenticated callers access only permitted cached data. Protected persistent records
are encrypted and authenticated. Allowed retained objects remain usable during origin
outages, subject to data TTL, permission expiry and key availability. Qualify native,
Cloudflare Workers and Railway with one shared contract and suitable host adapters.

Extend `internal/authority`, `internal/runtime`, existing SigV4 authentication and
`internal/runthrough` caching/consent/retry paths. The current authentication hook
returns allow/refuse, not a principal/policy: add that contract rather than interpreting
a caller-supplied tenant header as identity. Reuse `internal/storage/fs/fs_records.go`
and `internal/atomicfile` for encrypted record publication; encrypt persisted records
and temporary publication paths, not just HTTP responses. Inspect the existing Go/WASM
runtime and TypeScript persistence adapters before adding Workers bindings. Browser
persistence is not evidence of Workers support. ADR 0010 governs actual enforcement.

## S1-7: Scoped access contract

- Application identity integration resolves trusted principal/namespace; Stow supplies
  resource authorization, not accounts/login. Endpoint credentials and scoped native
  handles use the same policy. Developer bypass cannot widen effective grants.
- Freeze bounded policy schema/version, operation/resource matching, exact and prefix
  grants, inherited attenuation and explicit-deny precedence. Default-deny scoped profiles
  preserve existing legacy behavior. S3 logical keys and workspace paths use their own
  existing identity rules; never clean an S3 key as an OS path and authorize another key.
- Authorize before serving/decrypting cache hits and before origin fetch. Lists, pagination,
  counts, Head/range/conditional responses must not reveal forbidden resources. Copy
  needs source read and destination write; batches retain per-item outcomes. Multipart
  scope/ownership applies on every operation and after restart.
- Scope capture/export/import/delta, inspection and subscription/query/delivery too.
  Explicitly scoped checkpoints and exports are an accepted direction from the recovery
  interview: callers may select an authorized portion of a workspace, and the saved
  artifact declares that scope. A whole-workspace request with insufficient permission
  refuses; it never silently omits forbidden files while claiming completeness. Webhook
  dispatch rechecks retained event permissions. Owner/admin operations are distinct from
  ordinary resource access.
- A queued report that discloses any resource/version no longer permitted to its recipient
  pauses as a whole. Do not automatically send a redacted subset under its old identity.
  An explicitly published new scoped report has a new identity and current permission
  checks; the original remains inspectable and protected until release/discard. Query
  and blocker diagnostics must not disclose newly forbidden historical content or paths.
- Partition cache identity by trusted namespace, origin and relevant response variants.
  Equal bucket/key or ETag does not make tenants interchangeable. Shared origin credentials
  do not grant all local callers access. Background propagation retains originating grant
  identity and rechecks policy; separate live-write consent remains enforced.
- Define policy versions, admitted/in-flight update behavior, local revocation, expiry and
  cross-host synchronization bounds. Expired or locally revoked grants refuse offline.
  Central revocation during a partition is bounded by the declared refresh/expiry model.
  Classify origin authorization failure separately from transient failure; known forbidden
  reads never fall back to stale-cache success.
- Return typed denied/expired/unknown-policy outcomes with secret-safe diagnostics. Report
  only enforced capabilities on each surface. Host filesystem access needs external
  enforcement. Full Amazon ACL/IAM/SSE wire APIs stay unsupported until separately qualified.

### Agreed scoped checkpoint/export direction

A caller allowed to access `reports/` may explicitly capture or export that portion
without requiring access to unrelated `private/` data. Record the requested resource
scope and completeness within that scope; do not describe the artifact as a complete
workspace. Scope is an additional restriction, never a grant. Check source capture/
export authority and destination import/restore authority independently; copying a
checkpoint does not install the source's permissions.

Extend the existing versioned checkpoint/archive APIs with explicit scope admission
and manifests. Current `CheckpointOptions` has no resource selector and this behavior
is not implemented. Preserve the existing file/object identity rules and documented
internal/sensitive-file exclusions. A forbidden resource requested explicitly must
cause refusal rather than be removed silently. Diagnostic metadata, provenance and
recovery records must not reveal resources outside permitted scope.

Scoped diff/delta/restore semantics must preserve the boundary: absence outside the
declared scope is not evidence of deletion. Define scope compatibility and refuse
unsupported combinations/readers before emitting or applying enhanced artifacts.
Completeness within a selected scope does not prove all dependencies for unfinished
operations are available. The subsequent [partial recovery decision](portable-recovery-plan.md#agreed-partial-recovery)
keeps affected operations and their dependents blocked while allowing independent
operations to activate after their own checks. Dependency representation and eligibility
remain proposed contracts; missing inputs never widen permissions.

Verify authorized subset capture/export, refused full/forbidden-scope requests,
permission changes before export/import, protected metadata, destination attenuation
and preservation of resources outside a scoped restore/delta. Reuse existing capture,
archive validation and policy conformance paths; no separate sharing store is needed.

## S1-8: Encryption contract

- Freeze a versioned authenticated-record envelope: suite/key IDs, authenticated resource/
  namespace/version binding, protected content/metadata, allowed plaintext leakage and
  format/size limits. Preserve logical ETag/checksums and size independently of ciphertext.
- Use standard authenticated encryption. AES-GCM in Go's standard library is a candidate;
  respect nonce generation/per-key invocation bounds. Consider fresh object-version data
  keys with an injected wrapping provider for distributed use. Envelope/suite/provider
  choices stay proposed until reviewed and tested across hosts; never invent a cipher.
- Give each trusted storage namespace an independently managed encryption-key lifecycle.
  Host providers may serve several namespaces, but active key selection, rotation,
  revocation and retirement must be namespace-scoped. A key change for one project/customer
  must not invalidate another's retained data or unfinished operations. Define authorized
  key resolution by namespace/key version; a caller-supplied key ID is not permission
  to use another namespace's key. This does not imply separate physical key services.
- Store key references/IDs, not secret bytes. Define offline key access, missing/revoked
  key behavior, rotation, re-encryption and retirement. Tampering, substitution, truncation,
  unsupported versions and wrong keys refuse reads. No silent plaintext downgrade or
  automatic corrupt-state replacement from origin. Repairs and conversions are explicit.
- Enumerate persistence coverage: cache and authoritative records, metadata/indexes,
  temporary files, multipart parts, immutable outbox payloads and sensitive notification
  state. Protect in-scope paths and name residual plaintext. Count ciphertext overhead
  in budgets; retain old keys until their data is explicitly re-encrypted or removed.
- Start with bounded whole-record encryption; a range read may authenticate the whole
  bounded object before slicing. This does not qualify large-media streaming. A later
  chunked format requires position/order/length/finalization authentication and its own
  compatibility/nonce contract. Release no unauthenticated partial plaintext.
- Ordinary workspaces remain plaintext working directories. Decrypted materialization
  is explicit. Encrypted archives require a separate versioned envelope over existing
  verified export/import; encrypted disk records do not make exports confidential.
  Keys, credentials and host policy never travel implicitly. Existing plaintext stores
  need explicit conversion or a new encrypted store; old readers refuse new formats.

### Agreed independent namespace key lifecycles

Routine rotation selects a new active key/version for new protected writes while
preserving authorized reads of existing retained versions. Reprotect retained records
with verified, recoverable publication before retiring keys they still require.
Logical saved-output identity, content digests and recovery operation identity must
remain stable when the encrypted representation changes. Enumerate dependencies across
objects, checkpoints/encrypted archives, multipart staging, outbox payloads and sensitive
notification state rather than checking the current object head alone.

Emergency revocation is a distinct explicit provider/administrative action. Stow must
stop resolving/using that revoked key under the declared propagation contract; affected
operations become blocked, without plaintext fallback or silent use of an old revoked
key. Keep required ciphertext/recovery records until authorized release/discard even
when they cannot currently be decrypted. Revocation may make the affected namespace's
data unrecoverable; it does not revoke keys already copied outside the provider or
erase prior plaintext disclosures. Do not claim otherwise from key policy alone.

Core supplies key references, dependency inspection and bounded re-protection/recovery
contracts; hosts supply key custody and persistence. Key availability on another host,
backup/recovery procedures and version mapping still need to be specified. Explicit
encrypted recipient handoff is accepted below; no source storage-key bytes are
automatically copied with saved data.

Verify two namespaces with separate key lifecycles, rotation during reads/restart,
interrupted re-protection, retained old checkpoints/parts/payloads, premature retirement
refusal or actionable provider dependency diagnostics, and emergency revocation.
Unaffected namespace operations must remain usable; affected work reports a permitted
key blocker and stays protected. Provider-enforced deletion/revocation outside Stow's
control is not prevented by core retention bookkeeping.

### Agreed recipient-encrypted handoff

An authorized scoped export may protect its selected data/recovery bundle for a
recipient's own keys. Hosts need not share a key provider. The source storage keys
remain source-side; after successful export, the recipient decrypts through its own
authorized key provider/identity and still checks local access and recovery-activation
policy. Recipient encryption is explicit, not inferred from a destination label or URL.

Use a standard authenticated envelope over the existing verified archive, with
explicit recipient/key binding and protected metadata; define suite, wrapping method
and sender authentication/trust during format qualification. The host identity/key
integration authenticates recipient/key selection. Do not substitute arbitrary
caller-provided keys without an authorized export/disclosure decision. Changing key
representation must preserve declared logical content/version and origin identities.

This grants the recipient an intended copy of the selected data, not the sender's
storage keys, account credentials, live-write authority or permission to automatically
resume external operations. Source-key revocation after export cannot retract a
recipient's already authorized decrypted copy or independent decryption capability.
Keep the source recovery set until verified recipient receipt permits its selected
dependencies to be released under the existing takeover/retention contract.

Current export is an unencrypted gzip/tar archive and verified import uses private
temporary files. An encrypted-envelope implementation must audit export/decrypt/verify
staging and protect in-scope temporary paths; do not claim confidentiality by encrypting
the final archive alone. Ordinary materialized directory workspaces remain the explicit
plaintext profile. No new archive inventory, identity or cleanup engine is needed.

Verify different source/recipient key providers, authorized subset disclosure, wrong or
untrusted recipient binding, ciphertext tamper/truncation, interrupted publication,
encrypted staging coverage and recipient import without source keys. Repeated import
still preserves operation identity and explicit activation; missing recipient keys
refuse decryption without plaintext fallback or grant widening. Key-loss backup/recovery
is specified separately below from a handoff successfully created for a known recipient.

### Agreed optional recovery-key backups

Allow an explicitly enabled encrypted backup to be prepared for a separately held
recovery key/recipient before failure. The resulting backup must be decryptable without
the original host or original namespace key service, provided the authorized recovery
custodian has the required recovery key material. The private recovery key is not
included in the backup. Reuse the recipient-envelope and verified archive mechanisms;
do not add silent escrow, key extraction or a hosted key-custody service to core.

Hosts own recovery-key custody, version retention and operational backup schedules.
Selecting the recovery recipient requires explicit authorized disclosure: holders of
that recovery key can decrypt the prepared backup. Support no implicit recovery recipient
or namespace-wide master key that silently spans other customers. Rotation of ordinary
storage keys must account for recoverability of retained backups and the independently
held recovery-key versions they require.

Recovery is possible only for backups actually prepared and retained for that key;
the feature cannot recover unreadable ciphertext after every usable key is lost.
Test recovery on a fresh authorized host with source data/key-provider paths absent.
Verify payload integrity, scope, namespace mapping, required key versions and destination
policy before exposing data. Stored source policies/progress are provenance, not current
destination authority. Pending external operations stay inactive until explicit activation
and confirmed takeover; decrypting a disaster-recovery backup does not establish either.

Ordinary source-key revocation does not invalidate an independently encrypted recovery
copy. Define recovery-key retirement and explicit backup deletion separately; do not
promise recall of ciphertext/keys copied outside Stow. The opt-in adds a deliberate
decryption route and must be documented in inspection and host capability evidence.

Verify recovery without the source provider, wrong/missing recovery keys, a disabled
recovery profile, tampered backup, rotated recovery-key versions, sensitive staging,
current destination permissions and inactive pending effects. A successful local key
lookup or archive checksum alone does not qualify disaster recovery.

## S4-4: Target profiles

| Target | Proposed profile and evidence |
| --- | --- |
| Native service | Existing Go object/cache service with scoped grants and encrypted records; prove restart, rotation, origin outage and denied reads of warm data on supported hosts. |
| Railway | Native/container service with persistent volume, injected secrets, authenticated networking, readiness and graceful shutdown. Start with one instance; prove retained ciphertext across redeployment and offline origin behavior. No shared-volume replica or global-edge claim. |
| Cloudflare Workers | First prove shared Go/WASM execution, then add minimal platform persistence, secret and request adapters. Candidate ciphertext acceleration uses Cache API; durable records may use R2 and transactional policy/journal state may use Durable Objects. Freeze topology from measured object/event needs. |

Workers Cache API is disposable per-location acceleration, not a globally replicated
journal. Authorize inside the Worker before returning/decrypting content; public CDN
shortcuts cannot bypass that gate. Platform persistence/key availability is distinct
from an origin outage. Store durable policy/event progress transactionally and test
host publication recovery. Reuse shared event identity and encrypted-format vectors.

Workers observations cover Stow-mediated mutations and explicit origin reconciliation,
not ordinary host-directory watches. Declare origin refresh/invalidation bounds; receiving
a webhook is not proof of instantaneous global purge. Native process/lock assumptions
and notification daemons cannot be copied into request-scoped Workers.

Railway persistence uses an explicit volume. Hosted endpoints require explicit bind,
authentication and TLS deployment configuration; current loopback/admin defaults are
not remote-readiness evidence. No cloud resource/customer-data operation occurs in this plan.

## Gap review before protected-cache qualification

These are source observations and contract/evidence gaps, not newly reproduced runtime
failures. Existing local correctness passes remain evidence of their covered profiles.
Review outcomes belong to the canonical IDs below; this is not another work queue.

| Priority / area | Current evidence and required review | Canonical owner |
| --- | --- | --- |
| First: authorization failure vs outage | `internal/runthrough/cache_read.go` falls back to cached data on non-not-found upstream HEAD errors; listing similarly falls back on upstream errors. Distinguish permission denial/revocation from transient unavailability in the protected profile, with cached data already present. Require denied requests to expose no forbidden data. | S1-7, S0-2 |
| First: durable freshness/deletion | `internal/runthrough/cache_policy.go` reconstructs retained entries without an expiry and starts TTL on a later read. Decide whether TTL measures residency or origin freshness; persisted freshness deadlines must not silently extend on restart. Define deletion/tombstone handling, offline stale-read limits and origin reconciliation on each host. | S1-7, S4-4 |
| First: bounded memory/network work | `refreshFromUpstream` buffers the complete origin object, even when filling for a cold Head request. `ListObjectsV2` collects matching local/origin objects before paging. Existing inbound-body/handler limits do not bound these origin-response/index costs. Measure encrypted-fill peaks and paginated namespace scale; enforce profile limits before allocating unbounded state. | S1-5, S1-8, S4-4 |
| First: keys, policies and restart | Key provider, resource policy, expiry/revocation and encrypted-format contracts are proposed. Freeze crash, unavailable-key, rotation/retirement, clock and policy synchronization behavior; include metadata/temp/multipart/outbox paths and explicitly retained plaintext. | S1-7, S1-8 |
| Next: origin/local write ownership | Separate-cache mode treats local writes as authoritative, while other paths revalidate origin-derived copies. Define read-only edge default, stale/conflicting write outcomes and what background retries may do after policy/key changes. Multiple caches do not acquire a merge or distributed-write guarantee from a shared origin. | S0-2, S1-7, S4-4 |
| Next: concurrent load and isolation | Shared runtime operation locks, simultaneous cold misses, slow origins and eviction need workload evidence. Decide safe per-namespace limits and whether coalescing fills is needed; no coalescing across different authorization/origin identities. Prove one tenant or failing origin does not exhaust all capacity. | S1-5, S4-4 |
| Next: notification consistency | Durable notifications/webhooks are planned. Define event relation to committed object versions, observer gaps/coalescing, backlog pressure, ordering and receiver retries. Old paths must not leak after ACL changes; Workers only observes managed changes/origin reconciliation. Consumers prevent feedback loops. | S1-6, S1-7, S4-4 |
| Next: host publication/lifecycle | Railway persistence, shutdown/readiness, endpoint/admin scope and Workers shared-core/durable state are not qualified. Test redeploy, eviction, interrupted publication and rolling key/policy changes using each host's actual persistence model. Remote exposure requires an explicit profile, not widening loopback defaults. | S4-4, S2-3 |
| Required before release: compatibility/upgrades/evidence | New scoped/encrypted formats need old-reader refusal, explicit plaintext conversion and rollback behavior. Publish a per-host/API capability matrix distinguishing Stow policies/encryption from AWS ACL/IAM/SSE APIs. Current 0.3.0 is unpublished; Linux runtime, provider and anonymous-install gates remain separate from local passes. | S1-7, S1-8, S2-1–S2-5, S4-4 |

First concrete exercises: warm a protected key and simulate an origin authorization
denial; restart a retained entry across its freshness deadline; cold-fill the largest
allowed object under concurrent encrypted reads; and rotate a key/policy during restart.
Use synthetic data and explicit profile outcomes. Reproduce a gap before changing
existing compatibility behavior; qualify fixes at the implementation revision.

## Ordered slices and acceptance

1. Freeze policy/envelope contracts and per-host capability matrix. Build shared vectors
   covering two principals reading the same warmed key, expiry/revocation offline,
   list/copy/multipart/capture/webhook scope and legacy-profile compatibility.
2. Extend common runtime/cache/API enforcement and actual background operation gates.
   Prove denied requests neither fetch origin nor leak retained data. Run appropriate
   race/auth/conformance checks with supported interfaces identified.
3. Implement bounded encrypted native records. Verify protected disk paths, wrong/missing
   keys, tamper/substitution/truncation, publication failure/restart, rotation, metadata/
   validators and ranges. Run required standards/release gates for that profile.
4. Qualify Railway using synthetic fixtures on an explicitly selected deployment. Check
   mounted persistence, secrets, authenticated transport, restart/redeploy and origin
   outage. Actual resource creation/deployment is a later concrete action.
5. Prove Workers shared-core compatibility and minimal scoped/encrypted read-through.
   Reuse policy/crypto vectors, then test platform eviction/restart, namespace isolation,
   durable policy/journal state and memory/object budgets. Record unsupported features.
6. Record revision/configuration/workload-specific S4-4 evidence per target, including
   policy synchronization, freshness, keys, failures and cost. No production-cache status
   follows from local tests or the existence of a deployment template.

## Primary sources checked 2026-09-29

- [Go authenticated encryption and random-nonce GCM](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)
  specifies authentication and nonce/key bounds. The pinned local toolchain exposes the
  API too; module lookup warnings during documentation lookup are not a build pass.
- [NIST GCM specification](https://csrc.nist.gov/pubs/sp/800/38/d/final) describes
  authenticated encryption; check current revisions when freezing the suite/profile.
- [Workers filesystem](https://developers.cloudflare.com/workers/runtime-apis/nodejs/fs/)
  documents in-memory temporary storage and unsupported watching/permissions.
- [Workers Cache API](https://developers.cloudflare.com/workers/runtime-apis/cache/)
  documents location-local cache content and deletion.
- [Durable Object storage](https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/)
  documents transactional strongly consistent state, a candidate persistence adapter.
- [Railway volumes](https://docs.railway.com/volumes/reference) documents persistence
  and the current restriction on replicas for services with volumes.
