# Historical evidence archive

The November 5, 2026 repository cleanup removed bulky historical outputs from
the current tree. Original bytes remain in Git history at
[`e549d047b3b5`](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19).
The cleanup preserves 12,833 files totaling 206.35 MiB there.

| Original location | Archived files | MiB |
| --- | ---: | ---: |
| `docs/reviews/` | 10,334 | 126.49 |
| `docs/benchmarks/` | 2,243 | 65.63 |
| `docs/assessment-evidence/` | 256 | 14.24 |

This keeps copied source snapshots, logs, profiles, temporary probes and build
archives out of everyday browsing. Reports retain their original dates and
results. Their evidence links point to the pinned snapshot where files were
removed. Paths inside old command examples or structured receipts describe that
original tree; use the snapshot when following them.

## Browse original files

- [Review evidence](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/reviews)
- [Benchmark evidence](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/benchmarks)
- [Assessment evidence](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/assessment-evidence)

## Recover a local copy

From a clone containing that commit, extract the original evidence into a
separate temporary directory:

```sh
archive_dir="$(mktemp -d)"
git archive e549d047b3b5aa220563a486deb4e7aa8caa4a19 \
  docs/reviews docs/benchmarks docs/assessment-evidence \
  | tar -x -C "$archive_dir"
printf '%s\n' "$archive_dir"
```

If a shallow clone lacks the commit, fetch it first:

```sh
git fetch origin e549d047b3b5aa220563a486deb4e7aa8caa4a19
```

The extracted records use their original paths. To rerun an old probe, use its
recorded source revision and environment; the archive is evidence, not a promise
that old harnesses work with today's source.

## What stays in the current tree

- Human-readable [review reports](reviews/README.md), [benchmark reports](benchmarks/README.md)
  and [assessment methods](assessment-evidence/README.md).
- The [session density baseline](benchmarks/session-baseline.json) and root benchmark configurations.
- The [October 13 comparison](benchmarks/2026-10-13-competitor-performance/comparison/),
  used by the benchmark verification helpers.
- The complete [October 19 marketing comparison](benchmarks/2026-10-19-marketing-comparison/README.md),
  which supports the site's published v0.3.3 measurements.
- The [correctness register](reviews/2026-10-18-correctness-closure/register.json)
  and other compact review metadata outside raw output folders.
- Generated SDK distribution files required by source consumers and release checks.

New evidence follows the [contributor guidance](../CONTRIBUTING.md#standards-and-documentation).
Git history was not rewritten. Full-history clones retain the archived objects;
the reduction applies to the current working tree and new source snapshots.
