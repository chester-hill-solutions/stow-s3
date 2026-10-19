# Documentation inventory capture — 2026-10-18

The real release candidate's documentation link check fails with `spawnSync git
ENOBUFS`: its tracked/evidence filenames exceed Node's default 1 MiB synchronous
output buffer. [Original Railway failure](2026-10-18-doc-inventory/raw/original.log).
This blocks the existing release standards gate; link validation itself is intact.

Give this one Git inventory call a bounded 16 MiB output buffer. Extract the
existing inventory/filter operation into a script-local exported helper so the
regression can use an isolated real Git index. No storage behavior, dependencies,
public storage API or publication gates change.

The regression builds a temporary index with 2,200 long evidence names totaling
more than 1 MiB plus one real README. With the original buffer limit it fails
specifically with ENOBUFS while all six existing controls pass. With 16 MiB all
seven tests pass, and the actual repository check validates 277 Markdown files.
[Red](2026-10-18-doc-inventory/raw/regression-red.log),
[green](2026-10-18-doc-inventory/raw/regression-green.log),
[repository check](2026-10-18-doc-inventory/raw/doc-links.log).

All execution ran on the authorized Railway scratch deployment
`22d4d547-8587-4784-9c32-f29684ee5c71`, Go 1.25.6 / Node 25.9.0, Linux amd64.
[Final preparation checks](2026-10-18-doc-inventory/raw/checks.json) also cover
native build, version/install/site claims, command references and reproducible
packaged output. The red test is an expected nonzero proof, not a product pass.
No benchmark runs. Broader release CI and the four native consumer checks remain
separate publication gates.

The new cost is a higher capture ceiling for this build-time inventory: up to
16 MiB rather than 1 MiB. Actual output remains proportional to the file list,
and larger inventories still fail explicitly. Runtime memory guarantees and
link-selection semantics are unchanged.
