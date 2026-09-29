# Assessment evidence — 2026-09-29

Tested source revision: `5485a3640dbeeb368324d7e2a223bc17d7d23fc5`.
Read the adjacent product assessment for conclusions and limitations.

These are the actual temporary probes used in this assessment, preserved without rewriting their environment-specific paths. They are review artifacts, not permanent project test coverage.

## Native probe

`native-probe.py` uses the built `bin/stow-s3` in the assessment worktree and AWS CLI at `/opt/homebrew/bin/aws`. Update the `ROOT` and `AWS` constants for another host. It strips AWS/STOW/S3 environment variables, supplies generated local credentials, and starts disposable loopback servers. It writes synthetic data under `/private/tmp`, terminates its servers in a finally block, and retains the data/logs for investigation. It explicitly selects CRC32 after recording the default checksum failure.

Run with `python3 native-probe.py` after building the source revision. Read each assertion's meaning: the bucket-expansion observation records `passed: true` when expansion was observed; that is not a security pass. The metadata assertion checks both value and key casing.

## Installed Node consumer

`node-probe.mjs` was executed from a fresh directory containing the locally packed client and matching platform package. It requires `@chester-hill-solutions/stow-s3` and `@aws-sdk/client-s3` to resolve there. The output path is the original temporary JSON file. It strips provider/source-override environment variables before opening sessions. Payload checking is length and first byte; the native probe separately checks full bytes/digests. All timing samples, including the slower first process, are retained.

## Browser

Copy the built TypeScript package `dist/` beside `browser-probe.html`, serve the directory over localhost, and open the page in a real browser. The first visit writes a fixture through the real Go WASM and IndexedDB adapter. Reload to check persistence across document lifetimes. It uses its own `stow-assessment-5485a36` IndexedDB database and `stow-assessment-written` localStorage marker. The saved screenshot and accessibility output show the reload result. This does not test multiple tabs or browser engines.

## Gates

`make test-all` exited 0. `make standards` exited nonzero at the file-size gate. The remaining standards gates were then run individually; their exit codes are recorded as `RESULT` lines. The environment used temporary Go/npm/pip caches. Live-provider tests were skipped. The logs preserve configuration-specific skips and complete suite totals.

Fresh Python wheel and npm tarball builds/installations were also exercised outside the repository; their observed results and public distribution checks are recorded in the report. Competitors were researched, not executed.

## Subsequent pre-code source discovery

`pre-code-source-manifest.json` records 20 OpenCode source files at the public
v2.0.16 tag commit. Each downloaded file's Git blob identity was checked against
the pinned tree; SHA-256 hashes and immutable source URLs are retained. The files
were read from temporary storage, not vendored into Stow.

The installed OpenCode binary reported `2.0.16`; `serve --help` confirmed a
headless server, loopback/ephemeral-port options and `--pure` (no external plugins).
Both commands used temporary XDG data/config/cache/state paths. No model session
or checkpoint integration was run. See [the discovery report](../../pre-code-discovery-2026-09-29.md)
for static findings and the distinction from earlier executed probes.
