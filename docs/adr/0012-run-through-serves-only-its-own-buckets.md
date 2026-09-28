---
---
status: accepted
decision_digest: ae8b80ad7a0321c3
relates_to: 0005, 0010, 0011
---

# A run-through proxy serves the buckets it was given, not the ones upstream has

This ADR records a decision that was implemented, tested, and defended in a branch
comment, and nowhere else. It is recorded here because it is the shape of decision that
gets reversed by someone who has not met the argument — including, in this case, the
person who wrote the test for it.

## Context

Run-through mode puts a local store and a separate cache in front of a real upstream
bucket, and the agent process never holds the upstream credential. That is the
product's central promise, stated in the README: *the agent process never holds the
upstream credential*.

There is a defect in the naive reading of that promise. A read for a key the local
store has never heard of was treated as a final answer, so a bucket that exists only
upstream was refused as `NoSuchBucket` by a server whose entire job is to fetch that
key. The only way through was to create the bucket locally first, which is a thing the
caller can do and was not asked to do. Run-through could not read anything the local
store had not already been seeded with.

There is a one-line fix, and it was written, and it works: treat a missing local bucket
as a recoverable cache miss and ask the upstream.

## Decision

**A bucket the local store has not been given does not exist as far as a run-through
server is concerned.** A read for one is refused without consulting the upstream.

The fix above is rejected.

The reason is the credential boundary. The server holds the upstream credential and the
agent does not, and that asymmetry is the whole security property — it is what makes
it safe to hand an agent a scoped set of buckets. A proxy that will fetch any bucket
its credential can see is not a narrower credential; it is the same one, reachable by
guessing names. An operator who intends an agent to reach `models` and not
`prod-backups` has no way to express that, because the read path is not where that
choice is made.

The two failures are not symmetric. The defect — a key that exists upstream and
nowhere locally cannot be read — is a real inconvenience with a one-command workaround
that the refusal names. The over-reach — an agent reads a bucket it was never given —
is a silent, unlogged capability expansion. One is a wrong answer, the other is a
removed boundary, and only one of them is discoverable after the fact.

## Consequences

Read-through works, and works for keys inside a bucket the local store holds. That is
the case the mode exists for, and it is what `prewarm` and the read-through cache
exercise. The cost is that a run-through server must be told which buckets it serves,
by preparing them or creating them locally, and the refusal says so.

`upstreamEnabled` remains the read-side chokepoint: no upstream, no `UpstreamRead`
grant, or a pinned `STOW_UPSTREAM_BUCKET` all stop a read before it leaves the
machine, and those are the places an operator narrows access. The namespace is a
second boundary, not a substitute for the first.

An operator who wants the naive behaviour has a way to get it: create the bucket
locally. That is a deliberate act naming a bucket, which is exactly the signal the
decision is asking for.

## The failure mode this ADR exists to prevent

The rejected fix is short, correct against every test, and obviously an improvement on
first reading. An implementer who finds the `NoSuchBucket` defect will write it, and
the existing tests will agree.

The distinguishing evidence is not in the read path. It is:

- the README's premise that the agent holds no upstream credential;
- `upstreamEnabled` returning **true** for any bucket when none is pinned, which means
  the read path was never the place access was restricted;
- ADR 0005, which holds that a policy is not consent — the same argument applied to
  writes, and the read path is where the same argument applies harder because a read
  has no consent step to violate;
- ADR 0010, which holds that a permission nothing checks is not a permission. The
  namespace is an enforcement site, so it has to be enforced somewhere — and the naive
  fix is a place where it is quietly not.

## Considered Options

1. **Treat a missing local bucket as a recoverable miss (rejected).** Fetches
   anything the upstream credential can see. Removes the credential boundary.
2. **Refuse (chosen).** The caller names the bucket by creating it locally. The
   refusal names that as the way forward.
3. **Ask the operator once and remember the answer.** A learn-then-allow list, so the
   first read of an unknown bucket is refused with the reason and a later one is
   allowed. This preserves convenience without a guess, and it is the obvious next
   step if the workaround proves tiresome. It is not built, because a learned allow-list
   is a new persistent security surface and this ADR does not need it yet.

## Amendments

None. Recorded at acceptance and not amended since.
