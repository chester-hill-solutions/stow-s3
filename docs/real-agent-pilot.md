# Real-agent pilot evidence

**Date:** 2026-09-29. **Stow revision:** `f54927668a135146518f56efaf27eca2b7df1578`
("Build portable workspaces and experimental agent checkpoint integration"),
the current `origin/main` at run time. **Version:** `stow-s3 0.3.0`, unpublished,
built from source. No package was published and no runner or sandbox feature
was added.

**Result: PASS with one stage BLOCKED.** Real-model turns, checkpoint
publication, lost-reply recovery, fail-closed admission and a moved-bundle adopt
with a fresh agent all passed on one Linux host. The cross-host transfer stage
is **blocked**: no second host was reachable. That stage is not simulated below.

---

## 1. Environment and exact commands

| Item | Value |
| --- | --- |
| Host | Ubuntu 24.04.4 LTS, Linux 6.x, `x86_64` |
| Checkout | `/home/nathaniel-arfin/Documents/stow` (pre-existing, local change preserved) |
| Node.js / npm | `v24.14.0` / `11.11.0` |
| Go on `PATH` | `go1.22.5` — too old |
| Go actually used | `go1.25.6`, auto-selected by `GOTOOLCHAIN=go1.25.6` from the Makefile's `$(GO_TOOLCHAIN)`, read from `.go-version` |
| OpenCode | `/home/nathaniel-arfin/.opencode/bin/opencode`, reports exactly `opencode v2.0.16` |
| Model | `opencode` / `space-bunny-free` |

The host's own Go is 1.22.5. The pinned toolchain is not a system install: the
Makefile invokes `GOTOOLCHAIN=go1.25.6 go`, which downloaded and used 1.25.6.
Verified directly — `GOTOOLCHAIN=go1.25.6 go version` → `go1.22.5` is what
`go version` reports without it, `go1.25.6` with it. No toolchain was
installed system-wide.

```sh
cd /home/nathaniel-arfin/Documents/stow
git fetch origin main          # origin/main -> f549276, the supplied commit
git checkout f54927668a135146518f56efaf27eca2b7df1578
make build build-wasm          # OK
npm --prefix packages/stow-s3 ci --ignore-scripts --omit=optional   # 259 packages
npm --prefix packages/stow-s3 run build                            # tsc + wasm copy
./bin/stow-s3 --version         # stow-s3 0.3.0
make test-agent                # 19 pass, 0 fail
```

**Local change preservation.** The checkout had one uncommitted modification,
`packages/stow-s3/dist/stow-runtime.wasm` (a build artifact, 5286653 →
5286680 bytes). It was stashed before checkout and is still at
`stash@{0} On main: pilot-preserve-wasm-artifact`. The working tree is clean at
`f549276` apart from that preserved stash; nothing was discarded. Note for the
next run: the wasm artifact the pilot built is a *rebuild* of the same source
and does not need the stash restored.

**`make test-agent`:** 19/19 pass, including the native lost-reply and
moved-bundle continuity test, admission-before-execution, bounded retry/backoff,
file-only quiescence, and the unconfirmed-writer-stop retention cases.

**`npm ci --omit=optional` was load-bearing.** The README warns that packaged
native carriers can shadow the source-built binary. Confirmed relevant: the
build and all runs used `/home/nathaniel-arfin/Documents/stow/bin/stow-s3`
explicitly, and `caller.json` names that absolute path.

## 2. OpenCode version gate

`server.mjs` refuses to start unless `--version` prints exactly
`opencode v2.0.16`. The installed binary satisfies this, both in the normal
environment and under a fully isolated `env -i` HOME (checked separately before
the pilot, so the version check was never bypassed). No npm install of
OpenCode was attempted; the pinned executable was already present and is the
one used. The caller ran it 6 times in this pilot and the gate passed every
time.

## 3. Model access — an intervention that weakens the isolation claim

The caller deletes all `OPENCODE_*` variables and runs OpenCode with its own
`HOME`/`XDG_*` under `stateDir`, so this session's OpenCode Console login is
not inherited. No provider API key was available in the environment (the only
credential-shaped variable present was an unrelated email key).

Per the user's explicit decision, the existing OpenCode Console credential was
**copied into the isolated data directory** rather than issued fresh for the
pilot. Method, without printing any secret value: a read-only SQLite
`VACUUM INTO` snapshot of the live `opencode.db`, then a copy with every table
except `credential` emptied, so no session, message, project or event history
crossed over. The isolated instance was then verified to be authenticated
(`opencode auth list` non-empty under `env -i`, and a real model turn returned
`succeeded`) before any pilot turn ran.

**This is a deviation from the documented model and is recorded as a
limitation, not a pass.** The README states the example does not copy
credentials from another OpenCode installation. The credential itself stayed
out of every prompt, fixture, note, checkpoint, and this report, and each
isolated `HOME` was fresh — but the pilot therefore did not prove that a
*freshly issued* provider credential works on a fresh host. That specific claim
stays open.

## 4. Run: real agent turns

Workspace prepared from a disposable manifest outside the repo, with registry
and caller state in separate directories:

- workspace `ws_85933debebf001a8`
- root `/tmp/opencode/stow-pilot/workspace-parent/task-sales-review`
- registry `/tmp/opencode/stow-pilot/registry/teams/pilot`
- caller state `/tmp/opencode/stow-pilot/caller-state`
- task: 20-row `orders.csv` + a brief asking for revenue by region and channel,
  then an improvement pass.

| Turn | Caller result | Checkpoint | Wall clock |
| --- | --- | --- | --- |
| 1 | `phase=saved`, `terminal=succeeded` | `cp_5de507d2d16988534d6b97b6` | 1m36s |
| 2 | `phase=saved`, `terminal=succeeded` | `cp_733b8ceb8ee18922489a307a` | 2m47s |
| 3 (fault-injected, §5) | `phase=saved`, `terminal=succeeded` | `cp_aef80b5b18b5b4db50d7c3b8` | 42s |
| 5 (crash-after-commit, §5) | recovered, `phase=saved` | `cp_eb619a1cb5c9062933732a6c` | 27s to crash, 0.4s to recover |

Turn 1's checkpoint was confirmed published (`registry/.../checkpoints/cp_5de507d2d16988534d6b97b6/`
with `manifest.json`, `receipt.json` and payloads) before turn 2 was admitted.
Turn 2's request recorded `parent: cp_5de507d2d16988534d6b97b6`, so the chain is
linked rather than flat.

### Independent verification of the task result

A saved checkpoint is not a correct answer, so every figure was recomputed
outside the agent, in Python, from `orders.csv` alone.

Turn 1 — all confirmed: 20 data rows, 6 columns; total revenue **2,266.11**
over 92 units; by region AP-SOUTH 650.90, EU-WEST 577.13, NA-WEST 525.91,
NA-EAST 512.17; by channel web 833.00, partner 779.61, retail 653.50; largest
order 1003 at **239.88**, runner-up 1012 at 199.90. The agent's extra claims
checked out too: `unit_price` is a strict function of `channel` (web 24.50,
partner 19.99, retail 31.00/41.75), 19 distinct dates, region counts all 5.

Turn 2 — all confirmed: per-region average revenue per order 130.18 / 115.43 /
105.18 / 102.43; per-channel averages 194.90 / 93.36 / 92.56; median order
revenue 122.50; first-half 1,057.05 vs second-half 1,209.06; least-squares
slope **+0.738/day**, r 0.086, r² 0.0075, t 0.368 on 18 df (p ≈ 0.72); per-half
medians 85.75 and 124.62. The agent's judgement — that the half-over-half rise
is *not* a trend — is supported by its own numbers and by mine.

Turn 3 cross-tab confirmed exactly: NA-WEST, NA-EAST and EU-WEST each
2 web / 1 partner / 2 retail, AP-SOUTH 3/1/1, column totals 9/4/7 = 20. The
agent reported a three-way tie for most even rather than picking one, which is
the correct reading.

Turn 5: largest order 1003 at 239.88, correctly identified as a partner order.

`orders.csv` was byte-identical to the manifest source after every turn
(`293423bc…` throughout), so the agent did not quietly edit its input.

### Turn 1 content was preserved across the turn 2 overwrite

The brief asked turn 2 to keep turn 1 visible. A diff of the turn-1 region
between checkpoint 1 and checkpoint 2 shows two heading rewrites
(`## Status / progress` → `### Turn 1 closing status…`,
`## Remaining work (Turn 2…)` → `### Turn 1 remaining work…`) and the new
`## Turn 2` body appended. No number, table or conclusion in sections 1–7
changed. This is a small deviation from "unchanged", caused by the agent
clarifying stale headings; worth knowing, not a defect in Stow.

### Checkpoint contents

For every checkpoint: manifest `version: 2`, `working_directory: repo`, four
objects (`STOW_PROGRESS.json`, `repo/STOW_NOTES.md`, `repo/TASK.md`,
`repo/orders.csv`), and a `receipt.json` with `request_digest` and
`options_digest`. Recomputed SHA-256 of each stored payload matched the live
file in every case. File modes were preserved exactly (0600 for the progress
file, 0664 for the notes, 0644 for the seeded inputs). `.stow` private state
was excluded, as documented.

## 5. Recovery — controlled fault injection

Two distinct injections, both against a real model, a real `stow-s3` binary and
a real OpenCode process. No checkpoint was hand-written.

**(a) Lost reply after a durable commit.** A driver wrapped the real
`nativeStorage` so `capture` performed the real capture and then discarded the
reply. Observed: `captures=1`, `resolves=2`, `promptCalls=1`. The checkpoint
committed was `cp_aef80b5b18b5b4db50d7c3b8`, and that is the ID the caller
settled on — the original capture, not a later state. `promptCalls=1` is the
load-bearing number: the model was prompted once, and reconciliation resolved
the existing receipt instead of re-running the turn. Its stored notes contained
the turn-3 section and no turn-4 content.

**(b) Crash after commit, before reply handling.** A second driver ran the real
capture, wrote the checkpoint ID to evidence, then `SIGKILL`ed itself
(exit 137) before the reply could be handled, leaving `phase: saving` in
`turn.json`. The kill orphaned the OpenCode agent — `ppid 1`, still running
with the pilot's isolated `HOME`. Per the README, that process was identified
and stopped *before* recovery, and it was confirmed gone. The stale
`admission.lock` directory was removed only after confirming all three: the
owning PID dead, no pilot agent alive, no `stow-s3 workspace serve` alive.

`node examples/opencode/run.mjs caller2.json --recover` then completed in
**0.44 s**, with no model call and no OpenCode process started. It resolved
`cp_eb619a1cb5c9062933732a6c` — the exact checkpoint the crashed run had
committed — and did not create a new capture. The checkpoint count went 4 → 4.
Recovered payload hashes matched the live tree, and the notes contained the
turn-5 section.

**Fail-closed behaviour, both directions.**

- *Unresolved save blocks later prompts.* A third run injected a storage stub
  where capture always fails and resolve always reports `not_found`. The turn
  ran the model to `succeeded` and wrote turn 4 into the notes, then failed to
  save. No new checkpoint appeared, and `turn.json` stayed at `phase: saving`.
  A subsequent normal turn was refused:
  `Error: previous turn needs explicit recovery; admission is closed`.
  `--recover` on that same state refused with
  `Recovery did not find a confirmed save; explicit new capture decision required`
  and did **not** promote the unsaved turn-4 state. `turn.json` was left intact
  for diagnosis. This is the fail-closed property working: the agent's
  successful execution and the successful save are kept distinct, and no
  unsaved tree was silently accepted as the turn.

**Operational note, not a Stow defect.** The crash left the notes on disk ahead
of the last checkpoint. The checkpoint was correctly *behind* the live tree, and
recovery did not paper over that. Anyone reconciling a crashed run has to look
at the live tree, as the README says.

## 6. Transfer — BLOCKED, not simulated

**This stage did not run. No second host was reachable from this machine.**

Attempted, in order:

1. `ssh chs-imac` (LAN, 10.0.0.38) → `Permission denied (publickey,password)`.
   `~/.ssh` holds only `railway_ed25519`; there is no agent running
   (`ssh-add -l` → `Could not open a connection to your authentication agent`).
2. `ssh nja-imac-remote` (cloudflared tunnel to `ssh.nathanielarfin.com`) →
   cloudflared is installed and opened a browser authentication URL, which
   cannot complete unattended.

The user chose to record this as blocked rather than proceed. Consequently
**there is no evidence that the same Stow commit builds or runs on a second
physical host, and no evidence of continuation on a different machine.** The
README already states a fresh-host OpenCode install is unverified; that remains
unverified.

What *was* done is same-host and labelled as such:

- Exported an unedited bundle: `handoff.json` + `checkpoint.tar.gz`, 12,471
  bytes, archive referenced by the **relative** path `checkpoint.tar.gz` with
  SHA-256 `cdcbc230…`.
- Renamed the bundle directory after export, and confirmed the digest was
  unchanged — the relative reference survived the move.
- Inspected the archive: exactly `manifest.json` plus the four declared
  payloads. A grep across extracted contents for `/home/nathaniel-arfin`,
  `/tmp/opencode/stow-pilot`, `access_key`, `secret_access_key` and private-key
  headers found **nothing**. No sender path and no credential is in the bundle.
- Adopted into a **fresh registry** (`/tmp/opencode/stow-pilot/receiver/fresh-registry`)
  with a new workspace ID `ws_658c502b2521a0d5`. All four adopted files are
  byte-identical to the sender's checkpoint payloads. The sender's registry was
  untouched.
- **Round-tripped through Railway object storage** (see §6a) and adopted a
  second time from the downloaded bytes alone, into a third fresh registry as
  `ws_200e141b92d9bd46`. All four files again matched the sender's checkpoint
  payload hashes. The bundle therefore survives a store-and-forward hop, not
  just a filesystem rename.
- Started a **fresh agent with no prior chat** (`sessionID
  ses_f11bba4d8ffeuBA3d22P0u7P3J`, distinct from the sender's sessions) against
  the adopted workspace and its own fresh isolated state directory. It read
  `STOW_NOTES.md` and `STOW_PROGRESS.json`, recomputed everything from
  `orders.csv` (as instructed), and appended a `## Continuation` section that
  closes the task. It saved `cp_94422236bf91617b6113181a` into the fresh
  registry.
- The receiver's copy of turns 1–5 is byte-identical to the sender's
  checkpoint apart from a trailing `---` separator before `## Continuation`.
  `orders.csv` is unchanged in the manifest, the sender workspace and the
  receiver workspace.
- The receiver ran with its own provider credential in its own isolated `HOME`
  and needed no sender filesystem path and no sender storage credential.

This demonstrates the *bundle* is portable and self-contained, and that a
checkpoint can carry a task across a registry boundary to a fresh agent. It
does not demonstrate the cross-host claim, which needs a second machine.

## 6a. Store-and-forward through Railway object storage

Because direct host-to-host access was unavailable, the bundle was also parked
in an S3-compatible bucket so a second machine can fetch it without any network
path to this one. This adds a transport hop to the evidence; it does **not**
substitute for the cross-host stage.

- Bucket: Railway project `stow-conformance-scratch`, bucket `stow-conformance`
  (backend bucket `stow-conformance-kvun-ytc`, endpoint host `t3.storageapi.dev`,
  region `auto`, virtual-host addressing). The bucket was empty before this
  (`objects: 0, storage: 0 B`) and is dedicated to Stow conformance work.
- Keys: `stow-pilot/f549276/handoff.json` and
  `stow-pilot/f549276/checkpoint.tar.gz`.
- Credentials came from `railway bucket credentials`, written to a `0600` file
  outside the repo. No access key or secret was printed, and neither is in this
  report.
- **Verified by round trip, not by the upload's own success message.** Downloaded
  both keys back and compared SHA-256: `checkpoint.tar.gz` = `cdcbc230…` remote
  and local, `handoff.json` = `27548222…` remote and local. `ListObjectsV2`
  returns 2 keys and `HeadObject` confirms 12,471 and 347 bytes.
- Adopted from the **downloaded** bytes into a third fresh registry
  (`ws_200e141b92d9bd46`): all four files matched the sender's checkpoint
  payload hashes. The relative `checkpoint.tar.gz` reference in `handoff.json`
  resolved correctly with only the two downloaded files present.
- Note: `railway bucket info` continued to report `objects: 0, storage: 0 B`
  after both uploads. That counter is wrong or lagging on Railway's side; the
  objects are demonstrably present per the SDK operations above. Trust the
  round trip, not the dashboard number.

**Security note.** These objects are readable by anyone holding the bucket
credentials, and the bundle contains the full task record — five turns of
analysis. It contains no credentials and no sender paths (verified by grep in
§6), so the exposure is task content, not secrets. Rotate or delete these keys
when the pilot is finished.

## 7. Timing

Model and network time dominates; capture itself is negligible at this size.

| Measurement | Samples |
| --- | --- |
| Turn 1 (model + save) | 96 s |
| Turn 2 (model + save) | 167 s |
| Turn 3, fault-injected lost reply (model + capture + resolve) | 42 s |
| Turn 5 to crash point | 27 s |
| `--recover` on a durable receipt, no model | 0.44 s |
| Bare portable capture, 4 files, CLI (3 runs) | 0.02 / 0.01 / 0.01 s |
| Bundle export → archive | 12,471 bytes |

The three 10–20 ms captures are consistent with the implementation report's
figure for small file counts and are not a measurement of a realistic workload.

## 8. Interventions and deviations

1. **Console credential copied into the isolated `HOME`** rather than freshly
   issued (§3). Weakens the "fresh host, fresh credential" claim. No secret
   value was printed, and no session history crossed over.
2. **Model `opencode/space-bunny-free`.** The only access available. A frontier
   model was not used, so this says nothing about behaviour under a different
   model.
3. **Two extra prompts beyond the two required turns** (turn 3 for the
   cross-tab, turn 5 for the crash case). Turn 4 was a deliberately failing
   save used for the fail-closed test: no checkpoint was created *for that
   turn*, and `turn.json` was left at `phase: saving`. Its content was
   nevertheless picked up by the next successful capture, because turn 5 ran
   against the same live tree and captured whatever was there. Verified by
   section: `cp_eb619a1cb5c9062933732a6c` contains turns 1–5, so turn 4's text
   is durable even though turn 4's own save failed. That is capture semantics,
   not a save of the failed turn.
4. **Turn 2 renamed two turn-1 headings** while keeping every figure (§4).
5. **A custom driver was written for fault injection** (`fault-turn.mjs`,
   `crash-turn.mjs`, outside the repo, in `/tmp/opencode/stow-pilot/`). It
   reuses the repository's `TurnController`, `OpenCodeAgent`, `startOpenCode`,
   `nativeStorage` and `shutdownPilot` unchanged and only wraps the storage
   seam. `run.mjs` itself was not modified and was used unmodified for turns 1–2
   and for both recoveries. The crash driver does skip `shutdownPilot`, which
   is the point — it models a `SIGKILL`.
6. **Host Go is 1.22.5**; 1.25.6 was used via the Makefile's `GOTOOLCHAIN`.
   Anyone reproducing this should expect the toolchain download.
7. **Three benchmark captures** were taken after the pilot for timing and are
   visible in the sender registry as `cp_8a8e3f28…`, `cp_941e8892…`,
   `cp_f042569…`. They are timing artifacts, not part of the evidence chain.

## 9. Bugs found

**No implementation defect was reproduced.** Two recovery paths, one
fail-closed path, a blocked-admission path, five real model turns, and three
independent adopts (same-host, fresh-registry, and from bytes round-tripped
through object storage) all behaved as documented. No regression test or fix was
needed, and no scope check, durability barrier or fail-closed guarantee was
weakened.

**One correction to an earlier claim in this report.** A first draft stated that
turn 4's content existed "in the live tree but in no checkpoint". That was wrong.
Re-checking each checkpoint's stored payload shows
`cp_eb619a1cb5c9062933732a6c` contains `## Turn 1` through `## Turn 5`: the next
successful capture picked up turn 4's text, because it captured the live tree
rather than a per-turn delta. The correction does not change any verdict — the
fail-closed behaviour is still exactly as described, since turn 4's *own* save
created nothing and left admission closed. But the durability claim was
materially understated and is now fixed in §8.3.

One friction point worth recording, though it is a documented behaviour rather
than a bug: after `SIGKILL`, `run.mjs --recover` fails with
`EEXIST … admission.lock` until the stale lock is removed by hand. The README
does say to remove a stale `admission.lock` only after confirming the owner and
OpenCode process stopped, and that is exactly the sequence followed. The error
message names neither the cause nor the remedy, so an operator who skipped the
README would be stuck. Improving that message is a candidate, not a defect
found by this pilot.

## 10. Remaining blockers

| # | Blocker | Owner |
| --- | --- | --- |
| 1 | **No reachable second host**, so cross-host transfer, cross-host build and cross-host continuation are unverified. Needs working SSH to a Mac, and an OpenCode 2.0.16 build for that host's platform. | user |
| 2 | **A freshly issued provider credential for an isolated caller is unverified**, because the Console credential was copied in instead. | needs a key issued for the pilot |
| 3 | **A non-trivial model is unverified.** All evidence is from `space-bunny-free`. | user |
| 4 | **OpenCode 2.0.16 cannot be installed from npm** — `opencode-ai@2.0.16` returned E404 per the README, and this pilot did not attempt it. A second host needs its platform binary from a verified source or a source build. | release work |
| 5 | `stow-s3 0.3.0` remains **unpublished**, so every consumer must build from source. The five `@chester-hill-solutions/stow-s3*` packages still need to be made public before anyone can install this. | user (A2 in the 2026-09-28 handoff) |
| 6 | Live-provider tests remain skipped; this pilot used no upstream S3 credentials, only the workspace backend. | needs R2 keys as repo secrets |
| 7 | Force-killing the controller leaves an orphaned agent and a stale `admission.lock`, both requiring manual host cleanup. Documented, not fixed. | accepted for the experimental example |
| 8 | The bundle now sits in the shared Railway bucket `stow-conformance` under `stow-pilot/f549276/`. Anyone with those credentials can read the full task record. Delete the keys or rotate the credentials when the pilot closes. | user |

## 11. Verdict and next action

**PASS on one Linux host for the RUN and RECOVER stages, with independent
verification of every task figure. BLOCKED on TRANSFER — no second host, and
the stage was not simulated. The experimental OpenCode example has now
demonstrated a successful real-model turn, which was its main outstanding
local gate.**

**Next action: restore access to a second host and repeat §6 there.** The
bundle is already parked in object storage (§6a), so the receiving side no
longer needs a network path back to this machine — only outbound HTTPS to the
bucket. Concretely: install an OpenCode 2.0.16 build for that host's platform,
build `f549276` from source, download the two keys from
`stow-pilot/f549276/`, verify the `cdcbc230…` digest, adopt into a fresh
registry, and run a fresh agent with a credential issued for that host. That
single run closes blockers 1, 2 and 3 together. It is the last substantive item
before the caller example can be described as validated rather than
experimental.

### Sync-down and restore procedure

Requires: Go 1.25.6, Node 24 with npm, an OpenCode 2.0.16 executable for the
host, and read access to the bucket. R2 credentials were not present in this
environment, so the pilot used Railway's S3-compatible bucket; any S3 client
works, the commands are identical.

```sh
# 1. Build the same commit (not main, and not an npm release).
git clone https://github.com/chester-hill-solutions/stow-s3.git stow && cd stow
git fetch origin main && git checkout f54927668a135146518f56efaf27eca2b7df1578
make build build-wasm
npm --prefix packages/stow-s3 ci --ignore-scripts --omit=optional
npm --prefix packages/stow-s3 run build
./bin/stow-s3 --version          # must print stow-s3 0.3.0
```

```sh
# 2. Fetch the bundle. Both files must land in the SAME directory, because
#    handoff.json references the archive by the relative path "checkpoint.tar.gz".
mkdir -p ~/stow-handoff && cd ~/stow-handoff
aws s3 cp "s3://stow-conformance/stow-pilot/f549276/handoff.json" . \
  --endpoint-url "https://t3.storageapi.dev"
aws s3 cp "s3://stow-conformance/stow-pilot/f549276/checkpoint.tar.gz" . \
  --endpoint-url "https://t3.storageapi.dev"
# Railway's CLI can do it without the AWS CLI:
#   railway bucket list --bucket stow-conformance

# 3. Verify before trusting. The digest is also recorded inside handoff.json,
#    so a mismatch means truncation, not a bad reference.
sha256sum checkpoint.tar.gz
# expect cdcbc23063db6af12a59696dc88bbde0a10f86e9dd3e30401ded0fdab6717dd5
python3 -c "import json;print(json.load(open('handoff.json'))['archive']['sha256'])"
```

```sh
# 4. Adopt into a fresh registry. The parent of --root must exist; --root
#    must NOT. Do not reuse the sender's registry directory.
mkdir -p "$HOME/stow-received"
./bin/stow-s3 workspace adopt \
  --handoff "$HOME/stow-handoff/handoff.json" \
  --root "$HOME/stow-received" \
  --registry-dir "$HOME/stow-registry"
```

Expect a new `workspace_id` and `checkpoint_id: cp_eb619a1cb5c9062933732a6c`,
with four seeded objects. The restored tree is
`~/stow-received/repo/{STOW_NOTES.md,TASK.md,orders.csv}` plus
`~/stow-received/STOW_PROGRESS.json`.

```sh
# 5. Start a fresh agent. Needs a provider credential in ITS OWN environment;
#    the sender's credential does not travel in the bundle.
$EDITOR caller.json    # workspaceID, registryDir, stateDir, stowBinary,
                       # opencodeBinary, model.providerID, model.id
node examples/opencode/run.mjs caller.json prompt.txt
```

The prompt should tell the agent to read `STOW_NOTES.md` and
`STOW_PROGRESS.json` first and treat the notes as the starting point. No sender
filesystem path and no sender storage credential is required, and none is
present in the bundle — both were verified absent by grep in §6.
