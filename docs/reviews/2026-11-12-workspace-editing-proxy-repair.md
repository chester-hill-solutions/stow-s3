# Workspace editing proxy diagnostics repair

**Result:** The caller diagnostics/reporting repair passed focused tests and
independent review. It does not reclassify F.5 or qualify the live OpenCode / Free
Router editing flow.

## Changes and evidence

The editing caller now records a bounded failure kind, a `guard` or `upstream`
stage, and whether the upstream fetch was attempted. Catalog and history refusals
are counted separately. Records exclude error messages, request bodies, headers,
prompts, file data, observation tokens and credentials.

Runner receipts now mark a paid fallback only when an explicit non-free model was
actually attempted upstream. Blocked non-free selections and null-model local
errors have separate counts. The request record keeps the task stage apart from
the proxy stage, and request context no longer carries state across direct proxy
calls.

No-provider loopback regressions cover incomplete catalog refusal before fetch,
history refusal, upstream exceptions, upstream HTTP and invalid response errors,
malformed null-model requests, blocked non-free selectors and privacy sentinels.
They exercise the same request-metadata helper used by passed and failed runner
receipts. The focused suite passed 25/25; the integrated agent suite passed 56
with one opt-in provider test skipped. Independent review cleared the changes. A
final fake-provider CLI smoke on the frozen source passed two synthetic stages in
10,676 ms with nine fake requests, including the extra stage-two in-root read,
source removal before continuation and exact 52-byte checkpoint verification.
Its public runner receipt reported `paidFallbackObserved: false`, zero explicit
non-free requests and zero null-model errors. This is caller-path evidence, not a
live-model result.

The prior F.5 trace remains failed: OpenCode retried a local 502 before later
successful Free Router requests and a correct first-stage save. Its null-model
fallback flag was incorrect; all successful requests used Free Router, and no
paid request was observed.
The historical 502 cause remains unknown. The repair improves future evidence but
does not identify that cause or turn the earlier run into a pass. No real-provider
call was made for this repair.

See the [repair receipt](../assessment-evidence/2026-11-12/workspace-editing-proxy-repair.json),
[F.5 run receipt](../assessment-evidence/2026-11-12/workspace-editing-scope-aligned.json),
and [F.5 review](2026-11-12-workspace-editing-scope-aligned.md).
