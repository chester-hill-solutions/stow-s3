# FOSS readiness plan

**Status:** proposed, not agreed
**Date:** 2026-09-28
**Scope:** what stands between this repository and being usable and contributable by
strangers. Payment, pricing, and accounts are out of scope; this is a
permissively-licensed package whose distribution is the product surface.

**Plan relationship:** [`docs/plan.md`](plan.md) remains canonical for workspace,
handoff, and deploy-anywhere status. This document covers a different axis —
installability, the conformance matrix, and the contribution surface — and the
two do not conflict. Where they touch, `plan.md` still governs ordering.

**Evidence base for this plan:** every code and distribution claim below was found
by building the tree, running the binary, driving it with
`@aws-sdk/client-s3` and two live instances of it, running the gates, and
querying the live registries. Document claims are named as such and are used only
to locate text that needs rewriting, never as evidence about behaviour.

---

## 0. Status

Recorded 2026-09-28, after the work in this plan was committed. Read this table before
the item sections: the sections below are the reasoning and were written before the
work, so they describe intentions rather than outcomes, and where the two disagree
this table is current.

| Item | State | Evidence |
|---|---|---|
| A1 `--version` | **Done** | `cmd/stow-s3/version.go`; four spellings plus bare `version`, exit 0 |
| A2 publish the five npm packages | **Blocked on one admin action** | Gate present; `published: false` is correct until the packages are public |
| A3 PyPI | **Not started** | Needs a token and a decision on whether to publish there at all |
| A4 site | **Open decision** | `stow.chesterhillsolutions.ca` returns Cloudflare 530 |
| A5 ship v0.3.0 | **Blocked on the release gate** | Needs the R2 pair as `STOW_LIVE_*` secrets |
| B1 committed write reported missing | **Done** | `TestMirrorWritesDoesNotTellTheCallerTheObjectIsMissing` |
| B2 `mirrorWrites` never created the bucket | **Done** | `TestMirrorWritesCreatesTheBucketUpstream` |
| B3 `serve --workspace` | **Not started — product decision** | Served workspace, or an honest refusal |
| C1 workspace backend in the matrix | **Done** | `conformance/workspace_contract_test.go` |
| C2 two real things talking | **Done** | `conformance/runthrough_pair_test.go`, six tests |
| D1 the four documents | **Done** | `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SUPPORT.md` |
| D2 templates and ownership | **Done** | `.github/PULL_REQUEST_TEMPLATE.md`, `ISSUE_TEMPLATE/`, `CODEOWNERS` |
| D3 dependency updates | **Done** | `.github/dependabot.yml` |
| D4 delete `EnvironmentPromote` | **Already done before this plan** | Refused by name in `internal/authority/authority.go`; only the refusal record remains |
| D5 legacy data directory | **Open decision** | — |
| E1 split `serve` | **Done, partially** | Complexity 31 → 22 against a ceiling of 15. The shutdown sequence moved the number; the flag block moved no complexity and is worth keeping for readability. `cmd/stow-s3/shutdown.go`, `serve_flags.go` |
| E2 aim the coverage ratchet at behaviour | **Not started** | The floor is still a single aggregate. Four packages sit under it, worst first: `cmd/stow-s3` at 56.4%, `tools/quality` 62.7%, `internal/atomicfile` 66.7%, `internal/s3api` 69.1% — read the numbers from `make check-coverage` |
| E3 object record layout cost | **Not started** | — |
| F1 README restructure | **Not started** | — |
| F2 agent-facing documents | **Not started** | Partly overtaken: the cache section was added to the skill |
| F3 extend the doc gate | **Done** | `scripts/check-doc-commands.mjs`, in `make standards`; `scripts/check-adr-index.mjs`, which makes an ADR amendment something you have to record |
| F4 document the contract's step vocabulary | **Done** | `docs/workspace-contract-steps.md`, checked against the three drivers by `TestContractStepVocabularyIsDocumented`. Written because `includeAdopted` reached two of three drivers and the case file passed anyway |
| G1 ownership must not survive a handoff | **Done** | `Options.Adopted`; `internal/storage/workspace/destroy_test.go` `TestAdoptedManifestDoesNotCarryOwnership`; five contract steps, red if the fix is reverted. `workspace restore` deliberately stays owned — rebuilds scratch rather than taking over a project, and that judgement is open |
| G2 contract drivers must isolate the default registry | **Done** | `TestContractDriversIsolateTheDefaultRegistry`. The TypeScript and Python drivers registered in and swept the developer's real `~/.config` on every run; the Go driver had always isolated its child and said why in a comment |

One item in this plan was answered by a decision rather than by code, and that
decision is now [ADR 0012](adr/0012-run-through-serves-only-its-own-buckets.md): a
run-through server refuses a bucket it was not given, rather than fetching any bucket
the upstream credential can see. The natural fix for B1 is the wrong fix for the
credential boundary, and the reasoning belongs somewhere more durable than a branch
comment.


## 1. What the evidence says

Four facts dominate everything else.

**One install path works, and it ships a pre-architecture binary.** The Go module
resolves from the public proxy and its embedded API runs in a fresh module. The
repository's own gate, run against the live registries, reports `1/3 published`:
npm answers 401 and PyPI answers 404. The five npm packages are not anonymously
readable, so the documented `npm install` cannot work for anyone outside the
organisation. The product domain answers 530.

And what the install paths deliver is not the product. `v0.2.0` is a tag, on
`0eaf47c`, commit 106 of 200, with no GitHub Release. At that commit `pkg/stow`
is five files — `doc`, `errors`, `runtime`, `runtime_test`, `types` — with no
`workspace.go`, `prepare.go`, `checkpoint.go`, `delta.go`, `resume.go`, or
`authority.go`. `cmd/stow-s3` at that tag has `serve` and `doctor`, zero
workspace verbs, no `prewarm`, and no `--offline`. 204 files under `pkg/`,
`internal/`, and `cmd/` differ from HEAD.

So `go get ...@v0.2.0`, the five npm packages at `0.2.0`, and anything a
subscriber already resolved all point at a snapshot with **neither** of the
product's two pillars in it. A public tag cannot be moved, so this is not
repairable by re-tagging.

**The binary cannot identify itself.** `internal/version/version.go:4` holds the
single source, and it surfaces only through `doctor`, `/_stow/status`, and the
readiness record. `stow-s3 --version` prints usage; `stow-s3 serve --version`
exits on `flag provided but not defined`. A user handed a tarball has no way to
ask what it is, and a bug report cannot carry the answer.

**The largest store in the repository is outside the contract that defines
correctness.** `internal/storage/workspace` is a complete S3 store: buckets,
objects, copy, conditional copy, list, and sixteen methods on `Store` in
`multipart.go`, 2,835 lines. `conformance/harness_test.go:68-79` accepts exactly
three backends — `memory`, `filesystem`, `runtime` — which `make test-conformance`
expands into four runs because `runtime` is exercised over both backing stores.
The workspace backend is not among them, and a run naming it fails outright
rather than skipping. It is also never served: its only construction site is
`pkg/stow/workspace.go:117`, reached by the workspace verbs, which hand back a
directory rather than an endpoint. The one surface an agent receives is the one
surface no conformance run has ever checked.

That last fact, together with the one below it, is the plan's centre.

**No test makes two instances talk to each other.** The two defects in section 3
were found by hand, in one session, by running the product against itself. Both
are properties of a *pair*: a local store and an upstream, an agent and a
network. The conformance harness starts a single server, and `internal/runthrough`
tests against a hand-written `stubUpstream` that cannot reproduce a real S3
server's bucket semantics. The category of bug that costs the most to find is the
one this test topology cannot see, and it is the category this product's whole
value proposition sits in.

---

## 2. Phase A — installable and identifiable

Nothing else in this plan reaches a stranger until these land. They are ordered
by dependency, not by severity.

### A1. Make the binary identify itself

Add `stow-s3 version` and a `--version` flag on every subcommand, all reading
`internal/version.Version` so the answer has one source. `doctor` already reports
it (`cmd/stow-s3/doctor.go:134-135`); this makes it answerable without running a
diagnostic.

Accept: `stow-s3 --version`, `stow-s3 serve --version`, and
`stow-s3 workspace --version` all print the version and exit 0. Assert it in
`cmd/stow-s3/main_test.go` and assert that the flag answer equals
`doctor --json`'s `binaryVersion`, so the two cannot drift.

Gate: `make test`, `make standards`.

### A2. Publish the five npm packages, with the prose, in one commit

This is one change and it cannot be split. `scripts/check-install-surface.mjs`
documents the sequence in its own header: set each package public in its GitHub
package settings, set `published: true` for the npm entry, and update the
install prose in every file `DOCS` names. The gate compares the declared state
against the registries, so a commit that flips `published` before the packages
are public produces a 401 and a red build that says nothing useful, and a commit
that updates the prose first trips the disclaimer rule.

The single commit carries:

| File | Change |
|---|---|
| GitHub package settings (not a commit) | All five `@chester-hill-solutions/stow-s3*` packages set to public |
| `scripts/check-install-surface.mjs` | `published: true` on the npm entry |
| `README.md` | Working `npm install`; remove the not-published disclaimer |
| `site/agent.md` | Same |
| `site/llms.txt` | Same |
| `skills/stow-s3/SKILL.md` | Same; this is the document a coding agent is handed before anything else, and its install section currently tells the agent to use the Go module because the other two do not work |

Accept: `node scripts/check-install-surface.mjs --online` reports `3/3
published`; an unauthenticated `npm view @chester-hill-solutions/stow-s3`
resolves; `make check-scripts` and `make check-install-surface` pass.

Risk to manage: step 1 is an admin action in GitHub's UI and cannot be reviewed
in the diff. Do it immediately before the commit, not days earlier, or the
ordering the gate depends on is unverifiable from the history.

### A3. Turn on PyPI

`release.yml:377-392` gates publication on the `STOW_PUBLISH_PYPI` repository
variable, which is unset, and a trusted publisher is not configured on pypi.org.
Configure the trusted publisher, set the variable, and publish. The wheel
plumbing — four platform tags, the executable-bit check at `release.yml:141-156`
— is already correct; only the pypi.org side is missing.

Accept: `pip install "stow-s3[boto3]"` resolves on all four supported platforms
and the client starts a session.

### A4. Decide the site's fate

`stow.chesterhillsolutions.ca` returns 530. `site/` has 899 lines of HTML, CSS,
and a `serve.py`, plus a Playwright render step for the launch cards, and
nothing in the Makefile or any workflow deploys it. Meanwhile
`skills/stow-s3/SKILL.md` publishes that URL as machine-readable instructions —
a URL an agent is told to fetch and cannot.

Choose one, and do not leave it pending:

- **Deploy it.** Add a workflow, commit the generated `site/images/*.png` as the
  render step already produces, and add a link check to CI so a dead URL fails
  the build rather than the agent.
- **Remove it.** Delete the domain from `SKILL.md`, `site/agent.md`, and
  `site/llms.txt`, and either delete `site/` or mark it clearly unpublished.

The link check belongs either way: the failure mode that produced A4 is a URL
that resolves at authoring time and not later, which only a network check in CI
catches.

### A5. Ship a version that contains the product

This is no longer "cut a release". `v0.2.0` is already a public tag, and it
points at a commit with neither pillar in it. The version number in
`internal/version/version.go`, the `v0.2.0` tag, and the five npm packages at
`0.2.0` all describe a binary that cannot do the two things the product is for.

The tag is immutable now, so the options are:

| Option | Cost | Consequence |
|---|---|---|
| **Cut `v0.3.0` from HEAD** | one tag plus the release workflow | A subscriber who pinned `v0.2.0` stays on the broken snapshot until they notice. Needs a changelog entry that says so in the first line, not in a footnote |
| Cut `v0.3.0` and republish `0.2.0` as a yank | npm and PyPI yanks, Go proxy cannot be yanked | A Go user who pinned `v0.2.0` cannot be rescued at all. The Go module is the one install path that works, so this is the one that matters most |
| Do nothing | zero | `go get` hands every new user a pre-architecture binary and `internal/version` claims `0.2.0` for it |

Take the first, and put the discrepancy in the release notes' first paragraph:
`v0.2.0` predates the workspace and cache pillars, `v0.3.0` is the first version
whose module and binary contain them. Silence about a version that was published
and does not do the product is the worst of the three.

Then add a release checklist so the next tag does not drift from `HEAD`: the tag
must be on a commit that passes `make test-all` and `make standards`, and the
notes must consume the unreleased changelog section rather than being written
twice. The gap is not a missing release — it is that nothing made a release an
event, so a tag landed on whatever commit was current and the version string was
never reconciled against what the tag contained.

Add a gate while you are there, because it is the check that would have caught
this and it is four lines: `internal/version.Version` must equal the version in
the nearest reachable tag, or there must be no tag ahead of it. `scripts/check-version.mjs`
already reconciles the platform package manifests against the version constant;
extend it to the tag rather than writing a new script.

Depends on: A2 and A3, so the release notes can state a true install surface.

---

## 3. Phase B — one defect, one half-fixed, one decision

All three items sit in code paths the test matrix structurally cannot reach, which
is the argument for C2 in the next section. B1 is fixed. B2 needs a decision before
it can be. B3 is not a defect at all; it is a decision the code currently makes by
omission.

### B1. A committed local write is reported as a missing object

**Correction, 2026-09-28.** This section was first written as two defects. One of
them was not a defect, and the reason it looked like one is worth recording
because the plan's author got it wrong by reading code and running the product
without looking for an existing test of the behaviour.

I reproduced this by hand: a run-through server pointed at a live upstream holding
`prod-data/config/app.yaml` answers `NoSuchBucket` for a bucket the local store had
never been given, and starts working once that bucket is created locally. I read
the refusal as an unhandled edge case and planned to fix it. It is a deliberate
security property: the local store is the namespace, so a client holding one
endpoint and one key pair cannot reach a bucket on the provider by guessing its
name. `internal/runthrough/runtime_composition_test.go` already asserts it,
including that the upstream is **not contacted** (`upstream.getCalls != 1`). A
plan item to "fix" it would have removed a security boundary in the name of a
friendlier error code.

The real defect is the one the reproduction actually showed, and it is below.

Under `mirrorWrites` with `STOW_ALLOW_LIVE_WRITES`, a bucket created through a
run-through client never reaches the upstream, because `CreateBucket` is routed to
the local store and nowhere else (`internal/runthrough/adapter.go:259-261`). The
first object write to that bucket propagates into an upstream that has never heard
of it, the attempt terminally fails, and `PutObject` returns
`storage.CommittedError` — whose cause is the upstream's not-found. The storage
error table maps not-found to a 404 `NoSuchBucket`, so **the server told the
caller that the object did not exist immediately after writing it.** A caller
that read the key back would find it; a caller that retried on the 404 would never
succeed, because nothing about the next attempt differs.

That error mapping was the fixable half, and it is fixed: `ErrMutationCommitted`
is now checked ahead of both the upstream table and the storage table in
`internal/s3api/errors.go`, so a committed write reports 5xx with a message saying
the write is local and a retry is safe, rather than a 404 denying the object
exists. The cause is not repeated in the message, for the same reason the upstream
table does not repeat it.

The propagation half is **not** fixed and should not be without a decision.
Propagating bucket creation means either reusing `UpstreamWrite` — which
contradicts the model's own rule that creating a namespace "is not an object
operation and does not inherit its permissions" — or adding an operation to a
deliberately closed set, which changes what a caller can narrow an `Authority`
against. `conformance/runthrough_pair_test.go` carries that assertion skipped,
with the reproduction written and the decision named, so the gap shows up in the
test output rather than only in this document.

### B2. mirrorWrites never creates the upstream bucket

The other half of the same reproduction, and the part that needs your decision
rather than a patch. `CreateBucket` routes to the local store and nowhere else, so
a bucket made through a run-through client does not exist upstream, and the first
object write to it propagates into a provider that has never heard of the bucket.

Measured: `outbox_terminal: 1`, `last_error: "object not found"`, upstream read
`NoSuchKey`. The same write to a bucket that already existed upstream propagated
correctly within about a second. The status route reports healthy throughout.

B1 removed the part of this that lied to the caller. What remains is that the
propagation does not happen, which is a consent question rather than a bug:

Fix: propagate bucket creation under whichever grant the decision names, using
the same outbox so the ordering is guaranteed and a failure is visible in the
outbox rather than at first object write. Do not
propagate bucket deletion without a separate decision — a delete that races an
in-flight object write is a different risk from a create that precedes it.

Test: a two-instance test asserting a locally created bucket reaches upstream,
and that `DeleteBucket` behaviour is the one the chosen decision specifies.

### B3. A served workspace, or an honest refusal

Two defensible outcomes, and the choice is a product decision, not an
implementation detail:

- **Serve it.** Add a way to run an S3 endpoint over an existing workspace, so
  the two halves the repository already has — a filesystem an agent can edit and
  a store that answers the S3 protocol against it — are reachable from one
  command. This is the combination the code is already built for and the one a
  reader would assume `workspace prepare` gives them.
- **Refuse it clearly.** Keep the current boundary and correct
  `cmd/stow-s3/main.go:104-113`, whose error tells the caller to "use the
  embedded API" without saying that the workspace verbs already construct this
  backend for file operations, and that no HTTP route exists for it at all.

If the answer is "refuse", the sentence belongs in the `serve` usage text and in
`README.md`, not only in an error string a user meets after typing the wrong
flag.

---

## 4. Phase C — two test harnesses the matrix does not have

Two distinct gaps, and conflating them would hide both.

### C1. The conformance matrix does not reach the workspace backend

`conformance/harness_test.go:68-79` switches on `STOW_CONFORMANCE_BACKEND` and
fails on anything outside `memory`, `filesystem`, `runtime`. Adding a `workspace`
case is small: build `workspace.New` over `t.TempDir()` and wrap it with
`runtime.OpenWithStore`, exactly as `newRuntimeConformanceStore` already does at
`harness_test.go:82-107`. Then add the configuration to the four lines in
`make test-conformance`.

That runs the shared 22-case corpus and the shared workspace contract against the
store an agent actually gets. It runs on `ubuntu-latest` only: the conformance
job at `ci.yml:74-94` has no matrix, and the separate `filesystem-platforms` job
runs only `internal/storage/fs` and `internal/storage/workspace`, not conformance.
Widening conformance to macOS and Windows is a third change, and a real one,
because the workspace store is a filesystem store and its case-folding and
namespacing behaviour is exactly the kind that differs by platform. Sequence it
after the backend is in the matrix, so the failures it surfaces are attributable.

This harness would not have caught B1 or B2. It exercises one store at a time.

### C2. No test makes two real things talk to each other

Both defects in section 3 are properties of a *pair* of instances, and nothing in
the repository asserts one. The conformance harness starts a single server.
`internal/runthrough`'s tests use `stubUpstream`
(`internal/runthrough/upstream_config_test.go:56`), a hand-written implementation
of the client interface, which by construction cannot reproduce a real S3
server's bucket semantics — which is precisely what B2 is about.

Add a two-instance harness under `conformance/`, which already imports both
`internal/s3api` and `internal/runtime` and so has no import problem: bind a real
`s3api.Server` on `httptest` as the upstream, point a run-through adapter at it
through `runthrough.NewS3Client`, and assert across the pair. One server for the
upstream, one for the client under test, and the real AWS SDK between them.

Reproducing B1 and B2 by hand took one session. Each assertion is a few lines
once the harness exists, and the harness is what stops a third arriving
unnoticed.

Accept: `make test-conformance` runs five backend configurations; the
two-instance suite covers B1 and B2; `go test -race -count=1` stays green.

---

## 5. Phase D — the contribution surface

A permissive licence with no way to contribute is a binary, not a project. None
of these files exist today, and `.github/` contains only workflows.

### D1. The four documents

| File | Must state |
|---|---|
| `SECURITY.md` | Where a vulnerability report goes, and what the blast radius is. This package runs with the caller's filesystem and network privileges and impersonates S3 to the caller's own code, so the honest boundaries are the ones the code already draws: the capability attenuation in `internal/authority/authority.go:75-90` and the admin-route rules in `internal/s3api/server.go:315-356`. The gap worth stating plainly is that the workspace is a directory, not an OS sandbox — a caller who hands a workspace to an untrusted agent has handed it a filesystem |
| `CONTRIBUTING.md` | `make build` then `make test-all`; that `make standards` is the gate; that a change to a `scripts/baselines/*.json` file is a deliberate debt decision and needs a stated reason, because the ratchets treat a baseline edit as unapproved debt |
| `CODE_OF_CONDUCT.md` | A real one. Contributor Covenant 2.1 unmodified is the cheapest correct answer |
| `SUPPORT.md` | Issues for bugs — thirty exist and twenty-eight are closed, so the practice works. There is no discussion forum, and the honest thing is to say which of the two questions belongs where rather than to imply a channel that does not exist |

### D2. Repository templates and ownership

An issue template that asks for the three things this codebase needs and cannot
infer: `stow-s3 version` output from A1, the `doctor --json` report
(`cmd/stow-s3/doctor.go:43-49` already emits the right shape), and whether the
report involves run-through, a workspace, or the S3 protocol surface. A PR
template pointing at `make standards`. `CODEOWNERS` over `internal/runthrough`,
`internal/s3api`, and `scripts/baselines`.

### D3. Dependency updates

There is no `dependabot.yml` and no `renovate.json`. The tree carries 12 Go
modules and a large npm dependency set, and the release workflow pins every
action to a commit SHA with a version comment — a discipline that without an
update path becomes a set of SHAs that only a maintainer remembers to move.

Add `dependabot.yml` for `gomod`, `npm` (both `packages/stow-s3` and the
workspace root lockfile), `github-actions`, and `pip` for
`packages/stow-s3-py/pyproject.toml`. Group the action updates: eight distinct
actions are pinned to SHAs across the three workflows, and an ungrouped bump
opens a pull request per action instead of one reviewable change.

### D4. Delete `EnvironmentPromote`

`internal/authority/authority.go:267-270` publishes a permission with no
implementation, on the record, with a reason. The reasoning is sound and the
mechanism is the right one. For a project inviting outside contributions it is
still a constant a caller can narrow against that means nothing, and the file's
own comment describes the failure mode precisely.

Delete the constant and the `Ungated` entry for it. If a future contributor
wants it, the closed set is the right place to add it, with a test.

### D5. Decide what a legacy data directory does

`internal/storage/fs/fs.go:71-83` logs `legacy .stowmeta layout detected; legacy
data is not migrated` and returns. A user upgrading across that boundary loses
objects with one line in a log they may not be reading. For a package distributed
as tarballs with no data migration story, either migrate it or detect it in
`doctor` as a failed check, where it cannot be missed.

---

## 6. Phase E — maintainability the gates already flag

### E1. Split `serve`

`cmd/stow-s3/main.go:276-458` is 182 lines at cyclomatic complexity 36, the
largest single entry in `scripts/baselines/go-quality.json`, in the file the
ratchet is configured to allow. It parses 22 flags, decides a mode, builds a
store, wires a server, and manages a shutdown. The ratchet is right to object
and the baseline is the exception.

Extract flag parsing into a typed input struct, store construction into the
function that already exists for the local/run-through branch, and signal
handling into its own unit. The goal is to remove the `complexity` entry from
the baseline, not to move it.

### E2. Aim the coverage ratchet at behaviour

`check-coverage` already gets this right and the plan should not undo it: the
invariant is the count of covered statements — 6,656 in
`scripts/baselines/go-coverage.json` — with a one-point percentage-drop guard
against the count being gamed, because a percentage falls whenever well-tested
code is added with defensive error branches (`scripts/check-coverage.mjs:4-20`).
Leave the mechanism alone.

The gap is that a single global statement count cannot tell a well-tested
protocol surface from an untested one. Extend the baseline to a per-package
statement floor for the packages that decide behaviour — `internal/runthrough`
for the outbox state machine, `internal/storage/workspace` for the registry, and
`internal/auth` for the presigned-query path — and raise those floors
deliberately. The workspace backend is where this matters most: adding it to the
conformance matrix in C1 will move numbers, and per-package floors are what make
that movement legible instead of a single global delta.

### E3. Say what the object record layout costs

`internal/storage/fs/fs.go:22-23` stores each object as one JSON record
containing its bytes and metadata, and `packages/stow-s3/src/session.ts:14-18`
records a measured 4.59 MB of peak RSS per MiB stored. Both are honest and both
are dev-scale limits. State them where a user meets them — the fs backend's
package doc and the filesystem session limits — rather than only in a benchmark
file and a code comment.

---

## 7. Phase F — documents

The gate in `scripts/check-install-surface.mjs` names the four files that carry
install prose and checks one rule about them: a disclaimer must be present for
an unpublished target and absent for a published one. That is a narrow rule. It
catches a document claiming an install that does not exist. It does not catch a
document describing behaviour that is not what the code does, which is how
`CHANGELOG.md`'s own account of the first `prewarm` describes three defects that
all passed every gate.

### F1. README restructure

`## Install` is at line 347 of a 1,048-line README. Before a reader reaches an
install command they pass "The two pillars", "This is not version control", "What
Stow provides", "When to use it", and a 245-line workspace walkthrough. For a
package whose entire adoption problem is that nobody can get it installed, the
order is wrong.

Restructure to:

1. one line on what it is;
2. the install that works, with the Go module first because it is the only one
   that has ever resolved, and the other two labelled by their actual state
   rather than by aspiration;
3. a runnable twenty-line example;
4. a "when not to use it" section, which does not exist today. "When to use it"
   at line 84 is a ten-item positive list with no counterpart, and the negative
   cases are the ones that protect a reader: not production object storage, not a
   security boundary for an untrusted agent, not a migration tool for an existing
   data directory. `pkg/stow/prepare.go:433-435` already refuses to stage
   `.aws/credentials` and `.ssh/` paths by default, which is a promise the
   negative section should name rather than leave in a source file;
5. the two modes, with the write-consent boundary stated where a reader decides
   which to use rather than after they have started one;
6. links to the workspace verbs as a set, since there are fifteen and no one
   remembers them;
7. contributing, security, and licence, once Phase D lands.

### F2. The agent-facing documents

`skills/stow-s3/SKILL.md` is the first thing a coding agent reads and is the
highest-leverage document in the repository. Its frontmatter trigger list is
good and its "Decide first" section correctly tells a reader when *not* to reach
for stow. Two changes beyond the A2 install flip:

- Add the two run-through facts a caller must know before starting a session: a
  read-through fetch needs the bucket to exist locally until B1 lands, and under
  `mirrorWrites` the upstream bucket is never created until B2 lands. State
  which are fixed by version.
- Add a "what this is not" line matching the README. The skill currently tells an
  agent when not to use stow but not that it is not a security boundary.

`site/agent.md` and `site/llms.txt` are the machine-readable pair and must state
the same facts in the same commit as `SKILL.md`, or the three diverge and the
gate cannot see it.

### F3. Extend the doc gate

The install-disclaimer rule is the only thing checking prose against reality.
Add two cheap checks in the same family:

- **Command and flag existence.** Extract the verb table from the source of
  truth — `workspaceVerbs` at `cmd/stow-s3/workspace.go:104-121` and the
  subcommand switch at `cmd/stow-s3/main.go:160-176` — and fail if a document
  names a command or flag the table does not contain. The workspace usage line is
  already derived from the table for exactly this reason; the documents are not.
- **URL reachability.** The check from A4, run in the release workflow where the
  network is already a dependency, as `check-install-surface --online` already is.

---

## 8. Ordering

```
A1 ──────────────────────────────────────────────► standalone

A2 ──┬─► A5 ship v0.3.0 ──► (release)            needs A3 for a true install surface
     └─► F1 README, F2 agent docs
A3 ──┘

A4 ──► standalone, but F2 must not publish a dead URL

B1, B2 ──┬─► C2 two-instance harness
          └─► re-verify B1, B2 as tests        C2 is the fix for their being findable
                                                  by hand at all

B3 ──────► product decision, then one of two implementations

C1 ─────────────────────────────────────────────► independent of B1–B3; covers a
                                                  single store, which is a
                                                  different problem

D1–D5 ─────────────────────────────────────────► parallel with all of the above
E1–E3 ─────────────────────────────────────────► parallel with all of the above
F3 ─────────────────────────────────────────────► after A2 and F1, so it has rules to check
```

The critical path is A2. It is one admin action, one commit, four documents, and
it is the difference between a repository and a package. A5 is second and it is
the one item that makes an existing subscriber's install correct rather than
merely documented. Phase D is five independent items that need no coordination
with either, with each other, or with the code work — which is the argument for
starting D first if one person is the constraint, because A2 will block on an
administrative step nobody can review.

## 9. What this plan does not claim

- No revenue model. A permissive licence with no price is a coherent choice and
  this plan does not argue against it.
- No traction estimate. The repository has 1 star, 0 forks, and 0 watchers. This
  plan is about whether a stranger who arrives can succeed, not about whether
  anyone arrives.
- No verdict on the run-through architecture. `docs/adr/0002` and
  `docs/adr/0005` settled the consent boundary and the code matches them; B1 and
  B2 are defects inside a settled design, not arguments against it.
- No claim that the conformance matrix will stay green. C1 is expected to surface
  failures the first time the workspace backend runs against the shared corpus —
  a store that has never been through it cannot be assumed to pass. That is the
  point of adding it, and the failures it surfaces are cheaper than the defects
  it would otherwise leave in the one surface an agent receives.
- No claim that C2 would have prevented B1 and B2 from shipping. It would not
  have; they shipped because no such harness existed. C2 is what stops the next
  one.
