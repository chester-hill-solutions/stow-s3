# The workspace contract's step vocabulary

`conformance/workspace/cases.json` is a specification, not a fixture. It is read by
three drivers — the Go runner against the binary, the TypeScript wrapper, and the
Python wrapper — and every driver has to reach the same conclusion from the same file.
This page is the schema for a step: what a key does, and which of the three drivers
owns it.

The case file's own `description` field explains *why* the scenario exists and how the
harness behaves. This page is the field-by-field reference, and it is checked.

## Why this page is checked

The three drivers share no type: a Go struct, a TypeScript interface, a Python dict.
Nothing made them agree, and they did not.

`includeAdopted` was declared on the TypeScript interface, read by the TypeScript and
Python drivers, and absent from the Go struct, so the three disagreed about what a
`prune` step means. `tsc` caught the TypeScript half — on a file that had never
compiled — and the case file passed, because a field no driver reads is not an error
in any of the three languages: Python's `.get` returns `None`, Go's decoder drops it,
and TypeScript has nothing left to check once the key is absent.

The same field on the *wrong* interface produced a `tsc` error that read like a typo
and was a design gap. Both times the steps ran and a typed field went nowhere.

`documentFields` was the other direction: declared in the case file, read by no driver
at all, on a step whose title said it checked the document it wrote. It asserted only
that the file appeared, which the previous step's `alsoWritten` already covered.

So two tests in `conformance/workspace_contract_drivers_test.go` hold this page to the
code: the three declared field sets must be equal, the case file may only name fields
the drivers read, and every field must appear below.

## The fields

Eighteen. Every driver declares all eighteen.

| Field | Type | What it does |
|---|---|---|
| `id` | string | The step's claim, written as a sentence. It becomes the subtest name, so it is what a reviewer checks the code against. |
| `verb` | string | The `workspace` subcommand. `noop` and `read` are the two that do not invoke one. |
| `args` | object | Flags passed to the verb, as strings. |
| `registry` | string | `a` for the driver's own registry, `default` for whatever the binary resolves with no `--registry-dir`. |
| `team` | string | `--team`, for the verbs that take it. |
| `root` | string | The tree a `read` step reports on. |
| `manifest` | object | A task manifest for `prepare`: `root`, `team`, and `inputs` stated as bodies rather than paths, because a path would be a fact about the machine that wrote the case file. |
| `write` | array | Files to put into the workspace, the way an agent editing one would. |
| `remove` | string | A path to take away, so the next step runs against a workspace whose files are gone. The caller deleting their own project. Without it a case file can only ever prove that `prune` does nothing, because a directory that is present is never prunable. |
| `documentFields` | object | Fields of the document the step wrote, checked by opening it at the same `--output` its verb was given. |
| `includeAdopted` | bool | `prune`'s opt-in to forgetting a caller's entry. A typed field rather than an `args` entry because `args` are strings, and `"true"` as a string is how a case asserts a flag that was never set. |
| `capture` | object | Field-to-capture-name, for values later steps substitute as `{{name}}`. |
| `expect` | object | Field paths against the verb's JSON, with `equals`, `notEquals`, `matches`, `length`, `minLength`, `min`, and `absent`. Dotted and indexed paths (`results[0].reason`) are resolved by one pass. |
| `alsoWritten` | string | A file the verb must have created, by name. |
| `renameInTransit` | object | Builds a substituted document: the named change is moved to a different path in the change list and the content map together, and the result re-encoded. How a document altered on the way to a receiver is stated as a refusal. |
| `returns` | string | `stdout` or `file`. The two verbs taking `--output` disagree, and that disagreement is the contract rather than an inconsistency to smooth over. |
| `tamper` | string | Corrupt a document the scenario really produced, so a refusal has to come from the digest rather than from a file that simply is not there. |
| `fail` | object | `contains`: the refusal the step expects. A step with neither `expect` nor `fail` asserts nothing, and the harness rejects it. |

## Two rules the drivers must not break

**Every field is implemented by all three.** Not "the two that are typed" — the
Python driver reads the same file through unchecked dict access, which is why the
inconsistency stayed invisible from its side. Two drivers agreeing is not evidence the
third does.

**Every driver isolates the default registry.** The contract has steps that
deliberately name no `--registry-dir`, because `adopt` takes none and that is part of
what is under test. A driver that lets the child resolve the default from the test
process's own `HOME` registers a workspace in the developer's real configuration
directory and then sweeps it. Two of the three did exactly that, on every run, for as
long as the suite existed; the Go suite saw the count it should and the Python suite
saw 72, and both were green.

`TestContractDriversIsolateTheDefaultRegistry` fails if that regresses. It reads each
driver's *directory* rather than one file, because the isolation has already been
moved out of the runner once and a path-pinned check would have called that a
regression.
