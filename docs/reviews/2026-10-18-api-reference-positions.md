# Stable API signature rendering — 2026-10-18

The release standards gate fails because the generated Go reference drifts.
Imported type AST nodes carry positions from their original files, but the
signature printer interprets them through the importing catalog's file table.
Unrelated storage edits therefore introduce arbitrary line breaks, including
`time.` followed by `Time` on another line.
[Original standards failure](../assessment-evidence/2026-10-18-release/standards-original.log),
[failed rehearsal](../assessment-evidence/2026-10-18-release/rehearsal-original.log).

A real-AST regression prints one imported struct against two unrelated position
tables. The compact table is the healthy control; the dense table splits the
selector and changes the signature. It fails twice normally and twice under race
before implementation changes, with no timing, randomized data or mocks.
[Normal red](../assessment-evidence/2026-10-18-release/railway/printer-red/printer-red-normal.log),
[race red](../assessment-evidence/2026-10-18-release/railway/printer-red/printer-red-race.log).

Change the single printer call to use an empty file table. Signature rendering
then ignores irrelevant positions while retaining the same AST declarations.
No storage behavior, exported runtime API, dependency or persistence format
changes. Regenerate the Go reference with this correction and the Python
reference with its release version `0.3.2`; all other generated reference pages
and the symbol inventory remain byte-identical.

Railway deployment `122c1ab0-acf4-4185-96e0-a2070983ec0e` passes normal/race
regressions twice each, formatting, quality/file-size checks, vet, regeneration
and two independent reference-check invocations. Exact formatted/tested Go and
generated-reference bytes are copied back and verified in the commit.
[Checks](../assessment-evidence/2026-10-18-release/railway/printer-final/checks.json),
[green](../assessment-evidence/2026-10-18-release/railway/printer-final/printer-normal.log),
[repeat verification](../assessment-evidence/2026-10-18-release/railway/printer-final/verify-reference-repeat.log).

The cost is one empty `token.FileSet` per rendered signature in this build-time
tool. No runtime allocations change, and no performance improvement is claimed
or benchmark run. Full release CI and packed-consumer qualification remain
publication gates.
