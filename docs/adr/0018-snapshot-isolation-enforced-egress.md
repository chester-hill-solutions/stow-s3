---
status: accepted
decision_digest: 8678bbce2c449c3e
amended: 2026-10-01
relates_to: 0004, 0007, 0013, 0014, 0017
---

# Isolation by snapshot and enforced egress, not by asking agents to cooperate

## Context

ADR 0014 assigns execution to the caller. It did not forbid a caller from
supplying an execution environment, and it did not anticipate an environment that
could refuse a write.

Coordination designs built on advice all failed at the same point: they could
observe an agent's activity but not stop it. ADR 0014's own amendment record and
the Ingress observer profile both state the consequence plainly — the profile
cannot refuse native filesystem writes. File claims were unfalsifiable because
nothing enforced them. Awareness cost roughly 139 tokens per co-located peer and
remained advisory. Inter-agent messages discarded 54% of what they sent. Two
platform primitives remove the problem instead of solving it better.

Measured against a deployed Worker on 2026-10-01:

- `snapshotContainer()` froze a container filesystem to a 3,952-byte immutable
  handle. An agent restored that handle into a separate container and saw exactly
  the pre-snapshot state, with no visibility of a file the first agent wrote after
  the snapshot. Both then diverged independently.
- With `enableInternet: false`, `interceptAllOutboundHttp()` routed all outbound
  HTTP through a Worker entrypoint. A read returned 200; a write returned 409. The
  refusal was confirmed by reading the audit trail from Durable Object storage
  rather than trusting the container's own account of itself.

## Decision

Ingress may supply an execution environment in which delegated agents run, when
doing so is the mechanism that makes its coordination guarantees enforceable
rather than advisory. This amends ADR 0014's execution clause and does not change
its storage clause.

Two conditions make this an execution boundary rather than an execution product:

1. **Isolation is by construction.** Each agent receives its own copy of the tree
   via a restored snapshot. Two agents never hold the same bytes, so no
   coordination is required to prevent them colliding.
2. **Egress is enforceable.** Every outbound request from an agent passes a Worker
   that may refuse it. An agent that ignores a declared scope cannot write anyway.

Storage identity, guarded-save admission, saved artifacts, retention and transfer
remain Stow's. Ingress gains no authority over stored bytes; it gains an
environment in which a caller's agent cannot silently write outside the copy it was
given. Callers continue to own agent and model selection, tool orchestration,
turn detection, execution credentials and cancellation.

The Agentbox deployment on the personal Cloudflare account is independent of this
decision and is not evidence for it. The evidence is the spike recorded in the
Ingress repository at `docs/snapshot-spike-report.md` and `docs/loop-spike-report.md`.

## Consequences

Coordination protocols built on advice are no longer required, and the ones
already refuted are not revived: awareness context, in-tree file claims, and
inter-agent conversation each addressed a collision that isolation removes. Gate
0's measured token cost stands as a fact about the awareness envelope and is not a
claim about this decision.

A Durable Object holds identity, state, policy and lifecycle while a container
supplies only the environment, which is the split ADR 0017 already described for
the Workers profile. Executing an agent inside a container is the case ADR 0017
left unstated.

A later run closed that loop: an agent inside a container with no public internet
wrote real files, committed, and pushed to Artifacts through the intercept, and the
commit and its contents were read back by an unrelated client. The gate observed the
full git conversation including the `git-receive-pack` write.

Not established by the evidence: cost at roster scale, durability across
deployment, whether a real agent harness runs in the container rather than only git,
and whether credentials can be injected at the gate so an agent holds none at all.
Each is open work, not a qualification of this decision.

## Amendments

- 2026-10-01: Accepted from measured spike results, then amended the same day after
  the loop test closed and the Artifacts round trip was verified out-of-band. Amends ADR 0014's execution
  clause only; its storage clause is unchanged. Relates to ADR 0017, which
  described the Durable Object and container split without stating this case.