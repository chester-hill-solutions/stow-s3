# Stow Code Standards

Stow uses ratcheted code-quality checks modeled on CallCaster and GoCanvass. Existing debt is recorded once in a checked-in baseline; new violations fail CI. Baselines may shrink when debt is removed, but they may not grow to make a change pass.

## Commands

Run the complete local gate with:

```sh
make standards
```

Individual gates:

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

The TypeScript wrapper also exposes the equivalent package-local commands through `packages/stow-s3`:

```sh
npm run lint
npm run check:lint-ratchet
npm run check:type-escapes
npm run check:dry
```

## Ratchet policy

- `scripts/baselines/go-quality.json` records Go quality identities.
- `scripts/baselines/lint-ratchet.json` records TypeScript warning counts.
- `scripts/baselines/type-escapes.json` records TypeScript escape identities.
- `scripts/baselines/dry.json` records TypeScript duplication counts.
- `scripts/baselines/file-size.json` records oversized-file identities.
- `scripts/baselines/go-coverage.json` records the current Go coverage floor; improvements require an explicit baseline update and regressions fail.
- A new identity fails the gate.
- A stale identity also fails the gate, forcing the baseline to be lowered after debt is removed.
- A baseline-generation command is allowed only as an explicit maintenance action after reviewing the diff. It is not a way to approve a regression.
- CI checks out full history (`fetch-depth: 0`) and compares against the explicit parent/merge-base; a shallow checkout is a hard failure, never a skipped ratchet.
- Inline suppressions are themselves counted where the check can detect them. A suppression requires a precise explanation and does not reset the ratchet.
- Existing test fixtures and generated output are excluded only when their exclusion is documented in the check configuration.
- The Go comment budget (`comment-lines`, `comment-ratio`) covers **non-test Go only**. Test files are excluded from the comment budget alone; the `any` rule was already scoped away from tests before this, and every other structural rule — complexity, parameter count, function length, file size — still applies to them. A test comment says what a test proves; a production comment says why the system is shaped as it is; counting them in one budget made them compete, and half the budget was test prose, so documenting a test properly had to be funded by deleting documentation from unrelated shipped code. Test files remain held to the coverage ratchet, `gofmt` and `go vet`.

## Go standards

The Go gate combines hard correctness checks with ratcheted structural checks:

- `gofmt -l` and `go vet ./...` are hard failures.
- `go test ./...` is a hard correctness gate.
- `packages/stow-s3/go.mod` is an intentional nested-module boundary so Go package discovery never walks TypeScript `node_modules` after an npm install.
- `tools/quality` reports functions over 200 lines, cyclomatic complexity over 15, and functions with more than five parameters.
- `any` and `panic` use are tracked as type/safety escape hatches.
- The shared storage, S3, and run-through tests are part of the same release gate; backend-specific exemptions are not allowed without a written reason.

## TypeScript standards

The wrapper uses a flat ESLint configuration with warning-level ratchets for:

- cyclomatic complexity (`15`);
- maximum nesting depth (`3`);
- maximum parameters (`5`);
- function length (`200` lines, excluding blank lines/comments);
- `console`, non-null assertions, explicit `any`, and `as unknown as`.

The following are hard errors:

- duplicate imports;
- `@ts-ignore` and `@ts-nocheck`;
- undocumented `@ts-expect-error`;
- generated `dist` drift;
- typecheck and Node test failures.

The TypeScript escape ratchet additionally records `as any`, double casts, explicit `any`, and suppression comments by source identity. This is deliberately separate from ESLint so type-boundary escapes cannot silently disappear behind a rule configuration.

## Duplication and size

`jscpd` tracks duplicated TypeScript structure. The Go quality and file-size gates track Go structural metrics and file size. Cross-language Go clone detection is not yet part of the gate and must not be implied by this document; it is a follow-up before claiming full cross-language DRY coverage. Each implemented dimension is ratcheted independently.

Generated `packages/stow-s3/dist` is checked into the repository for release reproducibility, but it is excluded from lint and duplication scans. The build is cleaned and regenerated, and CI fails if the checked-in output differs.

`make check-generated` compares the committed `dist` against a fresh build of the tree, so a committed artifact that the current source does not produce fails it. It is not a check that the build is repeatable from itself, which is what it was for a while: the target ran the build before the comparison, so the two digests were both of fresh output and a stale artifact passed. The corpus jobs' `git diff --exit-code -- packages/stow-s3/dist` is the same comparison and stays as an independent second opinion. See [CONTRIBUTING](../CONTRIBUTING.md).

## The site is gated on its claims, not on its prose

`scripts/check-site-claims.mjs` checks `site/index.html`, the six launch cards,
`agent.md` and `llms.txt` for claims that can be compared against the tree: the
install commands each prints, four short forms that do not resolve
(`go get /pkg/stow`, `go get pkg/stow`, `Backend: memory`, `@chs/stow`), the
binary and WASM sizes against the built artefacts, and the default quotas against
`internal/runtime/types.go`.

It exists because all four of those were wrong while every other gate stayed
green. `check-install-surface` covers the four documents an agent reads and did not
cover `index.html`, so the page a person lands on was the one surface nothing
checked — and it told readers to run a Go command that resolves to nothing, showed
a Go snippet that does not compile, and told them the social card to install a
package name that does not exist.

What the gate deliberately does not do is judge the writing. The page was a
generation out of date in positioning, selling an S3 test-fixture product for a
project whose centre of gravity is agent workspaces and portable checkpoints, and
no amount of substring matching catches that. A gate that cannot see the change
under test is worse than no gate, because it is green, so this one checks only what
it can actually see and says so.

`scripts/check-site-claims.test.mjs` tests the decision against fixtures rather
than only running it. That is not ceremony: the first version of the gate collapsed
only spaces, so a deliberately reintroduced `go get /pkg/stow` — wrapped across a
newline, as the real line is — went unreported, and the running check was green.

## Platform rationale held here rather than in the code

Long platform and gate arguments live here so the code carries the rule and this
document carries the reasoning. Each was originally a comment block; the comment
line ratchet (`comment-lines`, see [Ratchet policy](#ratchet-policy)) makes prose
in shipped `.go` the most expensive place in the repository to keep anything.

- **Windows parent-directory sync.** `atomicfile.syncDir` is a no-op on Windows,
  and that is the platform's answer rather than a gap. `os.Open` on a directory
  succeeds and `File.Sync` then calls `FlushFileBuffers` on the handle, which
  Windows refuses for a directory with `ERROR_ACCESS_DENIED`; while the arm
  reported that error, every workspace test failed at manifest write, so the
  workspace backend could not persist at all. The durability argument differs
  rather than being skipped: on POSIX a rename is atomic but the directory entry
  recording it is not durable until the directory is fsynced; on Windows the
  rename goes through `MoveFileEx`, the filesystem journals directory metadata,
  and there is no supported way to flush a directory handle. A caller needing
  more there must `FlushFileBuffers` the file handle before the rename, which is a
  different design. Reporting an error is worse than silence — every write would
  fail where the write is in fact fine.
- **kqueue process watching.** A `kqueue` filter is scoped to the open
  descriptor, so closing the descriptor removes every filter registered against
  it and nothing about the filter survives it. Closing it on return, with no
  goroutine ever waiting on the kqueue, is what leaves a macOS watch unarmed while
  every layer above reports success. `kevent()` also returns a count of delivered
  events, not of registered changes.
- **The `--offline` flag is not read back.** Reading a flag after the fact and
  re-parsing its string can fail, and it fails silently: a value that cannot be
  read leaves the config at `false`, so the network stays open on a flag whose
  whole purpose is closing it. Two things prevent that state. The flag package
  parses the value, so an unparseable one never becomes a config; and `serve`'s
  flag set is `ExitOnError`, so the parse error has printed why and exited before
  a config exists — which is why `serve` discards the error from `flags.Parse` and
  why discarding it is safe rather than merely convenient.
- **Negative SigV4 cases carry the whole signed header set.** With a shorter
  list, a repairing verifier produces one missing a required header, and the
  ordinary "the date and the content hash must be signed" rule refuses the request
  for a reason that has nothing to do with strictness. Such a case passes whether
  or not the parser is strict, which is how a suite of fifteen negative cases can
  be entirely green against the behaviour it is meant to pin.

## CI

`make standards` is the local equivalent of the required CI quality job. The release workflow must run the same command after dependency installation. A quality failure is never converted into a warning-only job.
