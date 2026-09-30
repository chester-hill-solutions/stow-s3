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
for the client suite. `make test-python` bootstraps its own virtualenv.

`make standards` is the gate. It is the whole check list — formatting, vet, the
race suite, the maintainability ratchets, the coverage floor, the version and
install-surface gates, the documentation gates, the script tests, the TypeScript
standards, and the generated-output check. Run it before you open a pull request.

One caveat, and it is the thing most likely to surprise you: CI runs
`make standards` *and* one check the local target cannot make. The three
TypeScript corpus jobs regenerate the package output and then run
`git diff --exit-code -- packages/stow-s3/dist`, and that diff is the only thing
comparing the committed WASM artifact against a fresh build. `make
check-generated` cannot do it: it digests `dist`, rebuilds, and digests again,
and the first digest is already taken after `npm run build` has copied the newly
built binary over the committed one. It proves the build is idempotent, not that
the committed bytes are current. A stale artifact passes every local gate and
fails three required checks.

So if your change reaches `pkg/stow` — which most Go changes do — the committed
`packages/stow-s3/dist/stow-runtime.wasm` is stale, and nothing local will tell
you. The WASM runtime links 14 packages of this tree, including
`internal/rooted`, `internal/storage/workspace`, `internal/storage/fs` and
`internal/capacity`, so almost any Go change moves that binary. And the build is
**not** byte-reproducible across host OS: same Go version, same `make build-wasm`,
same `-trimpath`, but darwin/arm64 and linux/amd64 emit different bytes. The
committed artifact has to be a Linux build, and a macOS developer cannot produce
one at all.

Until that is settled, regenerating it means building on a Linux runner and
committing the result. A throwaway pull request whose only job is
`make build-wasm` on `ubuntu-latest` is the cheapest way. Do not commit a macOS
build to quiet the diff — it fails the same check, and for the same reason.

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
