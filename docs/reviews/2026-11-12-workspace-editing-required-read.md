# Workspace editing required-read pilot

**Date:** 2026-11-12. **Status:** the second bounded live model attempt failed
the editing gate. Native and E.1/E.2 CLI/MCP/SDK qualification remain local to
the declared macOS ARM64 `host_quiesced` profile; this attempt does not qualify
model editing, continuation, Linux, another physical host or release behavior.

## Attempt and evidence

This was a separately authorized attempt using OpenCode `v2.2.7` and the
OpenRouter Free Router. It used one task prompt and two guarded completion
requests, both HTTP 200: `liquid/lfm-3-1.7b:free` followed by
`dots-studio/dots-3-note-preview:free`. The first request required the four-tool
catalog and forced `stow_workspace_file_read_for_save` without a terminal schema.
The proxy correlated the actual assistant tool call with its native result and
verified a successful read of `docs/STATE.json`. Only the subsequent request
received the strict JSON schema; the verified-read phase and four-tool catalog
are present in the receipt.

The model made no batch read and no save call. It returned terminal fields
claiming a committed result without reading the named batch or publishing the
state. The strict trace and byte verifier rejected that response. Stage two did
not start, no checkpoint or handoff was created, and the sender workspace and
registry were retained. Independent host verification confirmed that
`docs/STATE.json`, `docs/batch-a.csv` and `docs/batch-b.csv` were all unchanged;
there was no save journal or receipt. The [second failed receipt](../assessment-evidence/2026-11-12/workspace-editing-model-required-read.json)
and [first attempt](2026-11-12-workspace-editing-model.md) remain separate. No
retry followed either attempt.

The corrected no-provider mock passed two prompts and eight fake completion
requests in 11,648 ms, including source removal and exact 52-byte final checkpoint
verification. The final agent suite passed 48 tests with one provider-opt-in test
skipped. Those checks validate the caller's offline behavior, not live model use
of the editing tools. Native sources remained unchanged and the 270 pre-existing
distribution omissions were preserved.

## Next gate

The initial-read requirement is now evidenced, but it did not make the model read
the batch or save. The next smallest gate change is to withhold terminal structured
output until the caller has correlated a committed save result for the exact path,
request key and generation. Keep strict read, batch-byte, single-save, host receipt
and checkpoint verification. Any further live attempt needs separate
authorization; do not accept terminal claims or reuse either failed receipt as a
pass.

The [W26 plan](../design/workspace-resource-plan.md#w26-f2-result) and
[canonical status row](../development/storage-foundation-plan.md) retain the live
model gate as open. No UI, native-code, deployment, Linux or second-host work was
included.
