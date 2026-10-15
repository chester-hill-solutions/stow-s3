# Filesystem listing: derive keys from record paths

## Problem

At 10,000 objects, the filesystem list path spent about 204–206 ms on any
single page and about 2.11 s on complete enumeration in the baseline run. A
page allocated about 21.2 MiB. First and middle pages had the same cost, which
showed that listing read record headers across the whole bucket before
pagination. Prefix filtering also happened after those reads.

## Root cause

`FilesystemStore.ListObjectsV2` walked every object path, read each record
header, collected full metadata, and only then called `storage.PaginateObjects`.
Flat and sharded record paths encode the key reversibly as hex. The
digest-addressed `_long/<shard>/<digest>` layout does not and still requires a
header to recover its key. The directory walk as a whole is not key-ordered
when these layouts are mixed.

## Change

Listing now derives reversible keys from flat and sharded paths, applies the
requested prefix before opening records, and sorts the candidates explicitly.
It uses the existing `storage.PaginateObjects` implementation to preserve
delimiter, continuation-token, and max-key behavior. It reads headers only for
objects returned on the page. Digest-addressed entries retain their discovered
header for use if they are returned.

No persistent index or write-path changes were added.

## Evidence

Both runs used the existing benchmark, one iteration per case, on the same
Apple M1 Pro host with the same command:

```sh
go test ./internal/storage/fs -run '^$' -bench '^BenchmarkListObjectsV2Scale$' -benchmem -benchtime=1x
```

| Case | Before | After | Bytes allocated before → after |
|---|---:|---:|---:|
| 1,000 first page | 15.2 ms | 47.7 ms | 2.01 → 2.21 MiB |
| 1,000 middle page | 16.8 ms | 8.5 ms | 1.86 → 1.64 MiB |
| 1,000 prefix page | 16.5 ms | 3.4 ms | 1.37 → 0.68 MiB |
| 1,000 full enumeration | 16.8 ms | 17.1 ms | 2.01 → 2.21 MiB |
| 10,000 first page | 206.0 ms | 102.1 ms | 20.24 → 13.03 MiB |
| 10,000 middle page | 204.0 ms | 108.5 ms | 20.24 → 13.03 MiB |
| 10,000 prefix page | 204.8 ms | 156.8 ms | 13.38 → 5.91 MiB |
| 10,000 full enumeration | 2,110 ms | 1,292 ms | 202.43 → 130.42 MiB |

The 10,000-object cases show lower time and allocations, consistent with fewer
header reads. The single-iteration 1,000-object timings are noisy: the first
page regressed sharply while middle, prefix, and full enumeration varied in
different directions. Treat those small-count timing comparisons as
inconclusive. The regression `TestListObjectsV2ReadsOnlyRecordsOnRequestedPage`
failed before the change when an unrequested record had a corrupt header and
passes after it; requesting that record's page still reports the corruption.
The filesystem storage package suite also passes.

## Impact

For the measured 10,000-object bucket, first-page latency fell by 50%, middle
page by 47%, prefix page by 23%, and full enumeration by 39%. First-page
allocation fell by 36%; prefix-page allocation fell by 56%. I treat the
pre-change design as no longer acceptable for interactive listings at 10,000
objects: it crossed roughly 200 ms per page and 2 seconds for enumeration.
Those cutoffs are a practical judgment, not a previously published Stow SLO.
The measurements only bracket the boundary between 1,000 and 10,000; they do
not locate a finer threshold. After the change, 10,000-object pages still take
about 100–157 ms and enumeration about 1.3 seconds, so this is a material
reduction rather than sublinear listing.

## Tradeoffs

The walk still visits the object tree and stores/sorts candidate keys. Complete
enumeration remains O(N × pages), and each request still has O(N) directory
work. This deliberately stops before an ordered index, which would add write
amplification, persistent metadata, and crash-recovery obligations. The
benchmark is one iteration per case; machine and filesystem-cache variation
limit precision.

## Compatibility

The object record formats, paths, writes, and public S3 API are unchanged.
Ordering and pagination still go through the existing shared paginator, and
context cancellation is checked during traversal and page materialization.
Flat, sharded, and bounded digest-addressed records remain supported.

## Remaining risks

Buckets dominated by bounded digest-addressed paths still read each candidate
header because the key cannot be recovered from the digest. All layouts still
require a complete directory walk per request. Raw edits to object record
directories are outside the managed profile; a path/header identity mismatch
is reported when that object is selected for a page.
