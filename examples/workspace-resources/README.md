# Free Router workspace pilot

Build Stow with `make build`, then create a private JSON profile. Replace the binary
and credential paths with your local absolute paths; the credential file contains
only your OpenRouter API key. Keep the key out of this profile.

```json
{
  "version": 1,
  "opencodeBinary": "/absolute/path/to/opencode",
  "expectedVersion": "opencode v2.2.7",
  "stowBinary": "/absolute/path/to/stow/bin/stow-s3",
  "model": { "providerID": "openrouter", "id": "openrouter/free" },
  "credentialEnv": "OPENROUTER_API_KEY",
  "credentialFile": "/absolute/path/to/private/openrouter.key",
  "deadlineMs": 300000,
  "maxTurns": 2,
  "allowPaidModels": false
}
```

Run the isolated acceptance pilot with the profile's path:

```sh
node examples/workspace-resources/run.mjs /absolute/path/to/free-router-profile.json
```

The current runner sends one task prompt, within the two-prompt ceiling, with no
automatic top-level retry. Optional arguments set the sanitized trace path and
bounded raw OpenCode JSONL diagnostic path, in that order. Child stderr on failure
is saved beside the diagnostic with a `.stderr` suffix. The runner uses an isolated
OpenCode home, a synthetic workspace, read-only MCP resources, and a loopback proxy.
It pins the root, title, compaction, and summary models to
`openrouter/openrouter/free`; the proxy rejects any completion request that is not
exactly `openrouter/free` or that contains fallback routing fields. The real
credential is read only by the runner and used only for the fixed OpenRouter
upstream; OpenCode receives a placeholder and the Stow MCP child receives an empty
key override.

The fixture has 100 alphabetically earlier decoys, a brief, and two CSV sources with one known mismatch. The answer must include both source resource URIs, the values 23 and 24, and the signed difference `1` (source Z minus A) in the terminal JSON response. Safe integer numbers and canonical integer strings are accepted; other coercions are rejected. The completed MCP trace must show a continuation page before reading the late source. A private sibling is outside the configured `inputs` read root. The runner records only bounded model request metadata and resource tool names, statuses, and URIs. Raw JSONL is written with mode `0600` to the diagnostic path for parser review and is never printed.

The OpenCode v2.2.7 selection points inspected for the pilot were `packages/opencode/src/session/context.ts` (`selectTitle`), `packages/opencode/src/session/compaction.ts`, and the agent/config schemas in `packages/schema/src/config/agent.ts` and `packages/schema/src/config.ts`. Local source inspection did not establish every fallback path, so the loopback request guard remains the enforcement boundary. OpenCode’s [agent documentation](https://opencode.ai/docs/agents/) describes the title, compaction, and summary maintenance agents; the [provider documentation](https://opencode.ai/docs/providers/) documents OpenRouter configuration. No resolved model suffix is treated as proof of price; the concrete returned model ID is recorded.

The independent resource protocol test is `extensions/stow/test/workspace-protocol.test.mjs`; the local guard, profile, trace-verifier, and loopback tests are `free-only.test.mjs`. These tests make no model calls. The pilot itself is opt-in and must only use the profile’s OpenRouter Free Router selector and its five-minute deadline.

## Local checkpoint continuation

Run the same-machine handoff recipe with the built Stow binary:

```sh
STOW_BIN=bin/stow-s3 node examples/workspace-resources/receiver-continuation.mjs
```

The recipe creates a portable checkpoint, exports and adopts it, removes the sender
workspace and registry, then creates a new checkpoint in the receiver workspace and
reads it through a fresh resource-only MCP server. It verifies the exact saved bytes
and SHA-256 values for two files while the receiver's mutable tree contains a later
edit. Output reports the source checkpoint identity separately from the receiver
workspace and receiver checkpoint identities. This is local same-machine evidence
only; it makes no model/provider calls and does not qualify cross-host behavior.
The extension's `checkpoint-transfer.test.mjs` invokes the same recipe in the
existing browser test suite.

## Fresh checkpoint continuation pilot

After building Stow and preparing the same private Free Router profile, the
checkpoint-specific runner can be invoked explicitly:

```sh
node examples/workspace-resources/checkpoint-runner.mjs /absolute/path/to/free-router-profile.json
```

This separate, opt-in run creates and adopts a portable handoff, removes the
sender workspace, registry, and source input tree, then starts a fresh isolated
OpenCode home against a receiver-local saved checkpoint. Its prompt supplies only
the checkpoint resource root, required JSON field names and URI navigation
guidance. The model must
read the saved prior total and next-batch data, calculate the next total, and cite
both exact resource URIs. The receiver's mutable input is deliberately changed
after snapshot capture, so the selected checkpoint remains the calculation source.
The MCP server is resources-only and the Free Router proxy guard remains active;
the runner makes one top-level model invocation with no retry and the profile's
five-minute deadline. It does not write model output back into the workspace.
The first local attempt returned the correct saved bytes and calculation but
constructed a file URI for a directory before recovering through tree navigation;
the strict trace gate rejected that attempt. The prompt now directs the model to
follow `root_uri`, entry `uri`, and `next_uri` exactly. The [failed receipt](../../docs/assessment-evidence/2026-11-11/checkpoint-model-local.json)
is preserved. A user-authorized [corrected attempt](../../docs/assessment-evidence/2026-11-11/checkpoint-model-local-attempt2.json)
completed all resource reads and returned the correct embedded result, but added
prose after the JSON. It also failed strict terminal-format acceptance. The
verifier remains unchanged; strict model qualification stays open.

The no-provider regression coverage is `checkpoint-pilot.test.mjs`; it checks
sender removal, distinct receiver identity, saved-versus-live bytes, selected
checkpoint configuration, and strict answer/resource-trace verification. This is
same-machine local evidence only and does not qualify cross-host continuation.

### Direct Luna checkpoint read continuation

A fresh `gpt-6-luna` / medium agent completed the same-host continuation through
the official MCP SDK. After the sender workspace, registry and source tree were
removed, it made seven successful catalog/read calls against the receiver's
resource-only checkpoint view and returned the exact whole-JSON result. Host
verification checked the advertised URI chain, saved-byte digests, changed live
inputs and absent sender paths. The model did not write or capture a checkpoint.
See the [review](../../docs/reviews/2026-11-12-checkpoint-luna-continuation.md)
and [receipt](../../docs/assessment-evidence/2026-11-12/checkpoint-luna-continuation.json).

This is same-machine read-continuation evidence only. It does not qualify Linux,
a second physical host, protected saved-version policy or process-wide filesystem
isolation. The earlier OpenCode / Free Router attempts remain failed.

## Managed editing qualification

The opt-in editing fixture is separate from both resource-only runners. Its
no-provider SDK test observes and updates `docs/STATE.json`, verifies the original
save receipt and a host-created checkpoint, exports/adopts the checkpoint, removes
the original workspace/registry/staging, then processes the second batch through
a new MCP process. The final state is read back from an immutable checkpoint.
This synthetic integration test does not constitute a model pass.

`editing-runner.mjs` prepares two fresh OpenCode contexts within one five-minute
deadline, stops after the first failure and never retries the task automatically.
It retains the private fixture and journal on failure, with bounded mode-0600 raw
diagnostics and a separate sanitized receipt. Expected numeric answers appear
only in host verification. The prior W25 failed receipts are unchanged.

The pinned binary's prompt API does not implement the installed SDK's legacy
structured-output route. The editing-only proxy therefore adds a strict provider
JSON schema and `require_parameters`, while retaining the existing fixed upstream
and Free Router selector guard. It filters the outgoing tool catalog to the four
qualified Stow editing names, and the verifier rejects every unexpected, failed or
out-of-order tool event. Terminal parsing accepts only the whole final JSON
message after completed tool calls. No embedded JSON or trailing prose is salvaged.

Direct MCP tools require a host configuration verified against the running binary;
the current [OpenCode configuration schema](https://opencode.ai/config.json)
provides `mcp.servers.<name>.codemode`. Tool names and permission behavior must be
proven with the real native MCP server and a fake provider; that local preflight
passed. Do not infer those names from stale SDK declarations. Linux, a second
physical host and the W25 selected immutable-view model flow remain separate gates.

Two separately authorized live Free Router editing attempts failed the model gate.
The [first](../../docs/assessment-evidence/2026-11-12/workspace-editing-model-local.json)
returned terminal values without any MCP calls. The [second](../../docs/assessment-evidence/2026-11-12/workspace-editing-model-required-read.json)
completed one verified STATE read but returned a committed claim without reading
the named batch or saving. Strict verification rejected both; host checks confirmed
all three inputs unchanged, with no checkpoint or handoff. Their [reviews](../../docs/reviews/2026-11-12-workspace-editing-model.md)
and [second-attempt analysis](../../docs/reviews/2026-11-12-workspace-editing-required-read.md)
preserve separate receipts. No retry followed either attempt. The corrected
no-provider mock passed both stages. A separately authorized direct Luna MCP
profile later passed; details follow.

## Direct Luna MCP editing result

Two fresh `gpt-6-luna` agents used the official MCP SDK on the same macOS ARM64
host. Each stage completed three model MCP calls: read the state, read its named
batch, then save the state. Stage one produced 26 units / 5 entries and a
checkpoint. The source workspace, registry and staging tree were removed before
the host adopted the checkpoint under a new workspace identity. A fresh second
context then continued from it.
Stage two produced 56 units / 7 entries; the final immutable checkpoint matched
the exact saved 52-byte state. See the
[review](../../docs/reviews/2026-11-12-workspace-editing-luna-direct.md) and
[receipt](../../docs/assessment-evidence/2026-11-12/workspace-editing-luna-direct.json).

This result covers the direct native MCP caller on one physical host. The generic
Node / exec runtime retained host capabilities, so it does not prove process-wide
filesystem isolation. The two OpenCode / Free Router attempts remain failed, and
Linux, second-host and protected-policy gates remain open.

## Free Router commit-gated attempt

One separately authorized OpenCode / Free Router attempt used the three-phase
caller gate: forced initial STATE read, required tools without terminal schema
until a committed save, then a no-tool terminal schema bound to the saved
generation. Both model stages saved the correct state. Stage one passed strict
verification and handed off its checkpoint. Stage two read STATE and
`docs/batch-b.csv`, but also read `docs/batch-a.csv`; strict verification rejected
that extra read because it was outside the stage-two input allowlist, even though
it was inside the configured `docs` read root. The verifier was not weakened.

An independent host follow-up resolved the existing save as a replay, verified
the exact 52-byte final checkpoint and confirmed both CSVs remained unchanged.
That post-failure verification made no additional model prompt or save and does
not change the failed trace result. See the
[F.4 review](../../docs/reviews/2026-11-12-workspace-editing-free-commit-gated.md)
and [receipt](../../docs/assessment-evidence/2026-11-12/workspace-editing-free-commit-gated.json).
The OpenCode / Free Router profile remains unqualified; direct Luna results are
separately documented above. Linux, second-host and protected-policy gates stay
open.

## Scope-aligned Free Router attempt

The scope-aligned caller now permits additional successful reads inside the
configured `docs` root while checking component boundaries and preserving the
required STATE/batch reads, token-bound save and strict receipt/checkpoint gates.
The next authorized OpenCode / Free Router attempt still failed strict trace
verification: a local proxy 502/retry was present, followed by one correct stage-one
save and terminal result. Stage two and checkpoint handoff did not run. The receipt
mislabels the generic local 502 as a paid fallback; all four recorded successful
upstream requests used the Free Router route, and no paid request was observed.
The cause of the 502 remains unknown. A separate host-only follow-up resolved the
save as a replay, verified the exact 66-byte state and unchanged CSVs, and captured
a checkpoint; it made no model call, new save or handoff. See the
[F.5 review](../../docs/reviews/2026-11-12-workspace-editing-scope-aligned.md)
and [receipt](../../docs/assessment-evidence/2026-11-12/workspace-editing-scope-aligned.json).
The Free Router editing profile remains unqualified; no retry or further live
attempt followed.

## Proxy diagnostics repair

The opt-in caller now records bounded guard/upstream failure categories without
logging request contents or raw errors. It distinguishes blocked non-free model
selection from a paid-route attempt and counts null-model proxy errors separately.
No-provider tests cover catalog/history refusals, fetch and response failures,
malformed requests, non-free selectors and privacy sentinels. Focused tests passed
25/25 and the integrated agent suite passed 56 with one provider-only test skipped.
The final fake-provider CLI smoke passed both synthetic stages and verified the
public receipt fields, source removal and exact checkpoint bytes.
This repairs diagnostics and receipt accounting; it does not establish the cause
of F.5's local 502 or qualify OpenCode / Free Router editing. See the
[proxy repair review](../../docs/reviews/2026-11-12-workspace-editing-proxy-repair.md)
and [receipt](../../docs/assessment-evidence/2026-11-12/workspace-editing-proxy-repair.json).
