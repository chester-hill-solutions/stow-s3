# Contributing

Keep changes focused and reviewable. Check the issue tracker for existing work,
and discuss substantial behavior changes before implementation. Read the
[documentation index](docs/README.md), [domain terms](CONTEXT.md) and
[architecture decisions](docs/adr/README.md) before changing a storage contract.

## Build and test

Go is pinned in `.go-version`; `make` selects that toolchain. Use Node 20 or newer
for the client and site, and Python 3.10 or newer for the Python client.

```sh
make build build-wasm
make test
make test-race
make test-conformance
make test-all
make standards
```

`make test-all` covers the native server, shared conformance corpus, TypeScript,
Python, WASM and caller examples. `make standards` is the required gate: it checks
formatting, vet, race safety, quality ratchets, coverage, documented commands,
installation claims, ADR lifecycle, documentation links, site pages and generated
artifacts. Browser changes also need `make test-browser`; install Chromium with
`npx playwright install chromium` in `packages/stow-s3` first.

Live Railway qualification is separate and writes only disposable test prefixes.
Use the manual `railway-browser` workflow or the documented local runner in
[the browser guide](docs/browser-file-history.md#repeatable-browser-and-railway-tests).

## Generated output

The TypeScript package's `dist` directory and bundled WASM are deliberately
tracked. After changing linked Go or TypeScript source, rebuild and commit the
matching output:

```sh
make build-wasm
npm run build --prefix packages/stow-s3
make check-generated
```

The check compares the saved output with a fresh build. Rebuilding before taking
that comparison's first digest would hide a stale artifact.

Site HTML is generated from Markdown into ignored `site/.build/`. Commit the
source, route manifest, assets and lockfile; do not commit build output, local
previews, private configuration or credentials. See [site deployment](site/README.md).

## Standards and documentation

The [code standards](docs/CODE_STANDARDS.md) define the ratchets. New debt fails,
and removed debt requires shrinking its recorded baseline. Do not raise a
baseline to admit a regression. Go comment volume counts production comment
lines; comment ratio is reported, not ratcheted. Coverage uses covered-statement
floors and a bounded percentage-drop check.

For documentation changes, run `make check-doc-links check-adr-index check-doc-commands`
and `make check-api-reference check-scripts`. Keep current API guides separate from dated evidence and
retired plans. Move historical designs under `docs/history/` and repair their
incoming and outgoing links. Only remove an ADR after its replacement is in force;
record its number and original Git source in `docs/adr/retired.json`.

## Documentation style

Use the clear-writing principles of [Simplified Technical English](https://en.wikipedia.org/wiki/Simplified_Technical_English).
This repository does not claim full ASD-STE100 compliance.

- Lead with the task or behavior. Put development history in dated records.
- Use active voice and name the actor: the application, server, adapter or provider.
- Give one instruction per sentence and one topic per paragraph.
- Aim for at most 20 words in instructions and 25 in descriptions.
  Split longer sentences when their meaning remains clear.
- Use the same term for the same concept. Define unfamiliar terms on first use.
- Preserve API identifiers, commands, units, limits and error codes exactly.
- State conditions and failure behavior. Do not replace precise guarantees with broad claims.
- Remove filler, slogans, rhetorical contrasts and repeated explanations.
- Use numbered steps for procedures and tables for comparable options.

Read edits against the code and contract. Check links and rendered pages after changing headings.

## Pull requests

Use one logical change per pull request and state what changed, why and how it
was verified. Describe any change to observable S3 behavior against the shared
compatibility contract. Follow the repository's PR and required-check rules;
maintainer access is not a reason to bypass them.

Workspace verbs are registered in `cmd/stow-s3/workspace.go`. Add corresponding
TypeScript and Python wrappers and a shared case in
`conformance/workspace/cases.json`; the independent drivers verify one contract.
