# Contributing

Thanks for considering it. This is a small project with one maintainer, so the
bar for a merged change is mostly about being reviewable rather than about
cleverness.

## Before you start

- **Open an issue first for anything larger than a bug fix.** A design that
  arrives as a patch is much harder to steer than one that arrives as a
  question, and the reason a change looks surprising is usually a decision that
  was made for a reason nobody wrote down.
- **Check whether the behaviour is already specified.** `docs/adr/` records
  decisions that were settled deliberately, sometimes at a cost. If your change
  contradicts one, say so in the issue rather than in the diff — the ADR is
  usually wrong in an interesting way and worth revisiting properly.
- **Read the issue tracker before filing.** Thirty issues exist and twenty-eight
  are closed; some of what looks new has been considered and rejected with a
  reason.

## Building and testing

```bash
make build            # the native binary, built the way the release builds it
make test             # the Go suite
make test-race        # what CI runs
make test-conformance # the shared S3 contract, across every backend
make test-all         # Go, conformance, TypeScript, Python, WASM
```

You need Go 1.25.6 (see `.go-version`; `make` pins it via `GOTOOLCHAIN`),
Node 20 or newer for the TypeScript and WASM suites, and Python 3.10 or newer
for the client suite. ESLint 10 requires Node 20.19+, 22.13+, or 24+ for
development checks; the published client still supports Node 20 or newer.
`make test-python` bootstraps its own virtualenv.

`make standards` is the gate. It is the whole check list — formatting, vet, the
race suite, the maintainability ratchets, the coverage floor, the version and
install-surface gates, the documentation gates, the script tests, the TypeScript
standards, and the generated-output check. Run it before you open a pull request.

One caveat, and it is the thing most likely to surprise you: the committed WASM
artifact has to match a fresh build, and if your change reaches `pkg/stow` — which
most Go changes do — it will not, until you regenerate and commit it. The WASM
runtime links 14 packages of this tree, including `internal/rooted`,
`internal/storage/workspace`, `internal/storage/fs` and `internal/capacity`, so
almost any Go change moves that binary.

`make check-generated` is what tells you, and it runs as part of `make standards`.
It compares the committed `packages/stow-s3/dist` against a fresh build of the
tree, so a stale artifact fails locally, on CI, and in the release job. The build
is byte-reproducible across hosts — the same Go version and the same
`make build-wasm` produce the same bytes on macOS and on Linux — so a macOS
checkout can regenerate and commit the artifact, and there is no platform rule to
remember.

It is worth knowing how that gate was broken once, because the failure was silent
and shipped twice. The target ran `npm run build` *before* the script that
compares, so the script's first digest was a digest of a fresh build and it
compared a build against itself: a committed artifact that no source produced
still passed, and the message said "reproducible". Only CI's separate
`git diff --exit-code -- packages/stow-s3/dist` noticed. If you ever change that
target, check that it still fails when you deliberately install a stale artifact
— a check you have never seen fail is not known to work.

## The ratchets, and what they mean for your diff

`make standards` includes gates that fail when a number gets worse. They are
ratchets, not thresholds, and three of them will shape your change:

- **Comment volume.** `scripts/baselines/go-quality.json` fails on an *increase*
  in comment lines and comment ratio. The project is deliberately cutting prose.
  When you need a comment, keep the contract or the invariant and delete the
  paragraph explaining what the code used to do. A useful test: if your comment
  would still make sense to someone reading the code before the change, it is
  probably history rather than contract.
- **Complexity, parameter count, and `any`.** New violations fail. Do not fix one
  by editing the baseline — the tool says to regenerate only after intentional
  debt reduction, and adding an entry is the opposite. Split the function.
- **Coverage.** The floor is the count of *covered statements*, not the
  percentage, because adding well-tested code with defensive error branches
  legitimately lowers a percentage. Add tests for behaviour, not for lines.

There is a documentation gate that runs the built binary and checks that every
command, verb, and flag named in the ten declared documents exists
(`scripts/check-doc-commands.mjs`). If you rename anything on the CLI, that gate
tells you which documents still name the old spelling.

## Commit and pull request shape

- One logical change per pull request. The release workflow builds a binary from
  a tag, so a mixed diff is hard to review and harder to revert.
- Write the commit message for the person who has to revert your change at 3am:
  what broke, what moved, and anything a future reader would otherwise have to
  reconstruct from `git blame`.
- Say what you verified and how. "Ran `make standards`" is useful; "should work"
  is not.
- If your change alters observable S3 behaviour, say whether it moves a client
  toward or away from real AWS. Fidelity to the real service is a design goal,
  and a deviation needs a reason someone can check.

## Adding a workspace verb

The verb list is a table in `cmd/stow-s3/workspace.go`, not a switch, and the
usage line is derived from it. Add the entry, register the TypeScript and Python
wrappers, and add a step to `conformance/workspace/cases.json` — that case file
is read by three independent drivers (Go, TypeScript, Python), and a driver that
grew a private copy of it would be asserting its own beliefs instead of the
shared contract.
