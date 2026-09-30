# Shared identity and storage admission contract

**Decision boundary:** 2026-09-29. This reference resolves the shared contract in
[W01 of the working-storage plan](storage-foundation-plan.md). It is not a second
work queue. Target requirements below govern guarded saves, scoped access,
retention and publication as they are implemented; they do not advertise new APIs
or certify a deployment. Existing direct `PutObject` and S3 behavior stays compatible.

## Current implementation and limits

- [Public object/options](../pkg/stow/types.go) expose content ETags and
  `IfMatch`/`IfNoneMatch`. The native memory/filesystem guarded surfaces add opaque
  process-local observations; public object records still do not expose generations.
  [Store adapters](../pkg/stow/store.go) require positive optional
  `ConditionalWriteStore` support before forwarding conditional writes.
- [Runtime writes](../internal/runtime/objects.go) check operation authority and
  perform quota admission and store mutation under the runtime mutex. Backend
  conditions are checked at the backend write gate. These locks qualify the
  participating in-process path, not arbitrary host writers or independent processes.
- [Workspace writes](../internal/storage/workspace/objects.go) assign a record
  version and write object metadata. Bytes can be published before a later manifest
  write fails. Generic failure therefore does not establish absence of effect.
  [Workspace lookup](../internal/storage/workspace/resolve.go) can reuse recorded
  metadata; a cached ETag is not fresh proof against every external file edit.
- [Authority](../internal/authority/authority.go) is currently an environment-wide
  operation mask. It is not resource policy, caller authentication or a trusted
  namespace registry. Protected-profile identities and policy are target work.
  `Workspace.Destroy` now checks retained destroy authority, including after Close;
  constructor rollback of newly created unfinished roots uses a private cleanup path.
  `EnvironmentPromote` remains exported and is the sole `Ungated` entry. Ambient
  host administrative `DestroyRegisteredWorkspace`/`Collect`/`DeleteCheckpoint`
  APIs have no principal binding;
  full resource/background policy compliance is not established by this lifecycle fix.
- [Checkpoint requests](../pkg/stow/checkpoint_request.go) bind retained receipts
  to workspace and options and can reconcile a saved checkpoint. They do not
  provide a general save receipt. Missing receipts after cleanup do not prove
  absence of a prior effect. [Conditional tests](../pkg/stow/conditional_put_test.go) exercise real
  memory/workspace runtimes and a runtime supplied as a custom Store.

The custom-store seam is retained with qualified optional support under the
amendment to [ADR 0010](adr/0010-an-enforcement-site-or-not-a-permission.md).
Capability declarations are necessary admission inputs, not proof that an arbitrary
implementation is correct. Each advertised profile needs behavioral evidence.

| Profile | Current qualified comparison | Persistence/access limit |
| --- | --- | --- |
| Native memory, managed read/save | Exact runtime/resource token; managed generation, SHA-256 content, logical metadata/checksum comparison and observed absence under store publication lock. | Volatile, single owning public runtime; no retained save receipt, namespace ACL or shared-store quota guarantee. |
| Native filesystem via `OpenFilesystem`, managed read/save | Runtime/resource token; generation, SHA-256 content, metadata/checksum comparison under the owning store lock. | macOS/Linux local directory; bounded request receipts and reopen resolution, exclusive process ownership. No resource ACL, host-edit CAS or copied-directory fencing. |
| Native object MCP | Retains the actual filesystem runtime/resource condition behind an expiring key-bound observation token; core performs save comparison. | Host fixes directory, bucket and read/write authority. Keyed saves and resolution use filesystem receipts; bounded calls, payloads and observations. Tokens do not survive reopening. No resource ACL or encryption claim. |
| Native memory, existing puts | Content ETag and expected absence under participating locks. | Volatile; environment authority, no resource ACLs. |
| Native workspace, existing puts | Content ETag and expected absence under participating locks. | Persistent bytes; arbitrary external-edit CAS and general durable save receipts unqualified. |
| Custom public Store | Conditional comparison only with positive optional support. | Adapter behavior/profile evidence required; support does not certify durability or richer guards. |
| Existing WASM/embedded wrapper | Forwarded ETag conditions with explicit older-host refusal. | Backend-dependent; no managed identity or resource ACL guarantee. |
| Protected native/Railway/Workers | Target, unimplemented qualification. | Trusted namespace/policy, encryption and durable recovery require their own gates. |

The bounded native managed-save slices qualify memory and the owned filesystem
profile. Memory is volatile. Filesystem request keys link a durable intent, object
record marker and retained terminal metadata; uncertain publication pauses managed
mutations until verified resolution. Same-key replay validates immutable meaning
and preserves later object versions. Old runtime conditions cannot be rebound after
restart; resolution uses the retained key instead. Missing receipts mean unknown.

These are legacy unscoped profiles: trusted namespace/resource ACLs, historical body
holds and declared-input ready publication remain target work. Workspace and custom
public stores refuse the richer surface even when they support ETag conditions.
Filesystem admission refreshes full authoritative accounting; no constant-time or
physical disk-reserve promise follows. Newly assigned generations detect managed
changes after enrollment, not all earlier ABA history or arbitrary host writes.
See [usage and fixed retention limits](portable-workspace-usage.md#filesystem-save-receipts-and-recovery)
and [dated local evidence](implementation-2026-09-30.md). The
[native object MCP profile](object-mcp-contract.md) exposes these filesystem
operations through a thin adapter; existing workspace MCP and WASM/in-process
TypeScript/S3 profiles do not expose the richer managed-save API.

## Trusted identity

A host authenticates a request and binds it to a trusted principal, namespace and
an attenuated handle. Core receives that binding through the host integration,
not through resource names or caller-controlled headers. An untrusted request can
select only resources within that handle. Principal and namespace identifiers are
opaque, stable host-assigned values; display names are separate. Core does not add
a hosted account service or manufacture authority from configuration.

The trusted namespace is the common policy, budget and encryption-lifecycle
boundary. Multiple principals may use one namespace; one provider may manage
several namespaces without joining their permissions or key lifecycles. A bucket,
workspace directory, model session or webhook subscription is not automatically
a namespace. Legacy unscoped operation remains an explicit separate profile;
opening an existing runtime does not silently create an ACL claim.

A resource identity is the typed tuple:

| Field | Meaning |
| --- | --- |
| Namespace | Trusted storage-policy boundary. |
| Collection kind and identity | Object bucket or registered workspace, identified within that namespace. |
| Resource kind and locator | Exact object key or confined relative workspace path. |
| Saved version, when selected | Immutable saved artifact/version identity; omitted for the current mutable resource. |

Object keys preserve existing S3 validation and exact key identity. Do not apply
filesystem normalization, case folding, URL decoding or path ancestry to keys.
Workspace paths use the existing confinement rules and a canonical relative-path
form; path scope matching uses complete components. `a/b` is below `a`, while
`ab` is not. Reject escape/ambiguous paths before admission. A mapping from an
object key to a workspace file is explicit; two representations do not silently
become the same authorization selector. A saved checkpoint reference retains its
workspace identity and explicit selected path/version.

Resource identity names what is checked. It neither grants access nor proves
existence. A digest, ETag, generation, checkpoint ID or encrypted envelope is also
not permission. Copy/export/import preserve original saved-content provenance,
while destination namespace/resource authority remains independently established.

## Scope and policy

A scope is a bounded canonical set of typed selectors under one trusted namespace:
exact resources; explicit object-key prefixes; or workspace path subtrees. Versions
are selected explicitly when historical content is involved. Object prefix `a`
uses key-prefix semantics; workspace subtree `a` uses path-component semantics.
Do not exchange those selectors implicitly. Normalize and deduplicate the set
before using it as immutable request meaning. Unprovable attenuation refuses
rather than guessing that a derived handle is narrower.

Effective protected-profile access intersects environment authority, current
principal policy and handle/resource scope, with explicit-deny precedence and
default denial. Upstream selection and live-write consent remain additional
attenuation requirements. Policy has a host-issued revision and a freshness/expiry
deadline; permission lifetime does not extend because cached data is available.
Local revocation and expiry apply offline. Cross-host revocation guarantees must
state their synchronization boundary and maximum stale-policy interval.

Authorize every resource that an operation reads, changes or discloses, including
copy sources/destinations, historical diff context, list/count/Head/range results,
multipart continuation, checkpoint operations and background retries. Authorization
precedes cache access, decryption and origin fetch. A protected exact/full artifact
request refuses when any required member is unauthorized; it is never silently
filtered into a differently scoped artifact. Enumeration returns only authorized
results with permission-safe pagination/counts. Scoped artifact completeness means
complete within its declared scope; absence outside that scope cannot authorize
deletion during restore.

### Implementing a scope

`internal/policy` implements the sections above. Four things in it are traps
rather than choices, and each is worth stating where a reader will meet it:

- **The two prefix semantics are not interchangeable.** An object key prefix `a`
  covers `abc`, because that is what an S3 prefix has always meant. A workspace
  subtree `a` covers `a/b` and not `ab`, because `ab` is a different directory.
  A prefix that silently widened into a subtree would grant a permission to a
  path nobody selected, so a selector carries its own kind and never matches the
  other one even when the locator text is identical.
- **Effective access is an intersection, and the obvious spellings of it are
  wrong.** `Authority.With` only adds, so intersecting by adding the policy's
  operations to the environment *widens* the result; applying `Without` to the
  wrong side withholds from the wrong set. `authority.Intersect` is the one
  implementation, for the same reason `IsSupersetOf` is one implementation.
- **An allow entry's mask is built from `None`, not `All`.** `All().With(x)` is
  still every operation, so an allow entry written that way grants everything
  its selector covers — a widening that is invisible in review because the call
  reads like "allow these".
- **An expired policy is neither a grant nor a denial.** Denying it would let an
  expired cache read as a permission decision; allowing it would extend a
  permission past its lifetime because the bytes were still cached. It is
  reported as unknown, distinctly, because those three answers need different
  responses from a caller.

Not yet implemented, and therefore not yet claimed: persistence of a policy
revision and its freshness deadline; enforcement outside the object path
(checkpoints, background retries, run-through propagation); scoped enumeration
counts; and any handling of a saved version selector.

### Where the runtime enforces this

`internal/runtime` consults a policy at one point per operation, and the shape of
that point is the enforcement.

`Options.Policy` is a `*policy.Set`, and a nil pointer means no policy is
consulted — the same shape and the same reason as `Options.Authority`, so an
`Instance` opened without one behaves exactly as it did before the field existed.
The policy is validated against the environment authority once, at open, rather
than at each check site, so the answer cannot depend on which operation asked. A
policy that turns out to be wider than its environment is **refused, not
clipped**: every operation fails with `policy.ErrWidening`. Clipping it silently
would leave the author believing a permission was in force when it is not.

Three check functions, and the distinction between them is the contract:

- `checkResource(op, resource)` — an operation on a named object. Every object
  operation goes through this.
- `checkUpload(op, uploadID)` — an operation addressed by a multipart handle. The
  resource is resolved from the instance's own record of the upload, *not* from
  the store, because the store's answer would have to be read before
  authorization in order to know what to authorize, and authorization comes
  first. An upload the instance cannot resolve has no established resource:
  without a policy that is the pre-policy behaviour, and with one it is refused
  as `ErrResourceUnresolved`, because a policy that cannot be evaluated must not
  fall back to allow — that would make the handle-based operations the one path
  around every selector.
- `check(op)` — operations that name no resource: the environment's lifecycle and
  the bucket namespace. There is nothing for a selector to match, so it consults
  the environment authority only.

`TestObjectChecksNameAResource` fails if an operation on an object reaches for
`check` instead of the other two. A policy that is never consulted is not a
narrower policy, and the failure mode of wiring only the obvious sites is that
the ones nobody thought about keep permitting everything.

Four consequences of enforcing per resource that are worth stating, because each
was a design decision rather than an implementation detail:

- **A copy is authorized on both ends, and the source is not optional.** A copy
  reads its source and publishes the bytes at a destination the caller chose, so
  checking only the destination let a write-only caller copy content out of the
  environment. The source is checked first, being the read that can disclose.
- **A list is authorized on the caller's own prefix.** That is sound only because
  the results are inside it: the store is asked for that prefix and nothing else,
  so authorizing the prefix authorizes the results. A grant covering `public/`
  therefore permits a list scoped to it and refuses the same list unscoped.
  Filtering a broad list down to a grant is the open half of enumeration and is
  not done here.
- **A batch delete authorizes each key, and a refusal leaves the batch
  unapplied.** Deleting the permitted keys and refusing the rest would be a
  partial answer to a question the caller asked as one.
- **A recovery hold needs both read and write on every object it covers.** A hold
  exists to stop those objects changing, and naming them is itself a disclosure.
  `ReleaseObjectRecoveryHold` names no objects, so under a policy it falls back to
  the environment authority rather than releasing: releasing is the irreversible
  direction, and a hold this instance cannot attribute is not evidence the caller
  may release it.

Retries and adoption recheck current destination policy; retained old grants are
provenance, not authorization. For initial coordinated profiles, a policy revision
change and mutation admission share the relevant gate: revocation effective before
admission blocks it; an already admitted effect may complete and is recorded.
No guarantee recalls transmitted bytes. Direct host filesystem access requires
external OS/sandbox enforcement.

## Comparison identity and guarded mutation

A new managed read returns a coherent body, logical metadata and an opaque,
versioned comparison identity. The identity binds the resource tuple, storage
coordination domain/incarnation and comparison mode. Tokens are expected-state
values, not authentication secrets or proof that a caller read the resource;
presenting one never relaxes current authorization. Parse/size limits and binding
checks are enforced by core, not language wrappers.

The initial strict managed mode compares an implementation-issued record generation
plus logical content and metadata identity. Every admitted managed replacement gets
a fresh generation, including same-byte and metadata-only writes. Covered logical
metadata is content type, checksum algorithm/value when present, and the canonical
user-metadata map; a workspace-file
profile also includes the permission mode when that mode is part of its saved
meaning. Transport timestamps, cache freshness, policy revisions and ciphertext/key
versions are not content metadata. Other observable fields must be explicitly
covered or declared outside the mode. Do not use ordinary map iteration or an
unspecified serialization to compute a comparison digest.

This mode detects managed A→B→A replacements within its qualified gate because
the generation changes. A matching content ETag alone detects neither that history
nor metadata-only replacement. Host edits that occur and return to the same final
bytes between observations remain undetectable; managed-generation mode must not
claim otherwise. A strict read against externally writable storage requires fresh
verified content/metadata and qualified coordination or explicit caller quiescence.
If coverage cannot be established, the strict operation refuses as unsupported or
unavailable; it does not downgrade to cached ETag comparison.

Comparison identities survive restart only when their coordination identity and
generation state are durably retained and verified. Restoring/copying a backup into
an independent gate creates a new coordination incarnation, invalidating old mutable
comparison tokens. Immutable saved-content identity may remain portable. Two
handles can share a mutable comparison domain only with an evidenced common gate;
a copied identifier or lease does not establish shared coordination.

Managed save requires exactly one explicit expectation:

| Expectation | Admission rule |
| --- | --- |
| Expected identity | Same bound resource/domain/mode and current generation/content/metadata. Otherwise conflict. |
| Expected absence | Resource absent at admission. This does not promise it never existed. |
| Explicit replacement | Deliberate unconditional mutation, still subject to current write authority and all other admission checks. |

Omission is invalid for the new managed surface. Existing direct unconditional puts
remain available. A stale or differently bound identity cannot overwrite, and a
conflict need not disclose forbidden current identity or bytes. The caller owns
rereading, merging and retry decisions. Strict multi-resource input-basis publication
uses this same identity and one qualified admission boundary; wrappers cannot
check inputs and then publish independently. Omitted input basis makes no freshness
claim; explicit historical mode checks authorized saved inputs instead of currentness.

## Outcome and retained request meaning

Effect state and reason are separate dimensions, not one ambiguous error string:

| Effect state | Claim |
| --- | --- |
| `not_committed` | Core can establish that this request published no effect. |
| `committed` | The named effect is established at the profile's declared commit boundary. |
| `unknown` | Available evidence cannot establish whether all or part of the effect committed. |

Refusal reasons include `invalid`, `denied`, `conflict`, `blocked`, `unavailable`,
`unsupported` and `quota_exceeded`. Ordinary admission refusal is
`not_committed`; an I/O error or cancellation after possible publication may be
`unknown`. A retained established effect can be `committed` even if the original
reply was lost or post-commit work failed. A protected resolver may disclose only
an authorized outcome subset; permission refusal must not turn an uncertain prior
effect into `not_committed`. `not_found` is a lookup result, not an effect state.

The commit boundary names the guarantee: volatile in-memory publication or qualified
durable publication. Do not label process-memory acceptance crash-safe. For durable
save profiles, the effect and its request receipt must be recoverably linked at the
publication boundary; crash recovery resolves pending evidence before admitting a
new conflicting effect. A generic returned Go error is insufficient evidence.

A caller persists a random bounded request key before an operation. Core binds the
key to namespace, operation kind, resource/scope, expectation, complete semantic
options and intended payload identity. Same key with different meaning conflicts.
A retry of a retained committed request returns its original saved result rather
than reapplying an old write over newer content. Resolution is read-only with
respect to starting a new effect, rechecks authority and verifies retained evidence.
Missing/expired receipts do not establish no prior commitment. Expiry/cleanup are
explicit, bounded and governed by the common retention contract.

## Shared admission boundary and capability qualification

The common core admission path is:

1. Bound request sizes, validate syntax and canonical resource/scope/expectation;
   attach the trusted host binding. Refuse unsupported required semantics.
2. Enter the qualified coordination gate. Resolve current policy/freshness and
   authorize the complete operation scope before reading protected state.
3. Resolve an existing request receipt before considering a new effect. Validate
   current comparison identities and any required declared input basis.
4. Admit aggregate budgets, required dependency holds and bounded recovery
   bookkeeping; stage and verify data without publishing it as the result.
5. Revalidate any state that can change outside the gate. Publish through the
   profile's recoverable boundary linking data, metadata, holds and receipt.
6. Return permission-safe effect/result evidence. Queue later transport work;
   never perform origin/webhook network I/O while holding storage mutation locks.

The numbered sequence describes one semantic boundary, not a new parallel runtime
or a guaranteed precedence between refusal reasons. Current native memory quota
admission may refuse before the store compares the observation.
Use existing authority, locks, quotas, atomic publication and receipt mechanisms
where qualified. Policy/generation/cleanup changes that matter to the decision must
participate in the same gate or cause explicit refusal. A lock around check-then-write
in an SDK does not supply core atomicity. Logical quota admission does not guarantee
physical disk headroom. Interrupted staging and partial publication require bounded
reconciliation; rollback must not be assumed.

Capabilities describe guarantees independently:

| Dimension | Required declaration |
| --- | --- |
| Conditional comparison | Content ETag, managed generation/metadata, expected absence; no implicit upgrade. |
| Coordination | Single owning runtime, common process gate, or evidenced cross-process/host mechanism; external-write coverage separately stated. |
| Persistence | Volatile/durable, receipt retention and verified restart reconciliation. |
| Access | Legacy unscoped or enforced resource scope, policy freshness/revocation boundary. |
| Atomic scope | Single resource or explicitly qualified multi-resource admission. |
| Recovery/security | Dependency holds/reserve and encrypted retained/staging paths qualified independently. |

An adapter advertises only implemented, tested combinations. Missing/false/unknown
support refuses strict requests before side effects; it never silently drops a
condition, opens scope, converts denial to cache fallback or replaces encryption
with plaintext. Feature names do not establish deployed support. Initial native,
Railway, Workers and custom-store matrices can differ while sharing semantic rules.
Introduce operation permissions only alongside an enforced operation and refusal
coverage, as [ADR 0010](adr/0010-an-enforcement-site-or-not-a-permission.md) requires.

Implementation acceptance remains in the [canonical queue](storage-foundation-plan.md):
two-reader conflicts, same-byte/metadata replacement, explicit absence/replacement,
wrong bindings, policy expiry/revocation, quota/holds, lost replies, restart and
crash-boundary evidence on each advertised profile. Cross-process/host and arbitrary
external-edit guarantees require separate evidence. [ADR 0015](adr/0015-durable-workspace-notifications.md)
and [ADR 0016](adr/0016-scoped-access-encrypted-cache.md) preserve notification,
readiness, scoped disclosure and key-custody boundaries.
