# Bounded correctness closure register — 2026-10-18

**One demonstrated fix remains: one low.** Of nine named historical leads, seven
are closed by existing implementations and fresh regressions; one is optional
input strictness with no demonstrated security consequence.
There are no unclassified leads in this bounded register. This does not certify
all code or every storage fault. [Machine-readable register](2026-10-18-correctness-closure/register.json).

C04 was closed on 2026-10-19 by the
[workspace uncertainty fix](2026-10-19-workspace-uncertainty.md), C06 by the
[repeated signed header fix](2026-10-19-sigv4-repeated-header.md), and C09 by
the [opaque entity tag fix](2026-10-19-opaque-etag.md). The last disposition
below stands unchanged.

The earlier statement that the *reproduced* core queue was closed described the
completed A1–A3/capacity work. It did not resolve the explicitly unverified older
leads. This closure pass reproduces four of those, including a native overwrite;
correctness work was therefore not finished at the time of that pass. Their scope
is now finite rather than another sequence of speculative “next bugs.”

Baseline is clean `b6d37430d731e03da3209f38b9737a99053765fe`. Fetching `origin/main`
again returns `219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Baseline](2026-10-18-correctness-closure/raw/baseline.json),
[frozen lead list](2026-10-18-correctness-closure/raw/lead-register-input.json).
No implementation, dependency, public API, storage format or quality baseline
changes. Only this report/register, historical-document pointers and evidence
are committed. Temporary red probes are retained as text and removed from the Go
test tree. No performance benchmarks run.

## Finite disposition

| ID | Historical lead | Current disposition |
| --- | --- | --- |
| C01 | Policy read ignores the current environment | Closed: read-side authority validation and its regression already exist. |
| C02 | Persisted recovery-hold expiry never takes effect | Closed for registry holds: expired holds stop blocking; live holds block. |
| C03 | Effective write-policy reporting ignores the resolved grant | Closed: reporting uses effective authority. |
| C04 | Workspace I/O errors are interpreted as absence | **Closed 2026-10-19:** only a confirmed absence is reported as absence, so a create-only write is refused over bytes the store could not read. |
| C05 | Post-rename uncertainty withdraws namespace reservation | Closed in the corrected managed writer: native fence/unknown regressions pass. |
| C06 | SigV4 only signs/verifies the first repeated header value | **Closed 2026-10-19:** a signed header contributes every value it carries, joined as the SDK joins them, and a changed set is refused. |
| C07 | SigV4 query sorting occurs before URI encoding | **Open, low:** a valid pinned Node signer request is refused for an encoded-name query. |
| C08 | Case-insensitive unsigned-payload marker | Optional strictness: accepted with a valid HMAC; no additional access demonstrated. |
| C09 | Opaque ETag preconditions compare case-insensitively | **Closed 2026-10-19:** an entity tag is opaque and compares character for character, in one shared rule for writes, copies and reads. |

## Required fixes, in priority order

### 1. C04 — Existing workspace bytes can be overwritten by a create-only request

**Closed 2026-10-19.** The
[workspace uncertainty fix](2026-10-19-workspace-uncertainty.md) reports only a
confirmed absence as an absent object, so a create-only write over a path the
store could not read is refused rather than admitted. The reproduction below
still describes the defect as it stood at this baseline.

The native workspace stores `victim=old`. With the file mode changed to 000 by
its owner, both HEAD and GET return `ErrObjectNotFound`. A subsequent native
`PutObject(..., IfNoneMatch: "*")` returns success and changes the file to `new`.
After permissions are restored, the original bytes are gone. A healthy control
returns `ErrPreconditionFailed` and preserves `old`.

This runs against the real workspace backend on the Railway results volume as
UID 10001, with enforced Linux permissions. It uses no custom store, mocked
absence error, callback or production hook. Two normal and two race executions
of the adverse leaf reproduce the overwrite; all four healthy controls pass.
It is a deterministic injected-permission failure, not a claimed production incident.
[Normal](2026-10-18-correctness-closure/raw/initial/workspace-normal.log),
[race](2026-10-18-correctness-closure/raw/initial/workspace-race.log),
[probe](2026-10-18-correctness-closure/raw/initial/workspace_probe.formatted.go.txt).

The trace is existing error-to-absence conversion in
[workspace rooted reads](../../internal/storage/workspace/rooted_reads.go#L40),
[path resolution](../../internal/storage/workspace/resolve.go#L22), and
[GetObject](../../internal/storage/workspace/objects.go#L121). `putLocked` accepts
the missing result from `peekLocked` as an absent destination, so the existing
precondition check admits the replacement. The smallest correction must preserve
I/O uncertainty through these read/lookup boundaries and prevent overwrite on
uncertain absence, while retaining true-missing and symlink-confinement behavior.
No index or storage abstraction is needed. The register states the acceptance
criteria for this one independently reviewable fix.

### 2. C06 — Later values of a signed HTTP header are not authenticated

**Closed 2026-10-19.** The
[repeated signed header fix](2026-10-19-sigv4-repeated-header.md) signs the whole
value set a header carries, as the SDK does. The reproduction below still
describes the defect as it stood at this baseline.

A request signed by the already-pinned AWS Go v2 signer with one metadata-header
value authenticates. The same signer with two values produces a valid signature
that Stow refuses. Conversely, sign the first value, then append a second value
without resigning: Stow still authenticates the modified request.

[headerValue](../../internal/auth/sigv4.go#L249) returns only `values[0]`, which
[canonicalHeaders](../../internal/auth/sigv4_canonical.go#L80) then canonicalizes.
The signature covers the complete multi-value header in the SDK, not just the
first element. The AWS rules require the values to be joined after normalization.
[AWS signing reference](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html).

The differential uses the repository's existing Go SDK v1.47.2 dependency and
public `Verifier.AuthenticateAt`, fixed signing time and test credentials.
Every single-value control passes; the repeated-value and appended-value cases
fail their required-contract assertions twice normally and twice under race.
This establishes a signature-binding and interoperability defect. It does not
establish that an appended metadata value changes a stored object or grants an
attacker additional authority through the built-in consumer.
[Normal](2026-10-18-correctness-closure/raw/initial/auth-normal.log),
[race](2026-10-18-correctness-closure/raw/initial/auth-race.log),
[SDK probe](2026-10-18-correctness-closure/raw/initial/auth_sdk_probe.formatted.go.txt),
[actual Go signer source](2026-10-18-correctness-closure/raw/initial/sdk-v4.go.txt).

### 3. C09 — Case-changed opaque ETags produce incorrect conditional outcomes

**Closed 2026-10-19.** The
[opaque entity tag fix](2026-10-19-opaque-etag.md) compares the tag character for
character, in one shared rule for the write, copy-source and read paths. The
reproduction below still describes the defect as it stood at this baseline.

The native S3 route over a memory store reports the tag
`"149603e6c03516362a8da23f624db945"`. Supplying its uppercase spelling produces:

| Request | Required result for the different opaque tag | Observed |
| --- | --- | --- |
| PUT with If-Match | 412, preserve destination | 200, replacement admitted |
| PUT with If-None-Match | 200 | 412 |
| COPY with source If-Match | 412 | 200 |
| COPY with source If-None-Match | 200 | 412 |

Exact-tag controls return their correct statuses. The defect is in the shared
[write/COPY matcher](../../internal/storage/preconditions.go#L78); the
[HTTP read validator helper](../../internal/s3api/validators.go#L166) has the same
case-folding pattern, but the dynamic matrix here qualifies PUT/COPY only.
Multipart's separate `storage.ETagEqual` is already case-sensitive and must not
be mislabeled as the source. An entity tag is opaque and compares character for
character, even when its current implementation resembles a hexadecimal digest.
[RFC 9110 comparison rules](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.3.2).

The corrected matrix has eight adverse leaf executions and eight healthy
controls in each mode (four routes, two repetitions), normally and under race.
[Normal](2026-10-18-correctness-closure/raw/final/s3api-normal.log),
[race](2026-10-18-correctness-closure/raw/final/s3api-race.log),
[corrected probe](2026-10-18-correctness-closure/raw/final/etag_probe.formatted.go.txt).
Existing exact, wildcard and weak-validator behavior needs regression coverage
when the comparison is corrected. No ETag-generation or format change is needed.

### 4. C07 — Encoded query names disagree with the pinned Node signer

For `z=last&%C3%A9=first`, Stow canonicalizes `z` before the encoded name. The
correct encoded ordering is `%C3%A9=first&z=last`. An independent literal canonical
request/HMAC fixture and the actual pinned Node `@smithy/signature-v4` 5.7.4 signer
both produce signatures Stow refuses. Their ASCII controls authenticate.
The source sorts decoded keys in
[canonicalQueryString](../../internal/auth/sigv4_canonical.go#L43), then encodes.
The signing rules put encoding before ordering.
[AWS canonical-request rules](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html).

This is a low-priority compatibility defect for unusual query-name inputs;
it is not evidence of an ordinary ASCII S3 SDK request failing or an authentication
bypass. Each independent-reference and Node-signed case runs twice normally and
twice under race. The actual installed signer version, source and generated
requests are preserved; no dependency is added or upgraded.
[Reference probe](2026-10-18-correctness-closure/raw/initial/auth_query_probe.formatted.go.txt),
[pinned signer receipt](2026-10-18-correctness-closure/raw/initial/node-sdk-receipt.json),
[generated signatures](2026-10-18-correctness-closure/raw/initial/node-sdk-signatures.json).

## Closed and nonblocking leads

C01 is closed by `Store.Policy` calling `policy.New(set, env)` and the existing
narrow-environment regression. C02 is closed by the registry's expiry check and
its expired/live control. C03 is closed by effective-authority reporting and the
existing grant/banner tests. C05 is closed within the managed namespace writer:
the native ancestor-fence permission fault retains `ErrUnknown` reservations;
the before-rename failure reconciles safely. These regressions run twice normally
and twice under race with no skips or failures.
[Closed-lead normal](2026-10-18-correctness-closure/raw/initial/closed-normal.log),
[race](2026-10-18-correctness-closure/raw/initial/closed-race.log).

C08's strict-spelling assertion deliberately records that a request using
`unsigned-payload` can authenticate **with a valid HMAC**. A credential holder
can already choose canonical `UNSIGNED-PAYLOAD`, so this probe does not demonstrate
additional authority or a newly bypassed body-integrity claim. Record it as
optional canonical-input strictness, rather than inflate the required defect
queue. That disposition follows the existing SDK compatibility/safety boundary in
[ADR 0002](../adr/0002-sdk-compatibility-and-mirror-writes.md), not a claim that
all AWS service strictness has been reproduced. The raw assertion failure remains
visible in the auth logs.

## Validation, limitations and stopping point

All execution is on scratch deployment `bb794cd2-6e96-4014-94bc-97bea9526b4a`:
Go 1.25.6, Node 25.9.0, Linux amd64, GOMAXPROCS 4, cgroup 4 CPU/4 GiB, test
processes UID 10001. The workspace permission probe explicitly uses `/results`
ext4. The task has no benchmarks and no local Go/Node project execution.
[Environment](2026-10-18-correctness-closure/raw/initial/environment.json),
[commands](2026-10-18-correctness-closure/raw/audit-plan.json).

The 22-step initial job and seven-step corrected-S3 job match their expected
exits, including the intentionally failing defect probes. This is **not** a claim
that the product passes those regressions. The initially missing explicit
Content-Length header made the PUT portion of the first S3 fixture unqualified;
that output is preserved and excluded from proof counts. The repaired fixture
sets the header, all healthy controls pass, and only the case-changed predicates
fail. No production edit or fault expectation is changed to obtain the result.
[Initial checks](2026-10-18-correctness-closure/raw/initial/checks.json),
[corrected checks](2026-10-18-correctness-closure/raw/final/checks.json),
[independent verification](2026-10-18-correctness-closure/raw/validation-receipt.json).

Qualified defect matrices contain **20 required-contract failing leaf executions
and 20 healthy passing leaf executions per mode**, plus two optional strictness
assertions per mode. Reference and SDK cases are executions of the same finding,
not additional defects. There are no probe skips or race-detector reports. These
are logical contract failures observed under race instrumentation, not newly
claimed Go data races. Vet, quality/file-size/format, site build/tests and docs
links pass after temporary probes are removed. Full Go/broad-race/four-conformance
qualification from the preceding [completion change](2026-10-18-completion-buffers.md)
is unchanged-source baseline evidence, not a new execution of those whole suites.

All tracked Go/module/package-lock inputs are checked against the pre-audit
inventory on Railway; local Git blobs are verified before and after commit.
The report, register and historical status pointers get separate Railway site
and documentation checks. The unchanged docs checker uses the recorded 16 MiB
Git-inventory capture preload, as in preceding reviews; the stock checker limit
is not claimed fixed. Evidence and teardown receipts are saved under
`2026-10-18-correctness-closure/raw`.

Physical crash/power-loss/disk-fault certification, wider Windows/macOS parity,
live-provider conformance, browser persistence and an exhaustive security review
remain qualification gaps. They are not four more reproduced defect tickets and
do not reopen the closed A1–A3 findings. The native workspace/HTTP/protocol scope
here is intentionally bounded to the nine listed leads.

The next independently reviewable correction is **C07, the ordering of query names
before URI encoding**. C04 was closed on 2026-10-19 by the
[workspace uncertainty fix](2026-10-19-workspace-uncertainty.md), C06 by the
[repeated signed header fix](2026-10-19-sigv4-repeated-header.md) and C09 by the
[opaque entity tag fix](2026-10-19-opaque-etag.md); the last required fix has
explicit acceptance criteria in the register. Incoming-write memory amplification,
listing/indexing and other performance work remain separate. No next fix or
optimization is implemented automatically in this closure review.
