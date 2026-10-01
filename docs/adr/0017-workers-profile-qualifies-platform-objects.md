---
status: proposed
decision_digest: e8dbec3fde9cb430
amended: 2026-10-01
relates_to: 0008, 0009, 0010, 0014, 0016
---

# Workers qualifies platform objects as Stow's workspace substrate; execution stays caller-owned

## Context

Cloudflare's 2026-09-30 and 2026-10-01 platform releases introduced host objects that
overlap Stow's workspace and persistence surface, and therefore change what a Workers
adapter has to be rather than merely making one easier. Containers gained a
`durable_object` scheduling policy in which a Durable Object selects a Container's image
and instance at start, starts it without a global control-plane hop (648 ms median
time-to-interactive against 4.049 s on the prior path, ComputeSDK Burst TTI benchmark),
and captures immutable filesystem snapshots in public beta. Workers KV Instant offers a
read-mostly namespace replicated to 300+ locations at 1.62 ms p99 read and 256 ms p99
write replication. K2 offers a durable ordered log over R2 in public beta. Artifacts
offers a versioned Git filesystem with fork, repository-scoped tokens, event
subscriptions and namespace-level jurisdiction.

These objects are adjacent to Stow's concepts rather than identical with them. ADR 0016
named Cloudflare Workers as a target for protected cache deployments but scoped that work
to a focused adapter over the existing cache; it did not decide how Session ID, Workspace,
Workspace Manifest and policy resolution are realized on a host with no local process, no
locks and no filesystem. ADR 0014 leaves execution with the caller, and the Containers
design reaches that same division independently, describing a Durable Object that retains
the sandbox's "identity, state, policy, and lifecycle" while the Container supplies only
the environment.

The failure this decision must avoid is competing for the compute role. The same release
made a first-party workspace that outlives a session available to any account, so a
Workers profile bidding for that role would lose it. The profile is also not yet
implementable: KV Instant is private beta behind a 1 MB namespace, 10,000 pair and
one-write-per-second-per-namespace limit, Artifacts requires Workers Paid, and the free
Workers CPU allowance of 10 ms is already recorded as insufficient for the measured
33.5 ms read pass in [deploy-anywhere-plan](../deploy-anywhere-plan.md).

## Decision

Qualify the Workers profile as the realization of Stow's existing storage primitives on
platform objects, and record the mapping here so that it is decided rather than
rediscovered during implementation. This adds no API and implements nothing. The profile
remains proposed until each mapping below has conformance evidence, and contract details
stay proposed until implemented and verified.

A Durable Object is the qualified host realization of Session ID and Workspace identity:
one object, one stable identity, serialized access, outliving the handle that opened it.
Resuming by Durable Object ID is resuming by Session ID, and introduces no second concept.
This is the same shape ADR 0009 gave the native process registry. A Durable Object does
not become an execution site. Stow supplies no container image, entrypoint, instance
choice or lifecycle policy on this profile; where a caller starts a Container that is
ADR 0014 caller-owned execution using a platform facility, and the platform's own statement
that the Durable Object retains identity, state, policy and lifecycle is the same division
this product already drew.

Split persistence by access shape rather than mapping every concern onto one store.
Durable Object storage carries workspace-scoped metadata and checkpoint records. R2 is a
candidate additional Persisted Backend tier beside the filesystem and in-memory backends,
for objects exceeding a Durable Object record size or shared across workspaces, subject to
the existing behavioral storage contract, read-through cache and live-write consent rules.
KV Instant is a candidate resolution path for policy and ACL decisions only and never for
object bytes: the decision payload is small, read-mostly and globally replicated, which is
the shape that service is built for. Its namespace ceiling is a bound on the ACL model and
is recorded as one — a namespace whose ACL set does not fit is refused, never silently
truncated, and a profile that cannot express ADR 0016's policy versioning and revocation
within that ceiling is not qualified.

The Workspace Manifest's per-key metadata must not be reproduced on a profile where the
host has none. KV Instant returns `null` from `getWithMetadata` and accepts no metadata on
write, so a Worker profile resolves manifest entries through object properties and claims
no metadata parity it does not have.

Do not adopt K2 for the Write Outbox under this decision. K2's published trade is that
messages are produced and consumed in batches "at the expense of message-level retries",
at roughly one second p99 produce latency, inside a 10 GB and 30 MB/s beta envelope. The
outbox is a correctness mechanism whose accepted behavior — expiring per-entry claims,
reconciliation against the immutable committed local version, and acknowledging rather than
duplicating a crash between upstream success and acknowledgement — is not traded for
latency or availability. Reconsider when a platform stream offers per-message retry, an
Express-tier latency envelope and a Kafka-compatible drop-in.

Stow adds no jurisdiction of its own. A Workers profile inherits its host's storage
jurisdiction and key custody and introduces no residency control; where a host declares
one, as Artifacts namespaces do with `us` or `eu`, Stow consumes it rather than
duplicating it. Jurisdiction neutrality is a deliberate difference from host products that
ship their own residency switch, and belongs in the product documentation as a property
rather than as a gap.

The profile requires a paid plan: the free CPU allowance is already known to be
insufficient for the measured read pass, and a workspace registry is not a free-tier
workload. Host adapters continue to own bindings, secrets and transport/lifecycle, and no
hosted Stow identity or control plane is added.

## Consequences

- A Durable Object becomes the reference realization of Session ID outside the native
  process registry, and resume-by-identity needs no platform-specific concept.
- R2 becomes a fourth candidate Persisted Backend; backend behavior stays single-sourced
  and the compatibility contract is unchanged.
- KV Instant's namespace and write-rate limits become a design input to ADR 0016's policy
  and revocation work, which is where that limit has to be designed around.
- The Write Outbox keeps its host-local implementation and no correctness property moves
  to a beta service.
- Stow does not compete with the platform for the agent-workspace role, and says so.
- This decision ships no API, provisions no cloud resources and certifies no deployment.

## Amendments

- 2026-10-01: Proposed from the 2026-09-30 and 2026-10-01 Cloudflare platform releases.
  No prior amendment; the status stays `proposed` until the user accepts the direction and
  until each mapping has evidence.