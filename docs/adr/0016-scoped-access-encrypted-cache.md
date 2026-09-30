---
status: accepted
decision_digest: 454b42c043ae71fd
amended: 2026-09-29
relates_to: 0010, 0014, 0015
---

# Scoped access and encrypted storage support protected cache deployments

## Context

The user identified ACLs and encryption as core additions that support edge caching.
Requested targets are native services, Cloudflare Workers and Railway. Existing
run-through caching, revalidation, offline mode and operation-level authority provide
a foundation, but per-caller resource ACLs, encrypted records and Workers qualification
are not implemented.

## Decision

Add resource-scoped access control and authenticated encrypted persistence as shared
storage capabilities. Qualify the existing cache on native/Railway and a focused
Workers adapter. This does not certify production caching or implement Amazon ACL/IAM
wire APIs. Contract details remain proposed until implemented and verified.

Authenticate callers into trusted principals/namespaces. Effective access intersects
environment authority, principal policy and resource scope; derived handles never
widen access. Scoped profiles deny by default without silently changing the existing
unscoped development profile. Authorization applies to cache hits, origin fetches,
lists, copy, multipart, capture/export and notifications/background retries. Cached
bytes and shared origin credentials never grant local access. Live-write consent
remains a separate attenuation input.

Define policy versioning, expiry and revocation. Offline data availability does not
extend permission lifetime. Local revocations and expiry apply offline; centrally
changed policy cannot be learned during a partition without a defined synchronization
and expiry contract. Ordinary host file access requires external OS/sandbox enforcement.

Start encryption with opt-in authenticated protection of persistent object/cache
records through a versioned envelope and injected host key provider. Bind ciphertext
to namespace/resource/version and preserve logical validators. Missing keys, unsupported
versions and authentication failure refuse reads; no plaintext fallback. Specify key
rotation, temporary files, multipart/outbox payloads and metadata leakage before
claiming a complete profile. Use standard cryptographic implementations, not a new cipher.

Encryption-key lifecycles are independent per trusted project/customer namespace,
even when one host provider manages several namespaces. Routine rotation preserves
authorized access to older retained versions until verified re-protection/removal
permits key retirement. Saved content/operation identity remains stable across changes
to encrypted representation. Explicit emergency revocation stops use of the affected
key and can block that namespace's recovery; no revoked-key or plaintext fallback.
Required ciphertext still follows retention protection until release/discard. Rotating
or revoking one namespace's keys must not invalidate other namespaces' data.
Host providers own custody and revocation propagation; core cannot retract previously
disclosed plaintext or prevent provider-side key destruction outside its control.

Encryption protects retained storage within the stated threat model, not a compromised
executing process that holds keys. Ordinary directory workspaces remain plaintext for
file tools. Encrypted bundles need a separate explicit envelope/key contract. Keys,
credentials and host policy configuration do not travel implicitly with checkpoints.
Existing plaintext stores require explicit conversion or a separate encrypted store.

Support explicit recipient-encrypted handoff of authorized scoped data/recovery
bundles over the existing verified archive. The recipient uses its own keys/provider;
source storage keys and credentials remain source-side. Authenticate recipient/key
selection through host integrations, qualify an authenticated versioned envelope and
protect in-scope export/import staging. Decryption does not grant destination access
or activate pending external operations; those retain current policy and confirmed-
takeover requirements. Source key revocation cannot retract an independent copy that
was already authorized and exported.

Offer explicitly enabled backups prepared for a separately held recovery key before
host/key-service failure. Reuse the recipient-encrypted envelope and verified archive;
private recovery keys stay outside the backup and hosts own custody/scheduling. The
recovery-key holder has an intended independent decryption route, not an implicit
source permission or grant to activate external operations. Qualify fresh-host restore
without the source key provider; every required recovery-key version must remain
available. No silent escrow or hosted custody service is added. Losing every usable
key cannot be repaired by the presence of encrypted data alone.

Native/Railway reuse the existing service with protected persistence. Railway initially
uses one service instance and an explicitly mounted volume. Workers requires platform
persistence and a qualified shared-core adapter; native processes, locks and file
watchers do not apply. Disposable edge cache acceleration stays separate from durable
policy/notification state. Preserve shared policy/format conformance and verify actual
Go/WASM compatibility rather than creating divergent authorization engines.

Implementation is S1-7/S1-8 and platform evidence is S4-4 in the canonical plan.
[Protected-cache detail](../protected-cache-plan.md) expands those items. Host adapters
own bindings, secrets and transport/lifecycle; no hosted identity/control plane is added.

## Consequences

- Resource permissions and encryption become reusable across agents, applications and caches.
- Existing compatibility/release gates remain in force; target capabilities are explicit.
- This decision ships no API, provisions no cloud resources and certifies no deployment.

## Amendments

- 2026-09-29: Accepted the user's ACL/encryption cache direction and native, Workers and
  Railway targets, including enforcement, plaintext-workspace and host-adapter boundaries.
- 2026-09-29: Selected independent namespace key lifecycles, separating routine rotation
  with retained-data continuity from explicit emergency revocation that can block recovery.
  Keys remain host-managed; dependency checks include historical and unfinished storage.
- 2026-09-29: Accepted explicit recipient-encrypted handoff across different host key
  providers without sharing source storage keys. Existing archive validation, scoped
  authority and recovery activation remain required; staging needs confidentiality evidence.
- 2026-09-29: Accepted optional backups prepared for a separately held recovery key,
  enabling qualified recovery without the source host/provider while keeping private
  keys outside backups and external-operation activation separate from data restoration.
