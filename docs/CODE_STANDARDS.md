# Stow Code Standards

Stow records existing code-quality debt in checked-in baselines.
New violations fail CI. Remove baseline entries when their debt is removed.
Do not raise a baseline to make a regression pass.

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
