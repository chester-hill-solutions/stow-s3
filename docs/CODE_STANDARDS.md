# Stow Code Standards

Stow records existing code-quality debt in checked-in baselines.
New violations fail CI. Remove baseline entries when their debt is removed.
Do not raise a baseline to make a regression pass.

Coverage instruments module packages across test boundaries. The embedded API and
its public `pkg/stow` facade retain the original combined coverage floor through
`packageOwners` in the coverage baseline. Extracting the implementation does not
lower the floor or stop counting the existing public contract tests.

The Go quality ratchet excludes files carrying Go's generated-code marker.
Generated protobuf code remains subject to compilation, formatting and tests.
The benchmark build regenerates its schema with pinned tools and checks for drift.

## Commands

Run the complete gate:

```sh
make standards
```

Run individual checks:

```sh
make format-check
make lint
make check-go-quality
make check-state-machines
make check-ts-quality
make check-type-escapes
make check-dry
make check-file-size
make check-coverage
make check-version
make check-generated
```

From `packages/stow-s3`, use the package checks:

```sh
npm run lint
npm run check:lint-ratchet
npm run check:type-escapes
npm run check:dry
```

## Ratchet policy

| Baseline | Records |
| --- | --- |
| `scripts/baselines/go-quality.json` | Go quality identities |
| `scripts/baselines/state-machines.json` | Implicit lifecycle state identities |
| `scripts/baselines/lint-ratchet.json` | TypeScript warning counts |
| `scripts/baselines/type-escapes.json` | TypeScript type escapes |
| `scripts/baselines/dry.json` | TypeScript duplication counts |
| `scripts/baselines/file-size.json` | Oversized files |
| `scripts/baselines/go-coverage.json` | Go coverage floors |

New and stale identities both fail. Update a baseline after reviewing the removed debt.
Baseline generation is a maintenance operation, not permission to accept a regression.
Detected inline suppressions also count and need a specific explanation.
Fixture and generated-output exclusions must be documented in the check configuration.

CI uses full history (`fetch-depth: 0`) to compare against the parent or merge base.
A shallow checkout fails the history check.

Coverage floors use the strictest platform measurement.
`internal/parentwatch` covers 22 statements on macOS and 10 on Linux, which changes aggregate coverage.
Check Linux CI results before updating the baseline; local measurements alone can set an invalid floor.
Coverage improvements require an explicit baseline update. Regressions fail.

`tools/quality` scans Go code and prints measurements.
`scripts/check-go-quality.mjs` applies the ratchet through shared comparison helpers in `scripts/ratchet.mjs`.
The scanner does not enforce the gate.

The Go comment budget counts production `comment-lines` only.
`comment-ratio` is reported without a floor; removing code can raise that ratio.
Test comments are excluded from this budget.
Structural checks, coverage, formatting and vet still apply to tests.

## Explicit state machines

`make check-state-machines` scans production Go and TypeScript using their parsers.
It detects two or more lifecycle booleans in one struct, class or lexical block,
and TypeScript lifecycle booleans paired with optional cleanup promises.
A single boolean remains valid for a two-state lifecycle. Use named states and
transitions when the lifecycle has additional phases or retry behavior.

Go roots and the TypeScript package come from `config/scan-roots.json`;
TypeScript scanning covers its `src` directory. Go test files, generated Go files,
TypeScript declarations and generated `dist` output are excluded. The shared
name vocabulary is in `config/state-machine-rules.json`; `is`/`has` prefixes and
leading underscores are recognized. This is a structural heuristic, not proof
that every implicit state machine has been found. It does not infer aliases,
arbitrary boolean expressions, cross-scope relationships or domain semantics.

Findings use file, owner, rule and binding names rather than line numbers.
New findings, stale entries and any identity added to the historical baseline fail.
`node scripts/check-state-machines.mjs --report` prints findings with locations.
After removing debt, `--write-baseline` records the reduction and refuses additions.
The first baseline records existing debt; later baselines may only shrink.
Parser, scanner and history failures fail the check.

The outbox retains one compatibility baseline for the public and persisted
`OutboxEntry` fields. Its runtime model lives in
`internal/runthrough/outbox_lifecycle.go`: publication is prepared or published;
propagation is ready, retry scheduled or terminal. Claim/preparation leases and
attempt history are independent of those phases. Publication, failure and lease
updates use named transitions, with existing mutexes and file transactions owning
atomicity. A retry schedule becoming due permits an attempt; it does not erase
history. Releasing ownership retains the obligation to reconcile an earlier
attempt. Terminal failure prevents further claims until the intent is removed.

## Go standards

- `gofmt -l`, `go vet ./...` and `go test ./...` must pass.
- `packages/stow-s3/go.mod` prevents Go discovery from entering TypeScript `node_modules`.
- `tools/quality` reports functions over 200 lines, complexity over 15, and more than five parameters.
- `any` and `panic` usage count as type or safety escapes.
- Storage, S3 and run-through tests share the release gate.
  Backend-specific exemptions require a written reason.

## TypeScript standards

The flat ESLint configuration sets warning ratchets for:

- complexity over 15;
- nesting depth over 3;
- more than five parameters;
- functions over 200 lines, excluding blank lines and comments;
- `console`, non-null assertions, explicit `any` and `as unknown as`.

Duplicate imports, `@ts-ignore`, `@ts-nocheck` and undocumented `@ts-expect-error` are hard errors.
Type checks, Node tests and generated-output checks must also pass.

The separate type-escape ratchet tracks `as any`, double casts, explicit `any` and suppression comments by source identity.
Changing ESLint rules does not remove these recorded escapes.

## Duplication and size

`jscpd` checks TypeScript duplication. Go quality and file-size checks track structural metrics and file size.
Cross-language clone detection is not implemented. Each existing dimension has a separate ratchet.

Tracked `packages/stow-s3/dist` output is excluded from lint and duplication scans.
`make check-generated` compares it with a fresh build.
Capture the saved digest before rebuilding; otherwise stale output could pass.
Corpus jobs also run `git diff --exit-code -- packages/stow-s3/dist`.
See [Contributing](../CONTRIBUTING.md).

## The site is gated on its claims, not on its prose

`scripts/check-site-claims.mjs` checks the homepage, six launch cards, `agent.md` and `llms.txt`.
It verifies install commands, binary/WASM sizes and default quotas against the source and built artifacts.
It rejects four known invalid forms: `go get /pkg/stow`, `go get pkg/stow`, `Backend: memory` and `@chs/stow`.

This gate cannot assess writing quality or product positioning.
Review those manually. Fixture tests include commands split across lines.

## Platform rationale held here rather than in the code

Keep long explanations here. Code comments should explain local choices without repeating the guide.

- **Windows directory sync:** `atomicfile.syncDir` is a no-op.
  Windows rejects `FlushFileBuffers` on directory handles with `ERROR_ACCESS_DENIED`.
  POSIX directory fsync and Windows journaled rename provide different durability behavior.
  A stronger Windows design would need to flush file handles before rename.
- **kqueue process watching:** closing a descriptor removes its registered filters.
  Keep it open while the watcher waits. `kevent()` returns delivered event count, not registered change count.
- **Offline flag parsing:** the flag package parses directly into configuration.
  `serve` uses `ExitOnError`, so invalid flags exit before configuration is used.
  Do not reread and silently reinterpret a parsed flag.
- **Negative SigV4 tests:** include every required signed header.
  Otherwise a missing-header error can hide the malformed input the test intends to reject.

## Documentation style

Use the [writing rules in Contributing](../CONTRIBUTING.md#documentation-style).

## CI

`make standards` is the local equivalent of the required CI quality job.
The release workflow runs it after dependency installation.
Quality failures must fail the job.
