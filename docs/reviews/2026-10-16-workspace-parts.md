# Atomic workspace multipart part publication

Follow-up: [Railway performance requalification and directory-path reuse](2026-10-17-part-listing-performance.md)
records all new measurements, including adverse timings. Changed-version durable
fallback is now measured on Railway and remains the next separate change.

This corrects **A2 only** from the [bounded storage audit](2026-10-16-storage-contract-audit.md).
Baseline is `91e8ba0501aaab5c3d221f6dde9f3bf52786425d` on
`codex/core-copy-quality`. Freshly fetched `origin/main` is
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of that baseline.
[Baseline](2026-10-16-workspace-parts/raw/baseline.json).
The user explicitly removed backward compatibility as a requirement.

## Original defect, reproduced before implementation

Workspace UploadPart first replaced a raw part file, then saved its descriptor
in `upload.json`. A failure in the second publication left retained bytes and
persisted size/ETag describing different versions. A committed-error marker alone
could not repair that persistent disagreement.

The exact [saved native audit probe](2026-10-16-storage-contract-audit/raw/workspace-probes.go.txt)
was run twice normally and twice under race before implementation:
[normal](2026-10-16-workspace-parts/raw/original-before.log),
[race](2026-10-16-workspace-parts/raw/original-before-race.log).
A reader callback obstructs the owner document after UploadPart has read it;
part publication succeeds and descriptor publication fails. Restoring the owner
document isolates the split without inventing a custom provider return value.
This is deterministic filesystem failure injection, not evidence of a production
incident or a physical crash test.

Permanent tests then reproduced the consequences after close/reopen for first
publication, larger replacement and same-size replacement, each with a healthy
control. A separate runtime test demonstrates the quota effect: the retained
11-byte replacement is reported as the old 3-byte part, admitting a 2-byte PUT
under a 12-byte cap. Abort must still release the actual retained reservation.

Before implementation, each twice-repeated run has **8 failing adverse leaves
and 6 passing healthy controls**, normally and under race:
[normal](2026-10-16-workspace-parts/raw/before-tests.log),
[race](2026-10-16-workspace-parts/raw/before-race.log).

## Smallest selected correction

Each staged part now contains its descriptor and payload in **one atomic file**.
The upload owner document holds initiation identity/options and is not rewritten
for every part. ListParts takes the existing store read lock, reads the owner
once and reads each retained part header. Completion reads payloads from those
same files and keeps its existing content/ETag checks. There is no runtime
accounting change, index, journal, checkpoint or new abstraction.

The private fixed header is 47 bytes: a version magic, part number, payload
length, seconds/nanoseconds timestamp and the existing MD5 digest. Header reads
validate identity, timestamp and physical payload length before allocating a
payload buffer. Header and payload use the existing `atomicfile.WriteParts`
interface; the payload is not copied into an additional combined record buffer.
The variable JSON descriptor list and its upsert helper are removed.

The [storage contract](../compat-contract.md) and [workspace contract](../workspace-contract.md)
were updated before production implementation. No public API, dependency or
completed-object format changes. Old raw staged parts are refused by listing
and completion without migration; runtime reopen that encounters them fails.
Abort remains available. Finish or abort old uploads before changing builds.

The retained production functions are byte-identical to the measured fixed
prototype. [Source freeze](2026-10-16-workspace-parts/raw/source-freeze.json)
and [Railway build identities](2026-10-16-workspace-parts/raw/railway-2806c27c-3315-4f0d-84ee-deb0c2a7142a/environment.json).
The only other production edit corrects an outdated atomic-writer comment.

## Comparative measurements — Railway only

**No benchmark ran locally.** Local work compiled/tested correctness; Railway
built and ran the comparison binaries using Go 1.25.6, linux/amd64, on an AMD EPYC
9655P host under a 4-CPU/4-GiB container limit. Go CPU settings were 1 and 4;
operations within each benchmark are sequential, not a concurrent-client load.
There are four samples per workload/variant/CPU, 200 ms minimum benchmark time,
with variant order reversed between two cycles. This is a bounded comparative
measurement, not a throughput, peak-memory or durability qualification.

Three complete cohorts each retain 144 samples, raw block logs, source/binary
hashes, compiler settings, mount information and resource snapshots:

- [Initial overlay-filesystem run](2026-10-16-workspace-parts/raw/railway-f3258e07-986e-4026-8861-63c2089b6b56/run.json): baseline, streaming scan/hash, JSON part record.
- [Persistent ext4 run](2026-10-16-workspace-parts/raw/railway-15faaa38-2176-4a9d-ae75-6ec943ffcf04/run.json): the same binaries on the volume.
- [Final persistent ext4 comparison](2026-10-16-workspace-parts/raw/railway-2806c27c-3315-4f0d-84ee-deb0c2a7142a/run.json): baseline, JSON record, fixed header.

[Every median, minimum, maximum, B/op and allocations/op](2026-10-16-workspace-parts/raw/comparison.md)
is retained, including adverse rows. The workloads are one 1-KiB staged part and
sixteen 1-MiB staged parts, measuring native listing/replacement and runtime
replacement. The latter is valid acknowledged staging, not a completable set of
non-final S3 parts: completion still requires the existing 5-MiB minimum.

Deriving descriptors by streaming/hashing every raw part avoids a new format but
makes the 16-MiB listing roughly **340–345× slower on ext4** (18.7–19.9 ms versus
0.055–0.058 ms), with about 581–582 KB allocated instead of 16 KB. Runtime
replacement is roughly **1.9–2.1× slower on ext4** and **4.6–4.7× on overlay**.
That approach was rejected before production implementation.

The JSON-record prototype avoids payload scans, but large listings allocate 346
times per operation versus the baseline's 115. The first ext4 run also has a
**12.8% slower CPU-1 runtime replacement** and a **5.8% slower CPU-4 native
replacement**. Those regressions are preserved; the later cohort does not erase
them. The fixed header was the one additional comparison, reducing decoding and
allocation costs while keeping atomic publication.

Final ext4 medians, baseline → retained fixed header:

| Workload | CPU 1, ms/op | CPU 4, ms/op | Allocations/op, CPU 1 |
|---|---:|---:|---:|
| List one 1-KiB part | 0.0189 → 0.0158 | 0.0183 → 0.0167 | 47 → 44 |
| Replace that part | 7.6609 → 4.2426 | 8.8012 → 3.4838 | 70 → 44 |
| Runtime replace that part | 8.1198 → 4.7202 | 8.0065 → 3.6327 | 237 → 204 |
| List sixteen 1-MiB parts | 0.0570 → 0.0908 | 0.0595 → 0.0889 | 115 → 234 |
| Replace one 1-MiB part | 15.9774 → 15.3020 | 15.5583 → 15.4733 | 122 → 45 |
| Runtime replace that part | 16.7735 → 16.1775 | 17.3239 → 16.0205 | 444 → 429 |

**Retained cost:** large listing is **49–59% slower**, about **29–34 microseconds**
per operation, with **119 additional allocations** and about **6.6 KB more
allocated per call** (16,192 → 22,804/22,829 B/op). It now opens one header per
part rather than decoding a single owner document. Small native replacements
save one durable publication and about 39% of allocated bytes (9,688 → 5,896 B/op).
Large runtime replacement allocation volume falls slightly, about 6.37 MB →
6.34 MB; this does not eliminate existing whole-part buffering.

Do not infer a significant large-upload speedup from these few samples: ranges
overlap. The fixed prototype is also slower than the JSON prototype in the final
runtime-replacement medians (about 3–25%); it was selected for simpler bounded
header parsing and lower allocations, not as the fastest result in every row.
The identical baseline's small ext4 replacement median varies from 6.24/6.54 ms
in the first cohort to 7.66/8.80 ms in the final cohort. Shared-host/storage
variation matters; comparisons are within cohort, and all ranges remain visible.
Allocated bytes measure Go allocations, not peak RSS or a process memory cap.

Both comparison deployments reached SUCCESS; the later validation deployment
also reached SUCCESS. Export receipts verify each result file's
length and SHA-256. The scratch runner was stopped after export verification;
the target remained stopped and untouched. Only the two empty task-owned volume
temporary directories were removed; result directories remain preserved.
[Deployment evidence](2026-10-16-workspace-parts/raw/railway-fixed-deployments.json),
[cleanup](2026-10-16-workspace-parts/raw/railway-temp-cleanup.log),
[runner stopped](2026-10-16-workspace-parts/raw/railway-runner-after.json),
[target unchanged](2026-10-16-workspace-parts/raw/railway-target-after.json).

## Regression and contract validation

The original first/larger/same-size publication cases now list the replacement's
size/ETag after reopen and complete to the exact replacement bytes with initiation
metadata preserved. Runtime refuses the 2-byte write under the 12-byte cap,
accepts the exact remaining byte, and releases staged bytes on abort.

Additional tests cover descriptor/payload roundtrip, timestamps before the epoch,
part-number boundaries, malformed/truncated headers, physical length mismatch,
invalid timestamps and huge/negative declared sizes refused before payload
allocation. Failed opens return no reader. Completion still rejects changed
payload bytes and preserves the upload without publishing a corrupt object.

All **38 focused regression leaf executions pass normally and under race**
(two repetitions in each mode), with
no skips or race reports:
[normal](2026-10-16-workspace-parts/raw/after-tests.log),
[race](2026-10-16-workspace-parts/raw/after-race.log),
[counts](2026-10-16-workspace-parts/raw/regression-counts.json).
The old audit probe's raw-file-layout/error assertions are not reused against the
new private encoding; permanent API/reopen/quota regressions replace them.

Broad local Go/race/conformance runs exhausted available disk space and failed;
they are **not passing validation**. A sequential retry was stopped when the user
clarified that all execution belongs on Railway. Failed/interrupted evidence is
retained: [Go](2026-10-16-workspace-parts/raw/local-full-go.disk-full.log),
[race](2026-10-16-workspace-parts/raw/local-broad-race.disk-full.log),
[conformance](2026-10-16-workspace-parts/raw/local-conformance.disk-full.log),
[stopped retry](2026-10-16-workspace-parts/raw/local-validation-stopped.json).
All further tests, race/conformance checks and benchmarks must run on Railway.
After explicit upload approval, final validation ran on Railway deployment
`1c7b7aba-7fdb-4b89-ac5b-a53a96d68c2f`, job
`0caf6ebb-cf77-4236-9f3d-914b614a8869`. **All 13 checks pass:**
[check commands and exit codes](2026-10-16-workspace-parts/raw/railway-validation-final/checks.json),
[completed result](2026-10-16-workspace-parts/raw/railway-validation-final/run.json).

- [Full Go suite](2026-10-16-workspace-parts/raw/railway-validation-final/full-go.log).
- [Broad storage/runtime/embedded/run-through/S3/public API race suite](2026-10-16-workspace-parts/raw/railway-validation-final/broad-race.log).
- [Focused normal](2026-10-16-workspace-parts/raw/railway-validation-final/focused-normal.log) and
  [focused race](2026-10-16-workspace-parts/raw/railway-validation-final/focused-race.log):
  38 passing leaf executions in each, with no skips or failures.
  [Counts](2026-10-16-workspace-parts/raw/railway-regression-counts.json).
- Conformance: [memory](2026-10-16-workspace-parts/raw/railway-validation-final/conformance-memory.log),
  [filesystem](2026-10-16-workspace-parts/raw/railway-validation-final/conformance-filesystem.log),
  [runtime-memory](2026-10-16-workspace-parts/raw/railway-validation-final/conformance-runtime-memory.log),
  [runtime-filesystem](2026-10-16-workspace-parts/raw/railway-validation-final/conformance-runtime-filesystem.log).
  Each retains the two existing mirror-bucket decision and live-upstream skips.
- [Go vet](2026-10-16-workspace-parts/raw/railway-validation-final/vet.log),
  [quality/file-size/format gates](2026-10-16-workspace-parts/raw/railway-validation-final/quality.log),
  [site build](2026-10-16-workspace-parts/raw/railway-validation-final/site-build.log),
  [site tests](2026-10-16-workspace-parts/raw/railway-validation-final/site-tests.log),
  [documentation links](2026-10-16-workspace-parts/raw/railway-validation-final/doc-links.log).

The first Railway run passed the new regressions and conformance but failed
broader tests because the container ran as root, did not resolve the test's named
loopback hosts, and left the parentwatch child as a zombie under PID 1. Those
[failed results](2026-10-16-workspace-parts/raw/railway-validation-initial/checks.json)
and [environment evidence](2026-10-16-workspace-parts/raw/railway-validation-environment-inspection.log)
remain retained. The corrected run uses UID 10001, two explicit loopback host
entries, and an existing-Python supervisor that reaps orphaned children without
terminating them. No tests or production sources were changed to obtain a pass.
[Runner configuration](2026-10-16-workspace-parts/raw/railway-validation-configured.log).
The supervisor is validation tooling only; it adds no product dependency.

The archive excludes local credentials, `.git` and ignored working files. The
quality gates receive the exact original parent baseline JSON files in a small
Git fixture, with `STOW_BASELINE_REF=HEAD`; this is not the product's Git history.
The [validation environment and source manifest](2026-10-16-workspace-parts/raw/railway-validation-final/environment.json)
identify the actual source snapshot. Final documentation links and production/test
source identities were checked again on Railway after updating this report.


## Limits and next priority — left unchanged

The part header adds 47 physical bytes per staged part, excluded from logical
payload quota just as previous descriptor-document overhead was. Existing
in-memory part/completion buffering remains. There is no new physical crash
simulation, cross-process writer coordination or bit-rot guarantee. Atomic
writer errors, including uncertain post-rename directory-sync failures, retain
existing semantics; listing still sees a consistent descriptor/payload pair.

The next highest-priority finding is **the same A1 recovery-error class in the
workspace owner-document enumeration**. A damaged `upload.json` is silently
skipped by `scanUploads`, independently of this part-publication split. A native
close/reopen probe confirms an acknowledged 11-byte part is omitted and a new
12-byte object is admitted under a 12-byte cap; repairing the owner document
reveals the retained part again. The healthy control refuses that write. The
probe fails twice normally and twice under race. This does not claim Stow's
atomic writer normally corrupts JSON, and this change does not repair it.

[Reproduction source](2026-10-16-workspace-parts/raw/next_recovery_probe.go.txt),
[normal evidence](2026-10-16-workspace-parts/raw/next-recovery.log),
[race evidence](2026-10-16-workspace-parts/raw/next-recovery-race.log).
It needs the same fail-closed treatment already applied to filesystem owners;
no new format or performance project is indicated. A3's native committed-outcome
sites remain queued. No next finding is implemented in this change.
