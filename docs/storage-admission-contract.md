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

### Enumeration, and why the decision is per key

Enumeration returned every key the store listed. The policy was consulted once, on the
prefix the caller asked for, and a per-key deny could not be honoured — not because of a
defect in the matcher, but structurally: a listing asks about a *prefix*, and
`public/secret` is not `public/secret/`, so an exact selector naming one key never matches
the resource a list is decided on. A policy denying enumeration of a key could be written,
validated and persisted, and did nothing. Worse, a caller who named the denied key *as the
prefix* was permitted, because the allow above it matched and the deny structurally could
not.

A listing is now filtered per key, and `KeyCount` counts what was returned rather than
what the store holds — a count the caller cannot account for is itself a disclosure, since
it says how much exists that this caller was not shown. `CommonPrefixes` are filtered the
same way, because a denied subtree disclosed as a bare name is the same leak. Truncation
and the cursors stay as the store reported them, so a page that lost keys is short rather
than silently complete.

The decision is `object.list`, matching S3: a caller holding list but not read still
enumerates, and a caller denied list does not learn that a key exists. The policy is
resolved once per listing rather than per key, because deciding per key through
`checkResource` re-validates the whole policy for every key on a page of up to a thousand.

Filtering is the right answer for enumeration and refusal is the right answer for a
protected exact request, and the contract already draws that line: an artifact request
refuses when any required member is unauthorized and is never silently filtered, while
enumeration returns only authorized results. A caller denied a key sees it as absent, which
is indistinguishable from it not existing — that indistinguishability is the property, not
an accident of it.

### Capturing a workspace, and an operation a reach test cannot reach

A capture was unconsultable. `Workspace.Destroy` consults a policy and
`workspace.capture` did not exist, so a policy could not deny capturing a workspace
at all — and the checkpoint surface is where an orchestrator asks what an agent has
done, so it is exactly the operation a deployment most wants to be able to refuse.

`workspace.capture` is a new operation rather than a reuse of `object.read`, because a
capture is not a read: it copies the workspace's bytes into a durable artefact that
can be exported and adopted on another host, so it changes state even though it
discloses nothing a reader could not already read. `ReadOnly` withholds it for that
reason. It is asked on the workspace root, before the capture lock, so a refusal costs
nothing and holds nothing; a capture of a subtree is a different operation with its own
contract, as with destroy.

Enforcing it in `pkg/stow` rather than `internal/runtime` created a problem worth
recording, because the fix is the interesting part. The policy reach test derives its
table from `authority.Defined()` and lives in `internal/runtime`, and it had no way to
reach a verb belonging to a caller of the runtime: the workspace is a resource
`internal/runtime` has no verb for. Its two honest answers were to add an `Ungated`
entry — which would have said the operation is unenforced while it is enforced, the one
answer this ratchet exists to make impossible — or to record where it is enforced
instead.

So `authority.EnforcedElsewhere` names those operations and the site that enforces
each, and `TestEnforcedElsewhereNamesASiteThatConsultsTheOperation` checks every entry:
the file must exist and must contain a call of the shape the enforcement scan already
recognises. A claim about a file this package cannot otherwise see is a hole with a
comment over it unless something verifies it, so something does. The reach test
asserts that every recorded operation also appears in its own table, so the two cannot
drift.

### Threading a principal into the out-of-session capture

`Workspace.CreateCheckpoint` was the easy half, because it holds a `Runtime` and so has
an authority. `stow.CheckpointOf` opens no runtime on purpose — that is the whole
operation, since claiming the session is exactly what an orchestrator must not do — so
it has no authority to read off one and no principal to decide as. Threading a policy
here therefore meant naming the principal rather than inferring it.

`CapturePrincipal` carries the two: `Environment` and `Policy`, a nil `Policy` meaning
no policy as everywhere else. `CheckpointOfAuthorized` consults them on the workspace
root before the capture lock. `CheckpointOf` keeps its signature and delegates with
`authority.All()` and no policy, which is what preserves its behaviour, and a test
asserts that consequence so the delegation cannot quietly become a gate.

That delegation is the honest reading and worth being explicit about: **the legacy
entry point refuses nothing.** Widening it to refuse would have been a breaking change
to a published API dressed as a security fix, and quietly tightening a flag's behaviour
is the failure mode this document keeps recording.

The CLI closes the loop. `workspace checkpoint --policy <path>` loads a persisted
revision through `policystore` and reads it **per decision**, not once at startup, so a
revocation is in force when it is issued rather than when the process began. Without
the flag the verb consults nothing, which is tested against a policy denying capture
that is present on disk and simply not named.

### The deny case cannot distinguish a check from no check

Three separate enforcement points produced the same finding, and it is worth recording
as a property of the design rather than an accident of one slice. If a policy denies
`workspace.capture`, then a check for *any other* operation is still refused, because
default denial spans kinds and operations. So:

| test | a correct check | no check / wrong operation |
|---|---|---|
| deny capture | refuses | **refuses** |
| allow capture | captures | refuses |

The deny case passes either way. Only the allow case tells a real check from no check —
and an allow-nothing policy is the least natural test to reach for, so a suite written
from the obvious example passes with the gate deleted. Every allow case in this work is
paired with a deny case for this reason, and each was confirmed by deliberately pointing
a check at a different operation.

This was not only a property of the new cases. Pointing `GetObject`'s check at
`object.write` instead of `object.read` produced **zero** failures in
`TestEveryOperationReachesThePolicy` — the systematic matrix was as blind as any single
test, and only an unrelated test about S3 prefix semantics caught it. So the reach test
now runs both halves for every operation: a policy denying the operation, and a policy
granting *only* it. The second half refuses every other operation by default denial, so
a verb checking the wrong one is refused and the case fails naming the operation. Between
them there is no way to pass by checking the wrong thing, and the failure message says
which operation the verb asked for instead.

Seeding the fixture for the allow half had to move onto the store rather than through a
runtime verb: the policy governs the runtime, so seeding through it needs `object.write`
and `bucket.create` granted, which is exactly what the half withholds. The deny cases
never noticed, because an explicit denial refuses before the store is consulted. Each
verb also needs the fixture its own operation implies — creating a bucket that already
exists fails, and deleting one that is not empty fails — so the seed is chosen per
operation.

A related asymmetry on the out-of-session path: a `Source` that cannot answer returns
its own error rather than a refusal, so an outage is not reported as a permission
decision. A caller handed "you may not" during an incident will treat it as a
revocation.

### The workspace as a resource

A workspace is a registered resource with its own lifecycle, and it is the one
resource `internal/runtime` has no verb for — the verb lives in `pkg/stow`, which
owns the directory. So the runtime's policy could not reach it, and `Destroy`
consulted the environment only: a policy could not deny it however it was written.

`Instance.Authorize` is the seam. It is exported rather than left to each caller
because resolving the current revision and deciding is the part that must have one
implementation — a second answer to "is this permitted here" is the shape this
repository keeps finding, and it is found in the enforcement point rather than in the
caller. `policy.Workspace(id, path)` names the resource, and the runtime is asked
about it the same way it is asked about an object.

Three things about the workspace selector are worth stating, and each was found by
a test failing for a reason that was not the intended one.

**A selector with no locator is the collection as a whole.** `Exact: ""` and
`Prefix: ""` are indistinguishable in a literal, and reading them as "matches
nothing" makes a whole workspace unnameable — the only way to say "this workspace"
does not exist. It also made the deny in the test pass for the wrong reason: with
nothing matching, default denial refused the destroy, and the test read that as the
deny working. A gate that passes because the wrong thing refuses is the failure
this repository keeps meeting, and it is the reason the allow case is in the suite.

**Default denial spans kinds, and that is not a bug.** A policy written about object
keys refuses to destroy a workspace, because it grants nothing for that. That looks
like a defect — attaching an object policy quietly made a workspace undestroyable —
and the tempting fix is to let a policy constrain only the kinds it names. That fix
is a widening: a policy mentioning no object entries would then grant every object
operation, and the test that caught it is `TestAPolicyNamingNoKindGrantsNothingAtAll`.
Refusal is the safe direction, the contract asks for it, and the fix belongs to the
assumption rather than the rule.

**A policy is a complete statement, not a patch.** A policy naming one workspace
denies destroy for every workspace it does not name, and the way to permit another
is an explicit allow. "Deny this one" and "deny this one and permit everything else"
are different statements, and only the second is a policy. The first version of
that test asserted the opposite and the code was right.

Two limits are recorded rather than fixed.

- **A subtree grant does not permit a whole-workspace destroy.** The root is not
  inside the subtree, so the selector does not match and the destroy is refused.
  Narrowing an irreversible operation to part of its target is not something that
  can be done safely, and the contract asks for scoped capture separately.
- **Capturing a workspace has no operation to be denied with.** `CreateCheckpoint`
  consults nothing, and adding a `workspace.capture` permission is a vocabulary
  change that ADR 0010 requires an enforced operation and refusal coverage for. The
  workspace ID is also generated, so a policy cannot be written against one before
  the workspace exists — a host opens, learns the registered identity, and writes
  the policy after. That sequence is what the tests here do, because it is the only
  one a host can actually perform.

### A revision in force, rather than a revision at open

`Options.Policy` is a `policy.Source`, consulted on each decision rather than read
once. It is a **function type** and not an interface for one reason: nil is the
answer the caller already has for "no policy", and an interface makes a nil
`*policy.Set` — which is not nil, because it carries a type — mean something else.
That difference is invisible at a call site and decisive at runtime, so `policy.Fixed`
names the fixed case and a nil `*Set` reaching it yields the empty set, which denies
everything. A typed nil failing open would be a permission nobody narrowed.

Two consequences follow from resolving per decision, and both are the point.

**Validation moves with it.** A revision is checked against the environment when it is
*read*, not when the environment was opened, because a later revision can be wider
than the one before it. `ErrWidening` is therefore reported by the operation, and
every operation reports it — which means the policy has to be resolved *before* the
environment is consulted, or a widening policy on a bucket operation would report
the environment's reason instead of the policy's. That ordering was got wrong once
and the widening test from the earlier slice caught it.

**An unknown answer is not a decision.** A source that cannot say returns its own
error, and the operation reports it rather than resolving it into a refusal. "You may
not" and "I do not know" need different responses from a caller, and a caller handed
the first during an outage cannot tell that it was the second. A stale revision is
the same case and already says so: `ErrStalePolicy`.

`Source` must be cheap and must not block, because the multipart paths consult it
while the instance mutex is held. A host whose policy lives on disk is expected to
wrap it in something that states how stale the answer may be — that wrapper is where
the contract's "maximum stale-policy interval" is written down, and it is the bound
a revocation takes to take effect. `policy.Expiring` is the shape of that for a
deadline rather than a document: it shortens and never lengthens, because a policy
that could extend its own lifetime by being asked again would never expire.

### Reaching upstream under a policy

The run-through adapter is the second enforcement point, and it exists because the
first one cannot cover it. `internal/runtime` gates an operation as the caller asks
for it; the propagation funnel does not run there. A per-second worker and the admin
retry route both drain the outbox, and a write that was admitted under a grant which
has since been withdrawn is exactly the case they present — which the contract
already called out as "retained old grants are provenance, not authorization".

So `runthrough.Config.ResourcePolicy` is consulted on both reaches, and the two are
kept separate rather than folded into the existing helper:

- **`upstreamReachable(bucket)`** answers whether the upstream is usable for a
  bucket: a provider is configured, the read grant is present, the bucket is the one
  pinned. It has no resource in it, so it asks no policy. Its callers are a
  scheduling pre-filter and a bookkeeping write, and neither is a reach.
- **`upstreamEnabled(bucket, key)`** is that plus the policy, and it is the read-side
  chokepoint. The key is a parameter because a policy decides per object and a bucket
  is not an object; a listing passes its prefix, which is what the runtime
  authorizes a list on too.
- **The propagation funnel** asks the policy itself, on the entry, rather than
  trusting the check the write path made when it enqueued.

The split is not tidiness, and it was found by a deliberate break. Folding the
policy into `upstreamReachable` puts one check on the propagation path twice, and
because the pre-filter runs first it answers for the funnel: deleting the funnel's
copy leaves every test green. That is the two-mechanisms-one-question shape ADR 0010
exists to remove, arriving through a change that looked like it removed one.

Two further decisions belong here rather than in the code.

**A denied propagation leaves the entry pending.** Refusing to propagate is not a
failure of the entry, and marking it terminal would discard a write the operator may
yet authorise.

**`DeleteObjects` narrows the batch rather than refusing it.** The grant is one
decision for the call; a policy is per object. A denied key is not propagated and the
rest are, because the local delete is authoritative either way and refusing the whole
batch over one denied key would leave a permitted key undelivered upstream for no
reason the caller can act on.

### Persisting a revision

`internal/policy` holds the record and the format; `internal/policystore` holds
the file. The split is forced, not tidiness: `internal/policy` is inside the
embedded runtime's dependency closure, which a test asserts must not link the
filesystem. A decision model that can read a file is a decision model whose
answers depend on a filesystem, and a WASM or embedded host has none.

A `Record` is a `Set` plus the three things a `Set` cannot carry: a host-issued
revision identity, a sequence that orders it against other revisions, and an
absolute deadline. The deadline is absolute rather than a lifetime for a direct
reason — a stored TTL is re-based on every load, so a process that restarts
inside the freshness window silently extends the permission past it, once per
restart, and nothing in the record shows it.

Four things in it are traps:

- **An absent record and a damaged one are different answers.** `ErrNoRecord` is
  a decision: run with the environment's own authority. `ErrRecordDamaged` is a
  policy whose answer is unknown, and answering that with the environment's
  authority hands back every permission the damaged policy was withholding.
  One error value for both would make the second read as the first.
- **The seal covers the deadline.** Without an integrity digest over the whole
  record, a truncated or edited file still decodes, and the failures that look
  safest are the dangerous ones: losing a deny entry narrows a grant, which
  reads as a refusal, while losing the deadline reads as a permission that never
  expires. The digest is over the canonical encoding, so a record that was never
  tampered with still fails if a field is.
- **Entries name operations; they do not carry a mask.** An `Authority` is a bit
  position, so adding an `Operation` renumbers it and a record written before
  that would decode to a different set of permissions afterwards. Names also make
  an operation this build does not define drop out on the way in, which is the
  safe direction, and going through `Set.Add` keeps that one rule rather than two.
- **A revision is replaced, never rolled back.** `Persist` takes the sequence the
  caller based its decision on and refuses with `ErrRecordSuperseded` if the
  stored record is not that one, under a file lock, so read-decide-write is a
  critical section. Without it, a revocation that lost a race to a re-grant would
  be silently undone and neither host would know it had lost.

Not yet implemented, and therefore not yet claimed: checkpoint capture, which has
no operation to be denied with.

**A saved-version selector is not implementable as written, and adding one would
repeat a defect already removed.** `Resource` carries a `Version` field and
nothing sets it. `Selector` has no way to name one. And there is nothing to name:
`storage.Store` has no versioned read — `GetObject` takes a bucket and a key and
nothing else — the public `pkg/stow.Object` exposes an `ETag` and no version
identifier at all, and the S3 surface answers `versionId` with
`InvalidArgument: versionId is not supported`. History is not retained: two writes
to one key leave the second readable and the first gone, which a workspace write
check confirms directly.

So a selector naming a saved version could never match, because no operation
produces a resource that names one. That is the `KindWorkspace` shape exactly — a
selector that is defined, persisted, matched, and built by no enforcement point —
and it was removed rather than wired up. A version selector added now would be
creating that dead selector rather than connecting an existing one, which is
strictly worse.

What the contract's "versions are selected explicitly when historical content is
involved" is actually about is retained historical content, and the only retained
historical content Stow has is a **checkpoint**: a sealed, hashed, durable snapshot
of a workspace that `diff`, `export` and `restore` read afterwards. Checkpoint
content is selected by checkpoint ID, which the contract already treats as not
being permission. So the version question and the checkpoint question are one
question, and it is the checkpoint operation that does not yet exist.

Two items were on this list and are now done, and are named here because a list
that keeps claiming shipped enforcement is unimplemented is a list nobody can
trust. A revision *is* consulted per operation, through `policy.Source`, so a
revocation is effective when it is issued rather than when the process restarts.
And run-through propagation *is* enforced, at two points: `upstreamEnabled` on
the read side, which every upstream read passes through, and the outbox funnel on
the write side, which asks per entry rather than only on the path that enqueued
it. Prewarm and provenance recording reach upstream through those chokepoints
rather than around them.

Background retries are enforced at the same funnel, and that was checked rather
than assumed: every call site that touches `a.upstream` was enumerated, and each
one is downstream of either `upstreamEnabled` or `decideUpstreamWrite`. A path
that reaches upstream with a routing check but no permission check would be a
bypass, and the one candidate — `recordUpstreamState`, which checks
`upstreamReachable` and not the policy — is reached only from intent enqueue,
which is itself guarded by `decideUpstreamWrite`.

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
- **An operation that names no resource is still answered by the policy.** A
  selector cannot match a bucket creation, but a deny entry that withholds
  `bucket.create` is honoured anyway, and an entry that withholds
  `environment.reset` is honoured even though Reset names no collection to
  scope it to. The asymmetry is deliberate: a deny is honoured wherever it can
  be attributed, and an allow needs a resource to be scoped to. Where the policy
  is silent the environment answers, which is what keeps a policy about object
  keys from also narrowing a namespace it never mentioned. Ignoring the deny
  instead would make a written refusal decorative, which is the more dangerous
  direction — the entry is in the file, the file is the contract, and the call
  would succeed anyway.

  `TestEveryOperationReachesThePolicy` in `internal/runtime` is the gate, and its
  table is derived from `authority.Defined()` so an operation with no case fails
  rather than waiting to be found. The gate it complements,
  `TestObjectChecksNameAResource`, asks a different question and could not have
  caught either half: it checks that object operations reach the *resource*
  check, and says nothing about the operations that do not.
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
