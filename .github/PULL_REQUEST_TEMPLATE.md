# What a change is expected to do before review

- [ ] `make standards` passes. That is the whole check list, and CI runs exactly
      that target: formatting, vet, the race suite, the maintainability
      ratchets, the coverage floor, the version and install-surface gates, the
      documentation gates, the script tests, the TypeScript standards, and the
      generated-output check.
- [ ] New behaviour has a test that could fail. If a test cannot fail, the
      feature is probably not reachable — check that first.
- [ ] Any CLI surface you touched is reflected in the documents. The gate at
      `scripts/check-doc-commands.mjs` runs the built binary and fails if a
      document names a command, verb, or flag that does not exist, so a rename
      will tell you which files to update.
- [ ] A change to a `scripts/baselines/*.json` file is a deliberate decision with
      a stated reason. Those files are ratchets, not thresholds, and regenerating
      one to make a build green is the thing they exist to prevent.
- [ ] Observable S3 behaviour: say whether the change moves clients toward or
      away from real AWS.

## One logical change

Keep unrelated edits out. The release workflow builds a binary from a tag, and a
mixed diff is hard to review and harder to revert. If a drive-by fix is genuinely
unavoidable, put it in its own commit and say so.

## Adding a workspace verb

The verb list is a table in `cmd/stow-s3/workspace.go` and the usage line is
derived from it, so the entry is the source of truth. Then add the TypeScript and
Python wrappers and a step to `conformance/workspace/cases.json`. That case file
is read by three independent drivers; a driver with a private copy of it would be
asserting its own beliefs rather than the shared contract.
