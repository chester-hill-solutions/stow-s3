# Native capacity fixture qualification — 2026-10-18

The release Windows job fails in new payload-scan and multipart tests before
reaching their assertions: both shared helpers unconditionally open namespace
capacity, while `capacity.Open` explicitly supports Darwin and Linux only.
The existing capacity fixtures already check `SupportsGuardedWrites` first.
[Original Windows failure](../assessment-evidence/2026-10-18-release/windows-original.log).

Use that existing capability predicate in the payload fixture and only the bound
branch of the shared multipart fixture. Unbound multipart and completion tests
continue to execute on Windows. No production code, supported backend behavior,
public API, dependency or storage format changes. The originally failing tests
remain the regressions; their setup now reflects the advertised capability.

Railway deployment `122c1ab0-acf4-4185-96e0-a2070983ec0e` formats both files and
passes the selected payload, completion and multipart tests twice normally and
twice under race. No selected Linux test skips. Existing allocation bounds and
content/quota/reopen assertions stay intact. Quality, file-size and formatting
checks pass. [Checks](../assessment-evidence/2026-10-18-release/railway/fixtures/checks.json),
[normal](../assessment-evidence/2026-10-18-release/railway/fixtures/focused-normal.log),
[race](../assessment-evidence/2026-10-18-release/railway/fixtures/focused-race.log).
The actual Windows rerun remains a required release-PR check.

The cost is explicit skips for namespace-bound tests on unsupported systems,
consistent with the existing fixtures. No allocation allowance is raised and no
benchmark runs. Exact Railway-formatted/tested source bytes are copied back and
verified in the commit. The same scratch job also captures an independent stale
API-reference reproduction/regeneration; those outputs are preserved as evidence,
not a claim that the API printer is correct.
