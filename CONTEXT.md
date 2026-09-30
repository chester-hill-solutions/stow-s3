# Context

Glossary for Stow: portable working storage for agents, tools, applications and tests.

## Product boundary

Stow owns preparation, storage access, workspace identity, checkpoints, inspection,
transfer, retention and cleanup. Callers own execution, agent turns, quiescing writers,
model access and sandbox enforcement. A turn-boundary adapter may request a checkpoint;
Stow does not need an executor or attempt framework to provide that operation.
Durable workspace notifications are an accepted core extension under
[ADR 0015](docs/adr/0015-durable-workspace-notifications.md): Stow will detect workspace
file changes and retain reports, parent-path subscriptions and acknowledgements, with
optional explicitly configured webhook delivery. Callers track agent reads and choose
readiness and reactions. The
notification API is planned, not shipped.
Resource-scoped ACLs and authenticated encrypted object/cache persistence are accepted
core additions under [ADR 0016](docs/adr/0016-scoped-access-encrypted-cache.md), with
native, Cloudflare Workers and Railway qualification planned. Hosts supply keys and
persistence; ordinary working directories remain plaintext. These APIs/profiles are
proposed, not implemented or certified.
[ADR 0014](docs/adr/0014-storage-product-caller-owned-execution.md) records this scope;
[the consolidated plan](docs/storage-foundation-plan.md) schedules it. The [planning index](docs/planning-index.md)
records retired plans and the disposition of their remaining work.

Scoped S3 sessions own a temporary storage server. Durable workspaces outlive a handle.
Neither use of “session” implies an agent process or security boundary.

## Terms

### Dev Bucket Service

A local, S3-compatible HTTP endpoint intended for development and automated tests. It is not production object storage.

### Embedded Runtime

A direct in-process object runtime with a small lifecycle interface (`open`, object/bucket operations, quotas, `reset`, and `close`). The first profile is memory-only. The S3 HTTP service is an optional compatibility adapter, not the runtime's primary interface. The `js/wasm` bridge exposes the same runtime through a JSON/base64 host protocol.

### Run-Through Adapter

A routing layer that accepts S3 requests at a local endpoint and, when explicitly configured, reads from or propagates supported mutations to a live upstream provider using the developer's existing environment credentials. Reachability and mutation are two separate decisions: the adapter is only constructed in run-through mode, and within it, run-through configuration plus a `mirrorWrites` policy is still not permission to mutate upstream.

### Upstream Configuration

The endpoint and credentials used by the run-through adapter to reach a live S3-compatible provider, including an optional session token for short-lived credentials. Resolved from environment variables with precedence: `STOW_*` > `S3_*` > `AWS_*`. Credentials determine how an explicitly requested upstream is authenticated; they never determine whether an upstream is used.

### Upstream Addressing

How a request names its bucket on the upstream: in the path, or in the host. `STOW_UPSTREAM_ADDRESSING` selects it, path-style is the default because every S3-compatible endpoint accepts it, and an unrecognized value is refused at startup rather than defaulted. Addressing is not a request to use an upstream — it configures how a requested one is reached, so setting it alone never selects one. The same distinction separates it from the client's own `forcePathStyle`, which is a separate setting on a separate surface.

### Mode Selection

The operational mode, and local-only is what stow runs unless something asks for run-through by name. Ambient AWS or S3 credentials are not an ask: a developer's shell and a CI runner both export them for unrelated tools, and inferring an upstream from their presence made whether stow contacted a live provider a property of the machine rather than of anything the user requested. Run-through is selected by `--mode run-through` or `STOW_MODE=run-through`; `STOW_MODE=local` forces local-only, and an unrecognized value is local rather than an error or a fallback. See [ADR 0011](docs/adr/0011-local-is-the-default-mode.md); it supersedes ADR 0001.

### Read-Through Cache

Isolated local copy of upstream-derived objects populated on cache miss. On cache hit, it can be revalidated against upstream via ETag/Last-Modified before serving. Transient upstream failures may serve stale cached data; local-only writes are never evicted merely because upstream lacks the key. Separate caches may enforce byte and object-count limits with oldest-access eviction and an optional TTL.

### Mirror-Writes Policy

An explicit run-through policy that asks for read-through behavior combined with propagation of supported local mutations to upstream. It is a routing choice, not a consent: propagation additionally requires live-write consent, so a run-through configuration carrying this policy still keeps every write local unless the consent is given. It emits a startup warning and records failed propagation in a durable per-key outbox. See ADR 0005.

### Live-Write Consent

The one mechanism that authorizes stow to mutate a live upstream, given separately from any policy — `STOW_ALLOW_LIVE_WRITES=true` or `--allow-live-writes`. It is the whole of the permission: run-through mode and the `mirrorWrites` policy together are not enough, and an explicit `false` is a refusal the policy cannot override. See ADR 0005.

### Write Outbox

A durable, per-key ordered record of upstream propagation intents. A prepared write-ahead entry is reconciled against the immutable committed local object version before propagation; file-backed workers coordinate attempts with expiring per-entry claims and a filesystem lock. An already-attempted entry is reconciled against upstream before it is re-propagated, so a crash between upstream success and acknowledgement is normally acknowledged instead of duplicated. Transient failures are retried, and prepared/active entries support loopback-only inspection and retry/discard actions. Outbox files carry a schema version: newer files are rejected, unversioned files migrate, and all writers sharing a file must run the same revision.

### Persisted Backend

Filesystem-backed storage using atomic object records and a versioned data format. It is the default backend. The former `.stowmeta` sidecar format is not migrated by 0.2.0.

### In-Memory Backend

Ephemeral storage backend with no filesystem persistence. It is explicitly selectable through the backend option and must satisfy the same behavioral storage contract as the filesystem backend. Because it holds every object in process memory, the session's byte budget is the only thing bounding it, so an upload in flight and the object a completion assembles from it are both counted against that budget.

### Multipart Initiation Properties

The object properties — content type, user metadata and checksum configuration — that a multipart upload fixes when it is created. They belong to the upload rather than to any part, because no part may change them: the object completion publishes carries exactly these, and they survive the process that initiated the upload.

### Local Dev Credentials

Ephemeral access key and secret key generated by stow on startup. Used for SigV4 authentication against the local endpoint. Distinct from upstream credentials.

### Workspace

A real directory, a bucket over those files through the embedded workspace API, and an identity that outlives the handle that opened it. An optional HTTP S3 facade borrows the same runtime and is available through `workspace serve`. The ordinary S3 filesystem backend uses a different object-record layout. See ADR 0007 and the canonical plan.

### Workspace Manifest

The single `stow-workspace.json` at a workspace root mapping key to content type, ETag, checksums, last-modified, and relative path. One manifest per workspace, never per-object sidecar files. A missing entry is legal and is the normal case for a file stow did not write. See ADR 0008.

### S3 Facade

An opt-in loopback S3 endpoint bound to the *same* runtime instance as a workspace, so that the object store has one implementation and two surfaces. A facade with its own storage is rejected: that would be two stores and a synchronization problem. This composition is implemented through `Workspace.Facade()` and `workspace serve`.

### Session ID

The durable identity of a workspace, minted at creation and naming its directory, bucket, quotas, timestamps, and liveness. Resuming is opening by this ID, never reconstructing state from an environment mapping. See ADR 0009.

### Collection

Bounded reclamation of a workspace whose owning session is dead, on a TTL measured in hours. Collection never runs against a workspace a live session holds, and never against a directory that is currently a process's working directory. That is a correctness property, not a policy.

### Promote

An explicit, named-destination move of a workspace to a real bucket. Never a side effect of expiry, of close, or of a successful run. It is a live write and carries the same consent requirement as any other.

### Handoff Reference

A value that names a workspace — its session ID and the capability to open it — rather than a credential. Distinct from the in-process-tree environment mapping, which does carry the generated secret key.

### Recovery State

Saved progress and pending storage or notification operations that accompany captured data so unfinished work can be reconciled and continued. It does not itself authorize those operations.

### Recovery Activation

An explicit decision to continue selected imported pending operations using the destination's current permissions. Receiving saved data is distinct from activating recovery.

### Confirmed Takeover

A transfer of responsibility for selected pending external operations after the source relinquishes it or shared ownership prevents the source from continuing. An unreachable source alone does not establish a takeover.

### Recovery Set

The data and saved records required to reconcile or continue selected unfinished operations. It is distinct from all contents of the workspace that originally held the work.

### Recovery Protection

A retention obligation that keeps a required recovery set available until its operations complete or are explicitly discarded. Ordinary expiry does not end this obligation.

### Recovery Reserve

Managed storage capacity held for recording completion, acknowledgement and cleanup of admitted work. It is distinct from the capacity needed to store transfer payloads.

### Storage Namespace

A trusted project or customer boundary grouping Stow resources for access policy and capacity accounting. It is supplied by the host's identity integration rather than established by a caller-provided label.

### Namespace Budget

The storage allowance for a storage namespace's managed working data, checkpoints and unfinished operations, including its recovery reserve. It is subject to the host's overall capacity limit.

### Scoped Checkpoint

A saved snapshot of an explicitly selected, authorized portion of a workspace, with completeness described within that declared scope. It does not represent the entire workspace or grant access to it.

### Partial Recovery

Recovery of verified, permitted data in which blocked pending operations and their dependents remain inactive while independent operations can be explicitly activated. It does not establish that the entire saved work has resumed.

### Ready Report

A caller's assertion that output at an identified immutable saved version is ready for use. It is distinct from an observation that mutable workspace content changed.

### Change Detail

An optional bounded comparison accompanying a ready report, describing changes between identified saved versions. It may contain a complete text diff or a changed-lines report and is not itself the saved output.

### Comparison Base

The saved version against which a ready report's output is compared, normally the previous ready version of the same output and scope. It is distinct from a recipient's last acknowledged version.

### Delivery Acceptance

A consumer's durable receipt of a notification, which ends delivery retries once recorded. It does not establish that the consumer has secured the referenced output.

### Artifact Release

A consumer's explicit acknowledgement that it has secured the saved output or no longer needs Stow's retained copy. It ends that consumer's retention obligation without ending other consumers' or operations' protections.

### Namespace Key Lifecycle

The independently managed active, rotated and revoked encryption-key versions for a storage namespace's protected data. It is distinct from the host provider that holds keys for one or more namespaces.

### Recipient-Encrypted Handoff

A scoped saved-data and recovery bundle explicitly encrypted for an authorized recipient's own keys. It is distinct from sharing the source's storage keys or granting permission to activate pending operations.

### Recovery-Key Backup

An explicitly prepared encrypted backup readable through a separately held recovery key when the original host or key provider is unavailable. It is distinct from storing the private recovery key inside the backup.

### Guarded Save

A managed save admitted only when its resource still matches the caller's expected read identity or expected absence. A stale expectation produces a conflict requiring the caller's next decision.

### Input Basis

The caller-declared resource versions used to produce an output. It distinguishes a readiness claim checked against current inputs from intentional use of a historical snapshot, without implying a complete record of reads.
