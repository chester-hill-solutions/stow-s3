# Scope-aligned workspace editing attempt

**Result:** F.5 remains unqualified. The authorized OpenCode / OpenRouter Free
Router attempt was rejected by the strict trace verifier; no automatic task retry
or second attempt followed.

## What happened

The run lasted 43,151 ms and used one task prompt. A local proxy request returned
502 and the OpenCode client retried; the verifier correctly rejected the retry/error
history. Four subsequent upstream requests returned HTTP 200 through the Free
Router route. The trace records a STATE read, two reads of `docs/batch-a.csv`, one
committed save of the correct stage-one state (26 units, 5 entries, next batch B),
and a matching terminal answer. The verifier did not proceed to stage two or
checkpoint handoff.

A separate host-only follow-up recovered the owner, resolved the original save as
a replay with the same generation, verified the exact 66-byte STATE file and
unchanged CSVs, and captured checkpoint `cp_817852b1bb504e408b509a3a`. It made no
model call, new save or handoff; it does not change the failed pilot result.

The receipt's `paidFallbackObserved` field is a reporting defect: a generic local
502 with no model value was counted as a paid fallback. All four recorded
successful upstream requests used the Free Router route; this trace does not show
that a paid route was requested. The cause of the local 502 is unknown and is not
attributed to catalog startup or another specific condition.

## Verification and next step

The scope-aligned policy permits additional successful reads within the configured
`docs` root while retaining canonical path checks, the exact prior STATE and
current batch requirements, one observation-token-bound save, native receipt
verification and strict terminal/checkpoint checks. Focused tests passed 10/10;
the integrated suite passed 50 tests with one opt-in skip. This validates the
caller and verifier logic, not the failed live trace.

The next bounded work is to diagnose and instrument the local proxy 502 and fix
the false paid-fallback classification. No further live attempt has run. The Free
Router editing profile remains unqualified. Linux, second-host, immutable W25
continuation and release gates remain open.

See the [evidence receipt](../assessment-evidence/2026-11-12/workspace-editing-scope-aligned.json)
for the bounded trace and separate host-only verification.
