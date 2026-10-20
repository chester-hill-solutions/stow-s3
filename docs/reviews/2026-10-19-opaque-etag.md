# Opaque entity tags are not folded by case — 2026-10-19

This closes C09 from the
[bounded closure register](2026-10-18-correctness-closure.md). A conditional
request carrying an entity tag that differed only in case was treated as
matching, and one carrying the exact tag was sometimes treated as not matching.

## The defect

An entity tag is opaque. It identifies a representation, and a tag that differs
only in case identifies a different one, so the comparison is character for
character. Both comparators folded the case instead, with `strings.EqualFold`:

- `internal/storage` `matchesETagHeader`, the shared matcher behind the write
  and copy-source preconditions;
- `internal/s3api` `etagHeaderMatchesMode`, the HTTP read validator.

The two were the same rule in two places, and the same rule was wrong in both.

The observable effect was that the two error directions swapped. A PUT with
`If-Match` carrying a case-changed tag was admitted and replaced the object,
where the answer is `412`. A PUT with `If-None-Match` carrying one was refused
with `412`, where the answer is `200`. The copy-source conditions did the same.

## The correction

The comparison is exact. The weak-validator prefix is still recognised, and a
weak comparison still accepts a weak validator where a strong one does not,
because that is the one difference between the two comparisons. The wildcard is
untouched: it names every representation rather than one.

The duplicated comparator is gone. The HTTP validator path now calls the
storage matcher, so there is one rule rather than two that can drift. That is
the same argument the conditional-header code already made about the four
conditions it applies to a read and to a copy source.

Nothing about how a tag is generated or stored changes.

## The evidence

`TestAnOpaqueEntityTagIsNotFoldedByCase` drives the six routes a client can
use a tag on — PUT with `If-Match` and `If-None-Match`, the two copy-source
conditions, and GET with `If-Match` and `If-None-Match` — over a memory store.
Each route is exercised with the exact tag and with its case changed, and the
exact case is the control. A refused write is checked to have left the object
it refused to replace alone, and a refused copy is checked to have left no
destination behind.

`TestOpaqueEntityTagsAreNotFoldedByCase` covers the shared storage matcher and
`TestWildcardEntityTagsStillMatch` keeps the wildcard contract explicit.

Against the previous implementation all six case-changed routes fail and the
exact-tag controls pass. Against the corrected implementation every case
behaves as the contract requires.

## Scope

This is a bounded correction of one reproduced defect. No public API,
dependency, storage format, ETag generation or quality-baseline behaviour
changes; the Go quality baseline falls by one comment line because the
duplicated comparator and its prose are gone.

One required fix remains in the register: C07, the ordering of query names
before URI encoding.
