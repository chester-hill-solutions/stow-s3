# Implementation receipts — 2026-09-29

These files describe local, unpublished 0.3.0 development measurements on macOS
arm64. The source tree changed between experiments; they are not measurements of
one released artifact. See [the implementation report](../../../implementation-2026-09-29.md)
for the validation ledger and limits.

- `stow-portable-cost-20260929.json`: original small fixture.
- `stow-portable-cost-large-20260929.json`: original large fixture under test load.
- `stow-portable-cost-parallel-20260929.json`: parallel flushes alone; no useful fix
  for the dominant repeated directory scans.
- `stow-portable-cost-indexed-20260929.json`: exact-path inventory replaces repeated
  directory reads; still measured during other test work.
- `stow-portable-cost-quiet-20260929.json`: same scan fix after tests stopped, serial
  copying, five full captures.
- `stow-portable-cost-copy-20260929.json`: final eight-copy/sixteen-flush workers;
  all verification and durability barriers retained. Contains an 11.10 s outlier.
- `stow-portable-cost-small-final-20260929.json`: final small fixture, five captures.
- `rejected-reuse-profile.json` and `rejected-reuse-chain.json`: parent hardlink
  experiment. Removed from production because measured latency worsened.
- `packaging.json`: earlier candidate's packed npm/wheel consumer checks. Final
  publication must rebuild, validate and check the exact released artifacts.

`retainedFileBytes` sums apparent sizes for every path. `uniqueInodeFileBytes`,
where present, counts each inode once. Neither is a physical allocation measure.
The accepted implementation uses full copies; logical retention limits count all
referenced payload bytes.
